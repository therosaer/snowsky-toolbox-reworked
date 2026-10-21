package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/therosaer/snowsky-toolbox-reworked/internal/toolbox"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "snowsky:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: snowsky <scan|backup|cleanup|rename|convert|lyrics> [options]")
	}
	switch args[0] {
	case "scan":
		return runScan(args[1:])
	case "backup":
		return runBackup(args[1:])
	case "cleanup":
		return runCleanup(args[1:])
	case "rename":
		return runRename(args[1:])
	case "convert":
		return runConvert(args[1:])
	case "lyrics":
		return runLyrics(args[1:])
	case "help", "-h", "--help":
		fmt.Println("Snowsky Echo Mini Toolbox (CLI)\n\nCommands: scan, backup, cleanup, rename, convert, lyrics")
		return nil
	default:
		return fmt.Errorf("unknown command %q; run snowsky help", args[0])
	}
}

func runScan(args []string) error {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "write scan results as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: snowsky scan [--json] <folder>")
	}
	result, err := toolbox.Scan(flags.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Printf("Files: %d  Size: %s\n", len(result.Tracks), toolbox.FormatBytes(result.TotalBytes))
	for _, track := range result.Tracks {
		fmt.Printf("%-12s %-8s %s\n", track.Compatibility.Status, track.Extension, track.RelativePath)
		if track.Compatibility.Reason != "" {
			fmt.Printf("             %s\n", track.Compatibility.Reason)
		}
	}
	return nil
}

func runBackup(args []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: snowsky backup <source-folder> <backup.zip>")
	}
	result, err := toolbox.Backup(flags.Arg(0), flags.Arg(1))
	if err != nil {
		return err
	}
	fmt.Printf("Backed up %d files to %s (skipped %d symlinks)\n", result.Files, result.Path, result.SkippedSymlinks)
	return nil
}

func runCleanup(args []string) error {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	apply := flags.Bool("apply", false, "delete discovered system files and empty folders")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: snowsky cleanup [--apply] <folder>")
	}
	result, err := toolbox.Cleanup(flags.Arg(0), *apply)
	if err != nil {
		return err
	}
	action := "Would remove"
	if *apply {
		action = "Removed"
	}
	fmt.Printf("%s %d files and %d empty folders\n", action, result.Files, result.Directories)
	for _, path := range result.Paths {
		fmt.Println(path)
	}
	return nil
}

func runRename(args []string) error {
	flags := flag.NewFlagSet("rename", flag.ContinueOnError)
	apply := flags.Bool("apply", false, "perform the proposed renames")
	preset := flags.String("preset", "trackno_trackname", "filename preset (trackno_trackname, trackno_artist_title, artist_title, title_only)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: snowsky rename [--apply] [--preset id] <folder>")
	}
	result, err := toolbox.Rename(flags.Arg(0), *preset, *apply)
	if err != nil {
		return err
	}
	for _, item := range result {
		fmt.Printf("%s -> %s [%s]\n", item.From, item.To, item.Status)
	}
	return nil
}

func runConvert(args []string) error {
	flags := flag.NewFlagSet("convert", flag.ContinueOnError)
	apply := flags.Bool("apply", false, "convert files (default is a dry run)")
	eq := flags.Bool("eq", false, "target 16-bit FLAC for EQ support")
	backupDir := flags.String("backup-dir", "", "copy originals here before conversion")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: snowsky convert [--apply] [--eq] [--backup-dir folder] <folder>")
	}
	result, err := toolbox.Convert(flags.Arg(0), *eq, *apply, *backupDir)
	if err != nil {
		return err
	}
	for _, item := range result {
		fmt.Printf("%s: %s\n", item.Path, item.Message)
	}
	return nil
}

func runLyrics(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: snowsky lyrics <extract|lookup> ...")
	}
	switch args[0] {
	case "extract":
		flags := flag.NewFlagSet("lyrics extract", flag.ContinueOnError)
		apply := flags.Bool("apply", false, "write .lrc sidecar files")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 1 {
			return errors.New("usage: snowsky lyrics extract [--apply] <folder>")
		}
		return toolbox.ExtractLyrics(flags.Arg(0), *apply)
	case "lookup":
		flags := flag.NewFlagSet("lyrics lookup", flag.ContinueOnError)
		apply := flags.Bool("apply", false, "write matching lyrics to .lrc sidecar files")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 1 {
			return errors.New("usage: snowsky lyrics lookup [--apply] <audio-file>")
		}
		path, err := filepath.Abs(flags.Arg(0))
		if err != nil {
			return err
		}
		return toolbox.LookupLyrics(path, *apply)
	default:
		return fmt.Errorf("unknown lyrics command %q", strings.TrimSpace(args[0]))
	}
}
