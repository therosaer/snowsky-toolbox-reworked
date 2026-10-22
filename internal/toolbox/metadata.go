package toolbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var validMetadataKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ListMetadata(path string) error {
	result, err := probe(path)
	if err != nil {
		return err
	}
	tags := mergeTags(result.Format.Tags)
	for _, stream := range result.Streams {
		if stream.CodecType == "audio" {
			tags = mergeTags(tags, stream.Tags)
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(tags)
}

func EditMetadata(path, action, key, value string, apply bool, backup string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to edit non-regular file %s", absPath)
	}
	if action == "set" || action == "remove" {
		if !validMetadataKey.MatchString(key) {
			return fmt.Errorf("invalid metadata key %q", key)
		}
	}
	changes := map[string]string{}
	switch action {
	case "set":
		changes[key] = value
	case "remove":
		changes[key] = ""
	case "sanitize":
		probeData, err := probe(absPath)
		if err != nil {
			return err
		}
		tags := mergeTags(probeData.Format.Tags)
		for _, stream := range probeData.Streams {
			if stream.CodecType == "audio" {
				tags = mergeTags(tags, stream.Tags)
			}
		}
		core := map[string]bool{
			"title": true, "artist": true, "album": true, "albumartist": true,
			"album_artist": true, "track": true, "tracknumber": true, "disc": true,
			"discnumber": true, "genre": true, "date": true, "year": true,
		}
		for tag := range tags {
			if !core[strings.ToLower(tag)] {
				changes[tag] = ""
			}
		}
	default:
		return fmt.Errorf("unsupported metadata operation %q", action)
	}
	if len(changes) == 0 {
		fmt.Println("No metadata changes needed")
		return nil
	}
	if !apply {
		fmt.Printf("Would %s %d metadata field(s) in %s\n", action, len(changes), absPath)
		return nil
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg is required to write metadata")
	}
	if backup != "" {
		backupAbs, err := filepath.Abs(backup)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(backupAbs); err == nil {
			return fmt.Errorf("backup already exists: %s", backupAbs)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := copyFile(absPath, backupAbs); err != nil {
			return fmt.Errorf("backup original: %w", err)
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(absPath), ".snowsky-metadata-*.tmp"+filepath.Ext(absPath))
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	_ = os.Remove(tempPath)
	args := []string{"-y", "-v", "error", "-i", absPath, "-map", "0", "-map_metadata", "0", "-c", "copy"}
	for tag, tagValue := range changes {
		args = append(args, "-metadata", tag+"="+tagValue)
	}
	args = append(args, tempPath)
	output, err := exec.Command(ffmpeg, args...).CombinedOutput()
	if err != nil {
		_ = os.Remove(tempPath)
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("ffmpeg metadata write failed: %s", message)
	}
	if err := replaceFile(tempPath, absPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace audio file: %w", err)
	}
	fmt.Printf("Updated metadata in %s\n", absPath)
	return nil
}
