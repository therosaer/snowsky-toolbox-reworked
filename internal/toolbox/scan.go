package toolbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var supportedExtensions = map[string]bool{
	".mp1": true, ".mp2": true, ".mp3": true, ".wma": true, ".wav": true,
	".ape": true, ".fla": true, ".flac": true, ".aac": true, ".m4a": true,
	".ogg": true, ".mp4": true, ".3gp": true, ".dff": true, ".dsf": true,
	".cue": true, ".lrc": true, ".aiff": true, ".opus": true, ".wv": true,
}

type ScanResult struct {
	Root       string  `json:"root"`
	TotalBytes int64   `json:"total_bytes"`
	Tracks     []Track `json:"tracks"`
}

type Track struct {
	Path          string            `json:"path"`
	RelativePath  string            `json:"relative_path"`
	Filename      string            `json:"filename"`
	Extension     string            `json:"extension"`
	SizeBytes     int64             `json:"size_bytes"`
	Duration      float64           `json:"duration_seconds,omitempty"`
	Bitrate       int               `json:"bitrate_kbps,omitempty"`
	SampleRate    int               `json:"sample_rate_hz,omitempty"`
	Channels      int               `json:"channels,omitempty"`
	BitsPerSample int               `json:"bits_per_sample,omitempty"`
	Codec         string            `json:"codec,omitempty"`
	HasArtwork    bool              `json:"has_artwork"`
	ArtworkCodec  string            `json:"artwork_codec,omitempty"`
	ArtworkWidth  int               `json:"artwork_width,omitempty"`
	ArtworkHeight int               `json:"artwork_height,omitempty"`
	Title         string            `json:"title,omitempty"`
	Artist        string            `json:"artist,omitempty"`
	Album         string            `json:"album,omitempty"`
	TrackNumber   string            `json:"track_number,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	Compatibility Compatibility     `json:"compatibility"`
}

type Compatibility struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type probeResult struct {
	Format struct {
		Duration string            `json:"duration"`
		BitRate  string            `json:"bit_rate"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType     string `json:"codec_type"`
		CodecName     string `json:"codec_name"`
		SampleRate    string `json:"sample_rate"`
		Channels      int    `json:"channels"`
		BitsPerSample int    `json:"bits_per_sample"`
		BitsRaw       int    `json:"bits_per_raw_sample"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		Disposition   struct {
			AttachedPicture int `json:"attached_pic"`
		} `json:"disposition"`
		Tags map[string]string `json:"tags"`
	} `json:"streams"`
}

func Scan(root string) (ScanResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ScanResult{}, err
	}
	info, err := os.Lstat(absRoot)
	if err != nil {
		return ScanResult{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ScanResult{}, fmt.Errorf("%s is not a directory", root)
	}
	result := ScanResult{Root: absRoot, Tracks: []Track{}}
	err = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() && path != absRoot {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			if isSystemFolder(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !supportedExtensions[ext] {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			rel = path
		}
		track := Track{
			Path: path, RelativePath: filepath.ToSlash(rel), Filename: entry.Name(),
			Extension: ext, SizeBytes: fileInfo.Size(), Tags: map[string]string{},
			Compatibility: Compatibility{Status: "UNKNOWN"},
		}
		result.TotalBytes += track.SizeBytes
		if ext != ".lrc" && ext != ".cue" {
			if probed, err := probe(path); err == nil {
				track = mergeProbe(track, probed)
				track.Compatibility = EvaluateCompatibility(track)
			} else {
				track.Compatibility = Compatibility{Status: "UNKNOWN", Reason: err.Error()}
			}
		}
		result.Tracks = append(result.Tracks, track)
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}
	return result, nil
}

func probe(path string) (probeResult, error) {
	executable, err := exec.LookPath("ffprobe")
	if err != nil {
		return probeResult{}, fmt.Errorf("ffprobe is required to inspect audio files")
	}
	output, err := exec.Command(executable, "-v", "error", "-show_format", "-show_streams", "-of", "json", path).Output()
	if err != nil {
		return probeResult{}, fmt.Errorf("ffprobe: %w", err)
	}
	var result probeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return probeResult{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	return result, nil
}

func mergeProbe(track Track, data probeResult) Track {
	track.Tags = mergeTags(data.Format.Tags)
	audioFound := false
	for _, stream := range data.Streams {
		if stream.CodecType != "audio" {
			if stream.Disposition.AttachedPicture == 1 {
				track.HasArtwork = true
				track.ArtworkCodec = strings.ToLower(stream.CodecName)
				track.ArtworkWidth = stream.Width
				track.ArtworkHeight = stream.Height
			}
			continue
		}
		if audioFound {
			continue
		}
		audioFound = true
		track.Codec = strings.ToLower(stream.CodecName)
		track.SampleRate, _ = strconv.Atoi(stream.SampleRate)
		track.Channels = stream.Channels
		if stream.BitsRaw > 0 {
			track.BitsPerSample = stream.BitsRaw
		} else {
			track.BitsPerSample = stream.BitsPerSample
		}
		track.Tags = mergeTags(track.Tags, stream.Tags)
	}
	track.Duration, _ = strconv.ParseFloat(data.Format.Duration, 64)
	bitrate, _ := strconv.Atoi(data.Format.BitRate)
	track.Bitrate = bitrate / 1000
	track.Title = firstTag(track.Tags, "title")
	track.Artist = firstTag(track.Tags, "artist")
	track.Album = firstTag(track.Tags, "album")
	track.TrackNumber = firstTag(track.Tags, "track", "tracknumber")
	return track
}

func mergeTags(groups ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, tags := range groups {
		for key, value := range tags {
			result[strings.ToLower(key)] = value
		}
	}
	return result
}

func firstTag(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(tags[strings.ToLower(key)]); value != "" {
			return value
		}
	}
	return ""
}

func isSystemFolder(name string) bool {
	switch strings.ToLower(name) {
	case ".trashes", ".spotlight-v100", ".fseventsd", "system volume information", "$recycle.bin":
		return true
	default:
		return false
	}
}

func FormatBytes(size int64) string {
	if size < 1<<20 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
}
