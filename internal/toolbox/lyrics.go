package toolbox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type lyricsRecord struct {
	TrackName    string `json:"trackName"`
	ArtistName   string `json:"artistName"`
	AlbumName    string `json:"albumName"`
	Duration     int    `json:"duration"`
	SyncedLyrics string `json:"syncedLyrics"`
	PlainLyrics  string `json:"plainLyrics"`
	Instrumental bool   `json:"instrumental"`
}

func ExtractLyrics(root string, apply bool) error {
	scan, err := Scan(root)
	if err != nil {
		return err
	}
	for _, track := range scan.Tracks {
		if track.Extension == ".lrc" || track.Extension == ".cue" {
			continue
		}
		if _, err := os.Stat(strings.TrimSuffix(track.Path, filepath.Ext(track.Path)) + ".lrc"); err == nil {
			continue
		}
		lyrics := ""
		for key, value := range track.Tags {
			if strings.Contains(strings.ToLower(key), "lyrics") && strings.TrimSpace(value) != "" {
				lyrics = strings.TrimSpace(value)
				if strings.Contains(strings.ToLower(key), "unsynced") {
					break
				}
			}
		}
		if lyrics == "" {
			continue
		}
		sidecar := strings.TrimSuffix(track.Path, filepath.Ext(track.Path)) + ".lrc"
		if !apply {
			fmt.Printf("Would write %s\n", sidecar)
			continue
		}
		if err := writeLyrics(sidecar, lyrics); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", sidecar, err)
			continue
		}
		fmt.Printf("Wrote %s\n", sidecar)
	}
	return nil
}

func LookupLyrics(audioPath string, apply bool) error {
	track, err := scanSingleTrack(audioPath)
	if err != nil {
		return err
	}
	title := track.Title
	if title == "" {
		title = strings.TrimSpace(strings.TrimSuffix(track.Filename, filepath.Ext(track.Filename)))
	}
	artist := track.Artist
	if artist == "" {
		return fmt.Errorf("artist metadata is required for lyrics lookup")
	}
	artistParts := strings.FieldsFunc(artist, func(r rune) bool {
		return r == ';' || r == '/' || r == ','
	})
	if len(artistParts) == 0 {
		return fmt.Errorf("artist metadata is required for lyrics lookup")
	}
	artist = strings.TrimSpace(artistParts[0])
	record, err := lookupLRCLib(title, artist, track.Album, int(track.Duration+0.5))
	if err != nil {
		return err
	}
	lyrics := strings.TrimSpace(record.SyncedLyrics)
	if lyrics == "" {
		lyrics = strings.TrimSpace(record.PlainLyrics)
	}
	if lyrics == "" {
		if record.Instrumental {
			return fmt.Errorf("LRCLIB identified this track as instrumental")
		}
		return fmt.Errorf("no lyrics match found")
	}
	if !apply {
		fmt.Printf("Found lyrics for %s — %s\n%s\n", artist, title, previewText(lyrics, 96))
		return nil
	}
	sidecar := strings.TrimSuffix(track.Path, filepath.Ext(track.Path)) + ".lrc"
	if _, err := os.Lstat(sidecar); err == nil {
		return fmt.Errorf("refusing to overwrite existing sidecar %s", sidecar)
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := writeLyrics(sidecar, lyrics); err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n", sidecar)
	return nil
}

func scanSingleTrack(path string) (Track, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Track{}, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return Track{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Track{}, fmt.Errorf("audio path must be a regular, non-symlink file")
	}
	extension := strings.ToLower(filepath.Ext(absolute))
	if !supportedExtensions[extension] || extension == ".lrc" || extension == ".cue" {
		return Track{}, fmt.Errorf("unsupported audio file extension %q", extension)
	}
	track := Track{
		Path: absolute, RelativePath: filepath.Base(absolute), Filename: filepath.Base(absolute),
		Extension: extension, SizeBytes: info.Size(), Tags: map[string]string{},
		Compatibility: Compatibility{Status: "UNKNOWN"},
	}
	data, err := probe(absolute)
	if err != nil {
		return Track{}, err
	}
	return mergeProbe(track, data), nil
}

func lookupLRCLib(title, artist, album string, duration int) (lyricsRecord, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	params := url.Values{}
	params.Set("track_name", title)
	params.Set("artist_name", artist)
	if album != "" {
		params.Set("album_name", album)
	}
	if album != "" && duration > 0 {
		params.Set("duration", fmt.Sprint(duration))
		for _, endpoint := range []string{"get-cached", "get"} {
			record, status, err := requestLyrics(client, "https://lrclib.net/api/"+endpoint, params)
			if err == nil {
				return record, nil
			}
			if status != http.StatusNotFound {
				return lyricsRecord{}, err
			}
		}
	}
	records, _, err := requestLyricsSearch(client, params)
	if err != nil {
		return lyricsRecord{}, err
	}
	best := -1
	bestScore := -1
	for i, record := range records {
		score := 0
		if strings.EqualFold(strings.TrimSpace(record.TrackName), strings.TrimSpace(title)) {
			score += 50
		}
		if strings.EqualFold(strings.TrimSpace(record.ArtistName), strings.TrimSpace(artist)) {
			score += 45
		}
		if album != "" && strings.EqualFold(strings.TrimSpace(record.AlbumName), strings.TrimSpace(album)) {
			score += 20
		}
		if duration > 0 && record.Duration > 0 && abs(duration-record.Duration) <= 2 {
			score += 35
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return lyricsRecord{}, fmt.Errorf("no lyrics match found")
	}
	return records[best], nil
}

func requestLyrics(client *http.Client, endpoint string, params url.Values) (lyricsRecord, int, error) {
	request, err := http.NewRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return lyricsRecord{}, 0, err
	}
	request.Header.Set("User-Agent", "SnowskyEchoMiniToolbox-Go/1.0 (https://github.com/therosaer/snowsky-toolbox-reworked)")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return lyricsRecord{}, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return lyricsRecord{}, response.StatusCode, fmt.Errorf("LRCLIB returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return lyricsRecord{}, response.StatusCode, err
	}
	if len(data) > 2<<20 {
		return lyricsRecord{}, response.StatusCode, fmt.Errorf("LRCLIB response exceeded 2 MiB")
	}
	var record lyricsRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return lyricsRecord{}, response.StatusCode, err
	}
	return record, response.StatusCode, nil
}

func requestLyricsSearch(client *http.Client, params url.Values) ([]lyricsRecord, int, error) {
	request, err := http.NewRequest(http.MethodGet, "https://lrclib.net/api/search?"+params.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("User-Agent", "SnowskyEchoMiniToolbox-Go/1.0 (https://github.com/therosaer/snowsky-toolbox-reworked)")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, fmt.Errorf("LRCLIB returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if len(data) > 2<<20 {
		return nil, response.StatusCode, fmt.Errorf("LRCLIB response exceeded 2 MiB")
	}
	var records []lyricsRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, response.StatusCode, err
	}
	return records, response.StatusCode, nil
}

func writeLyrics(path, lyrics string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(file, strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(lyrics, "\r\n", "\n"), "\r", "\n"))+"\n"); err != nil {
		file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func previewText(text string, max int) string {
	text = strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
