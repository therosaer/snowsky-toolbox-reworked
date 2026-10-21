package toolbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ConvertResult struct {
	Path    string
	Message string
}

func Convert(root string, eqCompatible, apply bool, backupDir string) ([]ConvertResult, error) {
	scan, err := Scan(root)
	if err != nil {
		return nil, err
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg is required to convert audio")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var backupAbs string
	if backupDir != "" {
		backupAbs, err = filepath.Abs(backupDir)
		if err != nil {
			return nil, err
		}
		if pathWithin(backupAbs, rootAbs) {
			return nil, fmt.Errorf("backup directory must be outside the source folder")
		}
	}
	results := make([]ConvertResult, 0)
	claimed := make(map[string]bool)
	for _, track := range scan.Tracks {
		needsConversion := track.Compatibility.Status == "UNSUPPORTED"
		if eqCompatible && (track.BitsPerSample > 16 || track.SampleRate > 192000) {
			needsConversion = true
		}
		if !needsConversion {
			continue
		}
		if track.Extension == ".lrc" || track.Extension == ".cue" || track.Codec == "" {
			results = append(results, ConvertResult{Path: track.RelativePath, Message: "skipped: audio stream could not be inspected"})
			continue
		}
		output := track.Path
		if track.Extension != ".flac" && track.Extension != ".fla" {
			output = strings.TrimSuffix(track.Path, filepath.Ext(track.Path)) + ".flac"
		}
		if claimed[output] {
			results = append(results, ConvertResult{Path: track.RelativePath, Message: "skipped: another input uses the same FLAC output"})
			continue
		}
		claimed[output] = true
		if output != track.Path {
			if _, err := os.Lstat(output); err == nil {
				results = append(results, ConvertResult{Path: track.RelativePath, Message: "skipped: conversion target already exists"})
				continue
			} else if !os.IsNotExist(err) {
				return results, err
			}
		}
		if !apply {
			results = append(results, ConvertResult{Path: track.RelativePath, Message: "would convert to " + filepath.Base(output)})
			continue
		}
		if backupAbs != "" {
			rel, err := filepath.Rel(rootAbs, track.Path)
			if err != nil || strings.HasPrefix(rel, "..") {
				results = append(results, ConvertResult{Path: track.RelativePath, Message: "skipped: source resolves outside target"})
				continue
			}
			if err := copyFile(track.Path, filepath.Join(backupAbs, rel)); err != nil {
				results = append(results, ConvertResult{Path: track.RelativePath, Message: "backup failed: " + err.Error()})
				continue
			}
		}
		temp, err := os.CreateTemp(filepath.Dir(output), ".snowsky-convert-*.flac")
		if err != nil {
			results = append(results, ConvertResult{Path: track.RelativePath, Message: err.Error()})
			continue
		}
		tempPath := temp.Name()
		temp.Close()
		_ = os.Remove(tempPath)
		bits := 24
		sampleFormat := "s32"
		if eqCompatible {
			bits, sampleFormat = 16, "s16"
		}
		args := []string{"-y", "-v", "error", "-i", track.Path, "-map", "0:a:0", "-map_metadata", "0", "-c:a", "flac", "-compression_level", "8", "-sample_fmt", sampleFormat, "-bits_per_raw_sample", strconv.Itoa(bits)}
		if track.SampleRate > 192000 {
			args = append(args, "-ar", "192000")
		}
		if track.Channels > 2 {
			args = append(args, "-ac", "2")
		}
		args = append(args, tempPath)
		command := exec.Command(ffmpeg, args...)
		if output, err := command.CombinedOutput(); err != nil {
			_ = os.Remove(tempPath)
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = err.Error()
			}
			results = append(results, ConvertResult{Path: track.RelativePath, Message: "conversion failed: " + message})
			continue
		}
		installErr := error(nil)
		if output == track.Path {
			installErr = os.Rename(tempPath, output)
		} else {
			installErr = os.Link(tempPath, output)
			if installErr == nil {
				_ = os.Remove(tempPath)
			}
		}
		if installErr != nil {
			_ = os.Remove(tempPath)
			results = append(results, ConvertResult{Path: track.RelativePath, Message: "could not install converted file: " + installErr.Error()})
			continue
		}
		if output != track.Path {
			if err := os.Remove(track.Path); err != nil {
				results = append(results, ConvertResult{Path: track.RelativePath, Message: "converted to " + filepath.Base(output) + "; original retained: " + err.Error()})
				continue
			}
		}
		results = append(results, ConvertResult{Path: track.RelativePath, Message: "converted to " + filepath.Base(output)})
	}
	return results, nil
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		_ = os.Remove(destination)
		return err
	}
	return output.Close()
}
