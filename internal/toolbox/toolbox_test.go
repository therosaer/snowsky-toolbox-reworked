package toolbox

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
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

func TestAnalyzeTracksUsesBoundedWorkersAndKeepsOrder(t *testing.T) {
	tracks := []Track{
		{Path: "first.mp3", Extension: ".mp3", Compatibility: Compatibility{Status: "UNKNOWN"}},
		{Path: "skip.lrc", Extension: ".lrc", Compatibility: Compatibility{Status: "UNKNOWN"}},
		{Path: "second.mp3", Extension: ".mp3", Compatibility: Compatibility{Status: "UNKNOWN"}},
		{Path: "third.mp3", Extension: ".mp3", Compatibility: Compatibility{Status: "UNKNOWN"}},
	}
	var active, maximum atomic.Int32
	probe := func(path string) (probeResult, error) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		data := probeResult{}
		err := json.Unmarshal([]byte(`{"format":{"tags":{"title":"`+filepath.Base(path)+`"}},"streams":[{"codec_type":"audio","codec_name":"mp3","sample_rate":"44100","channels":2}]}`), &data)
		return data, err
	}
	analyzeTracks(tracks, 2, probe)
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrent probes = %d, want 2", got)
	}
	if tracks[0].Title != "first.mp3" || tracks[2].Title != "second.mp3" || tracks[3].Title != "third.mp3" {
		t.Fatalf("probe results were not assigned to their original tracks: %+v", tracks)
	}
	if tracks[1].Compatibility.Status != "UNKNOWN" {
		t.Fatalf("LRC entry should not be probed: %+v", tracks[1])
	}
}

func TestScanSingleTrackRejectsSymlinkAndUnsupportedFile(t *testing.T) {
	root := t.TempDir()
	unsupported := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(unsupported, []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scanSingleTrack(unsupported); err == nil {
		t.Fatal("expected unsupported file extension to be rejected")
	}
	symlink := filepath.Join(root, "linked.mp3")
	if err := os.Symlink(unsupported, symlink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := scanSingleTrack(symlink); err == nil {
		t.Fatal("expected symlink input to be rejected")
	}
}
