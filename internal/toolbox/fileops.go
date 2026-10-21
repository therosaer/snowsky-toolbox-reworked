package toolbox

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type BackupResult struct {
	Path            string
	Files           int
	SkippedSymlinks int
}

type CleanupResult struct {
	Paths       []string
	Files       int
	Directories int
}

type RenameResult struct {
	From   string
	To     string
	Status string
}

var unsafeFilename = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
var digits = regexp.MustCompile(`\d+`)

var renameTemplates = map[string]string{
	"trackno_trackname":    "{track_no}. {title}",
	"trackno_artist_title": "{track_no}. {artist} - {title}",
	"artist_title":         "{artist} - {title}",
	"title_only":           "{title}",
}

func Backup(source, destination string) (BackupResult, error) {
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return BackupResult{}, err
	}
	destinationAbs, err := filepath.Abs(destination)
	if err != nil {
		return BackupResult{}, err
	}
	info, err := os.Stat(sourceAbs)
	if err != nil {
		return BackupResult{}, err
	}
	if !info.IsDir() {
		return BackupResult{}, fmt.Errorf("%s is not a directory", source)
	}
	if pathWithin(destinationAbs, sourceAbs) {
		return BackupResult{}, fmt.Errorf("backup archive must be outside the source folder")
	}
	if _, err := os.Lstat(destinationAbs); err == nil {
		return BackupResult{}, fmt.Errorf("destination already exists: %s", destinationAbs)
	} else if !os.IsNotExist(err) {
		return BackupResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destinationAbs), 0o755); err != nil {
		return BackupResult{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(destinationAbs), ".snowsky-backup-*.zip")
	if err != nil {
		return BackupResult{}, err
	}
	tempPath := temp.Name()
	_ = temp.Close()
	defer os.Remove(tempPath)
	archiveFile, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return BackupResult{}, err
	}
	archive := zip.NewWriter(archiveFile)
	result := BackupResult{Path: destinationAbs}
	rootName := filepath.Base(sourceAbs)
	walkErr := filepath.WalkDir(sourceAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path != sourceAbs && entry.IsDir() && isSystemFolder(entry.Name()) {
			return filepath.SkipDir
		}
		if path == sourceAbs {
			_, err := archive.Create(rootName + "/")
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			result.SkippedSymlinks++
			return nil
		}
		rel, err := filepath.Rel(sourceAbs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Join(rootName, rel))
		if entry.IsDir() {
			_, err = archive.Create(strings.TrimSuffix(rel, "/") + "/")
			return err
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = rel
		header.Method = zip.Deflate
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		result.Files++
		return nil
	})
	closeErr := archive.Close()
	fileCloseErr := archiveFile.Close()
	if walkErr != nil {
		return BackupResult{}, walkErr
	}
	if closeErr != nil {
		return BackupResult{}, closeErr
	}
	if fileCloseErr != nil {
		return BackupResult{}, fileCloseErr
	}
	if err := os.Rename(tempPath, destinationAbs); err != nil {
		return BackupResult{}, err
	}
	return result, nil
}

func Cleanup(root string, apply bool) (CleanupResult, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return CleanupResult{}, err
	}
	info, err := os.Stat(rootAbs)
	if err != nil {
		return CleanupResult{}, err
	}
	if !info.IsDir() {
		return CleanupResult{}, fmt.Errorf("%s is not a directory", root)
	}
	result := CleanupResult{Paths: []string{}}
	var emptyDirs []string
	err = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == rootAbs {
			return nil
		}
		if entry.IsDir() {
			if isSystemFolder(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(entry.Name())
		if name == ".ds_store" || name == "thumbs.db" || name == "desktop.ini" || strings.HasPrefix(name, "._") {
			result.Paths = append(result.Paths, path)
		}
		return nil
	})
	if err != nil {
		return CleanupResult{}, err
	}
	for _, path := range result.Paths {
		if apply {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return result, err
			}
		}
	}
	result.Files = len(result.Paths)
	if !apply {
		return result, nil
	}
	err = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path != rootAbs && entry.IsDir() && !isSystemFolder(entry.Name()) {
			emptyDirs = append(emptyDirs, path)
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	for i := len(emptyDirs) - 1; i >= 0; i-- {
		if err := os.Remove(emptyDirs[i]); err == nil {
			result.Directories++
			result.Paths = append(result.Paths, emptyDirs[i])
		} else if !os.IsNotExist(err) && !isNotEmpty(err) {
			return result, err
		}
	}
	return result, nil
}

func isNotEmpty(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "not empty") || strings.Contains(strings.ToLower(err.Error()), "directory not empty")
}

func Rename(root, preset string, apply bool) ([]RenameResult, error) {
	template, ok := renameTemplates[preset]
	if !ok {
		return nil, fmt.Errorf("unknown preset %q", preset)
	}
	scan, err := Scan(root)
	if err != nil {
		return nil, err
	}
	type proposal struct {
		track Track
		to    string
	}
	var proposals []proposal
	results := make([]RenameResult, 0)
	for _, track := range scan.Tracks {
		if track.Extension == ".lrc" || track.Extension == ".cue" || track.Title == "" {
			continue
		}
		number := parseTrackNumber(track.TrackNumber)
		title := cleanTag(track.Title)
		artist := cleanTag(track.Artist)
		album := cleanTag(track.Album)
		if strings.Contains(template, "{track_no}") && number == "" ||
			strings.Contains(template, "{artist}") && artist == "" ||
			strings.Contains(template, "{album}") && album == "" {
			continue
		}
		name := strings.NewReplacer(
			"{track_no}", number, "{title}", title, "{artist}", artist, "{album}", album,
		).Replace(template)
		name = strings.Join(strings.Fields(name), " ")
		name = strings.TrimSpace(strings.TrimRight(name, " ."))
		if name == "" {
			continue
		}
		upper := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
		switch upper {
		case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			name = "_" + name
		}
		target := filepath.Join(filepath.Dir(track.Path), name+track.Extension)
		if filepath.Clean(target) == filepath.Clean(track.Path) {
			results = append(results, RenameResult{From: track.RelativePath, To: filepath.Base(target), Status: "MATCHING"})
			continue
		}
		proposals = append(proposals, proposal{track: track, to: target})
	}
	targetCounts := make(map[string]int)
	for _, proposal := range proposals {
		targetCounts[strings.ToLower(proposal.to)]++
	}
	for _, proposal := range proposals {
		status := "RENAME"
		if targetCounts[strings.ToLower(proposal.to)] > 1 {
			status = "CONFLICT: duplicate target"
		} else if _, err := os.Lstat(proposal.to); err == nil {
			status = "CONFLICT: target exists"
		} else if !os.IsNotExist(err) {
			return results, err
		}
		if status == "RENAME" && apply {
			if err := os.Rename(proposal.track.Path, proposal.to); err != nil {
				status = "FAILED: " + err.Error()
			}
		}
		results = append(results, RenameResult{From: proposal.track.RelativePath, To: filepath.Base(proposal.to), Status: status})
	}
	return results, nil
}

func cleanTag(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = unsafeFilename.ReplaceAllString(value, "_")
	return strings.TrimSpace(strings.TrimRight(value, " ."))
}

func parseTrackNumber(raw string) string {
	if split := strings.SplitN(raw, "/", 2); len(split) > 0 {
		raw = split[0]
	}
	digits := digits.FindString(raw)
	if digits == "" {
		return ""
	}
	number, err := strconv.Atoi(digits)
	if err != nil {
		return digits
	}
	return fmt.Sprintf("%02d", number)
}

func pathWithin(candidate, root string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}
