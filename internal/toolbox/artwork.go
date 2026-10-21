package toolbox

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ArtworkResult struct {
	Path   string
	Status string
	Reason string
	Width  int
	Height int
}

var musicBrainzRequestMu sync.Mutex
var lastMusicBrainzRequest time.Time

func InspectArtwork(root string) ([]ArtworkResult, error) {
	scan, err := Scan(root)
	if err != nil {
		return nil, err
	}
	results := make([]ArtworkResult, 0)
	for _, track := range scan.Tracks {
		if track.Extension == ".lrc" || track.Extension == ".cue" {
			continue
		}
		result := ArtworkResult{Path: track.RelativePath}
		if !track.HasArtwork {
			result.Status, result.Reason = "MISSING", "No embedded artwork"
			results = append(results, result)
			continue
		}
		result.Width, result.Height = track.ArtworkWidth, track.ArtworkHeight
		switch track.ArtworkCodec {
		case "mjpeg":
			result.Status = "COMPATIBLE"
		case "png":
			result.Status, result.Reason = "UNSUPPORTED", "Artwork must be JPEG"
		default:
			result.Status, result.Reason = "UNSUPPORTED", "Artwork must be JPEG"
		}
		if result.Width > 1000 || result.Height > 1000 {
			result.Status = "LIMITED"
			result.Reason = "Artwork exceeds 1000x1000 pixels"
		}
		if track.ArtworkCodec == "mjpeg" {
			data, err := extractArtwork(track.Path)
			if err == nil && jpegIsProgressive(data) {
				result.Status, result.Reason = "UNSUPPORTED", "Progressive JPEG is not supported"
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func FixArtwork(root string, apply bool, backupDir string) ([]ArtworkResult, error) {
	results, err := InspectArtwork(root)
	if err != nil {
		return nil, err
	}
	if apply && backupDir == "" {
		return nil, fmt.Errorf("a --backup-dir is required when applying artwork fixes")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	backupAbs, err := filepath.Abs(backupDir)
	if err != nil && backupDir != "" {
		return nil, err
	}
	if backupDir != "" && pathWithin(backupAbs, rootAbs) {
		return nil, fmt.Errorf("backup directory must be outside the source folder")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg is required to fix embedded artwork")
	}
	scan, err := Scan(rootAbs)
	if err != nil {
		return nil, err
	}
	tracks := make(map[string]Track, len(scan.Tracks))
	for _, track := range scan.Tracks {
		tracks[track.RelativePath] = track
	}
	for i := range results {
		if results[i].Status == "COMPATIBLE" || results[i].Status == "MISSING" {
			continue
		}
		track := tracks[results[i].Path]
		if !supportedArtworkContainer(track.Extension) {
			results[i].Status = "SKIPPED"
			results[i].Reason = "CLI artwork replacement supports MP3, FLAC, and M4A containers"
			continue
		}
		if !apply {
			results[i].Reason = "Would re-encode and resize embedded artwork"
			continue
		}
		rel, err := filepath.Rel(rootAbs, track.Path)
		if err != nil || strings.HasPrefix(rel, "..") {
			results[i].Status, results[i].Reason = "FAILED", "file is outside target folder"
			continue
		}
		backupPath := filepath.Join(backupAbs, rel)
		if err := copyFile(track.Path, backupPath); err != nil {
			results[i].Status, results[i].Reason = "FAILED", "backup failed: "+err.Error()
			continue
		}
		imageTemp, err := os.CreateTemp(filepath.Dir(track.Path), ".snowsky-cover-*.jpg")
		if err != nil {
			results[i].Status, results[i].Reason = "FAILED", err.Error()
			continue
		}
		imageTempPath := imageTemp.Name()
		imageTemp.Close()
		_ = os.Remove(imageTempPath)
		filter := "scale=w='min(1000,iw)':h='min(1000,ih)':force_original_aspect_ratio=decrease"
		cmd := exec.Command(ffmpeg, "-y", "-v", "error", "-i", track.Path, "-map", "0:v:0", "-frames:v", "1", "-vf", filter, "-q:v", "2", imageTempPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			_ = os.Remove(imageTempPath)
			results[i].Status, results[i].Reason = "FAILED", strings.TrimSpace(string(out))
			if results[i].Reason == "" {
				results[i].Reason = err.Error()
			}
			continue
		}
		outputTemp, err := os.CreateTemp(filepath.Dir(track.Path), ".snowsky-art-*.tmp"+track.Extension)
		if err != nil {
			_ = os.Remove(imageTempPath)
			results[i].Status, results[i].Reason = "FAILED", err.Error()
			continue
		}
		outputTempPath := outputTemp.Name()
		outputTemp.Close()
		_ = os.Remove(outputTempPath)
		args := []string{"-y", "-v", "error", "-i", track.Path, "-i", imageTempPath, "-map", "0:a", "-map", "1:v:0", "-map_metadata", "0", "-c", "copy", "-disposition:v:0", "attached_pic", "-metadata:s:v:0", "title=Album cover", "-metadata:s:v:0", "comment=Cover (front)", outputTempPath}
		out, err = exec.Command(ffmpeg, args...).CombinedOutput()
		_ = os.Remove(imageTempPath)
		if err != nil {
			_ = os.Remove(outputTempPath)
			results[i].Status, results[i].Reason = "FAILED", strings.TrimSpace(string(out))
			if results[i].Reason == "" {
				results[i].Reason = err.Error()
			}
			continue
		}
		if err := replaceFile(outputTempPath, track.Path); err != nil {
			_ = os.Remove(outputTempPath)
			results[i].Status, results[i].Reason = "FAILED", "could not replace file: "+err.Error()
			continue
		}
		results[i].Status, results[i].Reason = "FIXED", "Artwork re-encoded as baseline JPEG"
	}
	return results, nil
}

func extractArtwork(path string) ([]byte, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, err
	}
	output, err := exec.Command(ffmpeg, "-v", "error", "-i", path, "-map", "0:v:0", "-frames:v", "1", "-c:v", "copy", "-f", "image2pipe", "-").Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}

func jpegIsProgressive(data []byte) bool {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return false
	}
	for offset := 2; offset+3 < len(data); {
		if data[offset] != 0xff {
			offset++
			continue
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset >= len(data) {
			break
		}
		marker := data[offset]
		offset++
		if marker == 0xd9 || marker == 0xda {
			return false
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if offset+2 > len(data) {
			break
		}
		length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if length < 2 || offset+length > len(data) {
			break
		}
		switch marker {
		case 0xc2, 0xc6, 0xca, 0xce:
			return true
		case 0xc0, 0xc1, 0xc3, 0xc5, 0xc7, 0xc9, 0xcb, 0xcd, 0xcf:
			return false
		}
		offset += length
	}
	return false
}

func supportedArtworkContainer(extension string) bool {
	switch strings.ToLower(extension) {
	case ".mp3", ".flac", ".fla", ".m4a", ".m4b", ".m4p":
		return true
	default:
		return false
	}
}

func DownloadArtwork(file, artist, album, backupDir string, apply bool) (string, error) {
	artist, album = strings.TrimSpace(artist), strings.TrimSpace(album)
	if artist == "" || album == "" {
		return "", fmt.Errorf("artist and album are required")
	}
	path, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to edit non-regular file %s", path)
	}
	if !supportedArtworkContainer(filepath.Ext(path)) {
		return "", fmt.Errorf("artwork embedding supports MP3, FLAC, and M4A containers")
	}
	if apply && backupDir == "" {
		return "", fmt.Errorf("--backup-dir is required when applying downloaded artwork")
	}
	ffmpeg := ""
	if apply {
		ffmpeg, err = exec.LookPath("ffmpeg")
		if err != nil {
			return "", fmt.Errorf("ffmpeg is required to embed downloaded artwork")
		}
	}
	recordID, err := findMusicBrainzRelease(artist, album)
	if err != nil {
		return "", err
	}
	cover, err := downloadCover(recordID)
	if err != nil {
		return "", err
	}
	if !apply {
		return fmt.Sprintf("Found cover art for %s — %s; use --apply to embed", artist, album), nil
	}
	backupAbs, err := filepath.Abs(backupDir)
	if err != nil {
		return "", err
	}
	if pathWithin(backupAbs, filepath.Dir(path)) {
		return "", fmt.Errorf("backup directory must be outside the audio folder")
	}
	backupPath := filepath.Join(backupAbs, filepath.Base(path))
	if err := copyFile(path, backupPath); err != nil {
		return "", fmt.Errorf("backup original: %w", err)
	}
	imageTemp, err := os.CreateTemp(filepath.Dir(path), ".snowsky-download-*.img")
	if err != nil {
		return "", err
	}
	imagePath := imageTemp.Name()
	if _, err := imageTemp.Write(cover); err != nil {
		imageTemp.Close()
		_ = os.Remove(imagePath)
		return "", err
	}
	if err := imageTemp.Close(); err != nil {
		_ = os.Remove(imagePath)
		return "", err
	}
	defer os.Remove(imagePath)
	if err := embedJPEG(ffmpeg, path, imagePath); err != nil {
		return "", err
	}
	return fmt.Sprintf("Embedded album artwork for %s — %s (original backed up to %s)", artist, album, backupPath), nil
}

func findMusicBrainzRelease(artist, album string) (string, error) {
	params := url.Values{}
	params.Set("query", fmt.Sprintf(`release:"%s" AND artist:"%s"`, escapeSearchTerm(album), escapeSearchTerm(artist)))
	params.Set("fmt", "json")
	params.Set("limit", "5")
	endpoint := "https://musicbrainz.org/ws/2/release/?" + params.Encode()
	musicBrainzRequestMu.Lock()
	if wait := time.Until(lastMusicBrainzRequest.Add(time.Second)); wait > 0 && wait < time.Second {
		time.Sleep(wait)
	}
	lastMusicBrainzRequest = time.Now()
	musicBrainzRequestMu.Unlock()
	client := &http.Client{Timeout: 15 * time.Second}
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "SnowskyEchoMiniToolbox-Go/1.0 (https://github.com/therosaer/snowsky-toolbox-reworked)")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MusicBrainz returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 2<<20 {
		return "", fmt.Errorf("MusicBrainz response exceeded 2 MiB")
	}
	var result struct {
		Releases []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	for _, release := range result.Releases {
		if release.ID != "" {
			return release.ID, nil
		}
	}
	return "", fmt.Errorf("no MusicBrainz release found")
}

func downloadCover(releaseID string) ([]byte, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many cover art redirects")
			}
			return nil
		},
	}
	endpoint := "https://coverartarchive.org/release/" + url.PathEscape(releaseID) + "/front-500"
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "SnowskyEchoMiniToolbox-Go/1.0 (https://github.com/therosaer/snowsky-toolbox-reworked)")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Cover Art Archive returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (20<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 20<<20 {
		return nil, fmt.Errorf("cover art response exceeded 20 MiB")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("cover art response is not a supported image: %w", err)
	}
	return data, nil
}

func embedJPEG(ffmpeg, audioPath, imagePath string) error {
	imageTemp, err := os.CreateTemp(filepath.Dir(audioPath), ".snowsky-cover-*.jpg")
	if err != nil {
		return err
	}
	imageTempPath := imageTemp.Name()
	imageTemp.Close()
	_ = os.Remove(imageTempPath)
	defer os.Remove(imageTempPath)
	filter := "scale=w='min(1000,iw)':h='min(1000,ih)':force_original_aspect_ratio=decrease"
	cmd := exec.Command(ffmpeg, "-y", "-v", "error", "-i", imagePath, "-frames:v", "1", "-vf", filter, "-q:v", "2", imageTempPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("normalize artwork: %s", message)
	}
	outputTemp, err := os.CreateTemp(filepath.Dir(audioPath), ".snowsky-art-*.tmp"+filepath.Ext(audioPath))
	if err != nil {
		return err
	}
	outputTempPath := outputTemp.Name()
	outputTemp.Close()
	_ = os.Remove(outputTempPath)
	defer os.Remove(outputTempPath)
	args := []string{"-y", "-v", "error", "-i", audioPath, "-i", imageTempPath, "-map", "0:a", "-map", "1:v:0", "-map_metadata", "0", "-c", "copy", "-disposition:v:0", "attached_pic", "-metadata:s:v:0", "title=Album cover", "-metadata:s:v:0", "comment=Cover (front)", outputTempPath}
	if output, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("embed artwork: %s", message)
	}
	return replaceFile(outputTempPath, audioPath)
}

func escapeSearchTerm(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func jpegDimensions(data []byte) (int, int, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}
