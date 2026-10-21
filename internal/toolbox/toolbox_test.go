package toolbox

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateCompatibility(t *testing.T) {
	tests := []struct {
		name  string
		track Track
		want  string
	}{
		{"supported mp3", Track{Path: "song.mp3", Extension: ".mp3", Codec: "mp3"}, "COMPATIBLE"},
		{"unsupported opus", Track{Path: "song.opus", Extension: ".opus", Codec: "opus"}, "UNSUPPORTED"},
		{"rate limit", Track{Path: "song.flac", Extension: ".flac", Codec: "flac", SampleRate: 384000}, "UNSUPPORTED"},
		{"visual filename limit", Track{Path: "song😀.mp3", Extension: ".mp3", Codec: "mp3"}, "LIMITED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := EvaluateCompatibility(test.track).Status; got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBackupCreatesArchiveAndRejectsNestedDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "library")
	if err := os.MkdirAll(filepath.Join(source, "disc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "disc", "track.mp3"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "library.zip")
	result, err := Backup(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 1 {
		t.Fatalf("archived files = %d, want 1", result.Files)
	}
	archive, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	found := false
	for _, file := range archive.File {
		if file.Name == "library/disc/track.mp3" {
			found = true
		}
	}
	if !found {
		t.Fatal("archive did not include the source file")
	}
	if _, err := Backup(source, filepath.Join(source, "nested.zip")); err == nil {
		t.Fatal("expected backup inside source to be rejected")
	}
}

func TestCleanupDryRunAndApply(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, ".DS_Store")
	ordinary := filepath.Join(root, ".keep")
	emptyDir := filepath.Join(root, "empty")
	if err := os.WriteFile(cache, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ordinary, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dryRun, err := Cleanup(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.Files != 1 {
		t.Fatalf("dry-run files = %d, want 1", dryRun.Files)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("dry-run removed cache file")
	}
	applied, err := Cleanup(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Files != 1 || applied.Directories != 1 {
		t.Fatalf("apply result = %+v, want one file and one directory", applied)
	}
	if _, err := os.Stat(ordinary); err != nil {
		t.Fatal("cleanup removed unrelated hidden file")
	}
}

func TestTransferCopiesWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceFile := filepath.Join(source, "track.mp3")
	if err := os.WriteFile(sourceFile, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "track.mp3"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	dryRun, err := Transfer(source, destination, "copy", false)
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.Files != 0 || dryRun.SkippedExisting != 1 {
		t.Fatalf("dry-run result = %+v, want one existing file skipped", dryRun)
	}
	result, err := Transfer(source, destination, "copy", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedExisting != 1 {
		t.Fatalf("apply result = %+v, want one existing file skipped", result)
	}
	data, err := os.ReadFile(filepath.Join(destination, "track.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing" {
		t.Fatal("transfer overwrote an existing destination file")
	}
}

func TestJPEGScanType(t *testing.T) {
	progressive := []byte{0xff, 0xd8, 0xff, 0xc2, 0x00, 0x0b, 0x08, 0x00, 0x01, 0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xd9}
	baseline := []byte{0xff, 0xd8, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01, 0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xd9}
	if !jpegIsProgressive(progressive) {
		t.Fatal("progressive JPEG marker was not detected")
	}
	if jpegIsProgressive(baseline) {
		t.Fatal("baseline JPEG was incorrectly marked progressive")
	}
}
