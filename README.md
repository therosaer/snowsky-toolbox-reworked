# Snowsky Toolbox Reworked

A Go command-line toolbox for checking and preparing music libraries for the Snowsky Echo Mini. It can inspect audio compatibility, convert files, manage metadata and artwork, handle lyrics, and perform common library file operations. Mainly made this because I didn't like the GUI of the original app, this has both a reworked GUI and CLI.

> **Project status:** The Go CLI is a source-built application; this repository does not currently publish Go CLI release binaries. The repository also retains an older Python/PySide6 desktop application. The existing GitHub release workflow packages that desktop app, not the Go CLI.

This is an unofficial community project and is not affiliated with or endorsed by Snowsky or its manufacturer.

## Requirements

- Go 1.24 or newer to build the CLI.
- `ffprobe` on `PATH` to inspect audio files. Scanning an empty folder does not require it.
- `ffmpeg` on `PATH` for conversion, applied metadata edits, artwork changes, and other operations that write audio files.
- Network access only for `lyrics lookup` and `art download`.

Install FFmpeg and FFprobe using your platform's package manager, or download them from [FFmpeg](https://ffmpeg.org/download.html).

## Build

From the repository root:

```sh
go build -o snowsky ./cmd/snowsky
./snowsky help
```

On Windows, use `go build -o snowsky.exe ./cmd/snowsky` and run `.\snowsky.exe`.

## Quick start

Back up your library before making changes. Most mutating commands are dry runs by default; inspect their output, then pass `--apply` to perform the changes. ZIP backup creates the archive immediately and does not require `--apply`.

```sh
# Inspect tracks and compatibility
./snowsky scan /path/to/music

# Save the scan result as JSON
./snowsky scan --json /path/to/music > scan.json

# Create a ZIP backup outside the source folder
./snowsky backup /path/to/music /path/to/music-backup.zip

# Preview a conversion, then apply it
./snowsky convert /path/to/music
./snowsky convert --apply --backup-dir /path/to/originals /path/to/music

# Preview a copy, then apply it
./snowsky transfer copy /path/to/music /path/to/backup/music
./snowsky transfer copy --apply /path/to/music /path/to/backup/music
```

## Commands

| Command | Purpose |
| --- | --- |
| `scan [--json] <folder>` | List recognized audio and sidecar files, probe tracks, and report compatibility. |
| `backup <folder> <backup.zip>` | Create a ZIP archive immediately. The destination must be outside the source folder and must not already exist. |
| `transfer <copy\|move> [--apply] <source> <destination>` | Preview or copy/move regular files. Existing destination files and symlinks are skipped. |
| `cleanup [--apply] <folder>` | Preview or remove common system files (`.DS_Store`, `Thumbs.db`, `desktop.ini`, `._*`) and empty directories. |
| `rename [--apply] [--preset id] <folder>` | Preview or rename audio files using metadata. The default preset is `trackno_trackname`. |
| `convert [--apply] [--eq] [--backup-dir folder] <folder>` | Preview or convert unsupported audio to FLAC. `--eq` also targets 16-bit/192 kHz-compatible FLAC. |
| `metadata list <audio-file>` | Print tags as JSON. |
| `metadata set [--apply] [--backup path] <file> <key> <value>` | Set one metadata field. |
| `metadata remove [--apply] [--backup path] <file> <key>` | Remove one metadata field. |
| `metadata sanitize [--apply] [--backup path] <file>` | Remove tags outside the core metadata set retained by the sanitizer. |
| `art inspect <folder>` | Report missing or incompatible embedded artwork. |
| `art fix [--apply] [--backup-dir folder] <folder>` | Preview or normalize embedded artwork. `--backup-dir` is required with `--apply`. |
| `art download --artist name --album title [--apply --backup-dir folder] <audio-file>` | Find cover art online and optionally embed it. Applying requires a backup directory. |
| `lyrics extract [--apply] <folder>` | Preview or extract embedded lyrics into `.lrc` sidecars. |
| `lyrics lookup [--apply] <audio-file>` | Find lyrics using LRCLIB and optionally write an `.lrc` sidecar. Existing sidecars are not overwritten. |
| `workflow run [--apply] <workflow.json> <folder>` | Run supported workflow steps from a JSON file. Mutating steps are dry runs unless applied. |

Use `./snowsky <command> --help` for command-specific flags and usage.

### Rename presets

Available preset IDs include `trackno_trackname`, `trackno_artist_title`, `trackno_artist_album_title`, `trackno_album_title`, `trackno_dash_title`, `trackno_dash_artist_title`, `trackno_dash_artist_album_title`, `artist_title`, `artist_album_title`, `album_title`, `artist_trackno_title`, and `title_only`. A preset that requires a tag missing from a track skips that track; conflicting target filenames are reported and not overwritten.

### Conversion and backups

`convert` only selects tracks the compatibility checker marks unsupported, except `--eq`, which also selects tracks over the EQ target of 16-bit or 192 kHz. Non-FLAC inputs are written as `.flac`; after a successful conversion the original is removed. FLAC inputs are replaced in place. `--backup-dir` copies each original before conversion; it is optional, so create a separate backup first if you need to preserve originals.

`metadata` and artwork writes replace files in place. Their `--backup`/`--backup-dir` options copy the original before writing. Artwork repair and download refuse to apply changes without a backup directory.

### Scan parallelism

The scanner probes tracks concurrently, preserves the scan order in its output, and uses up to 8 workers by default. Set `SNOWSKY_PROBE_WORKERS` to a positive integer to tune worker count (the CLI caps it at 32 and at the number of files). Lower values can help on slower removable drives; higher values may help on fast storage.

## Workflow JSON

Workflow files contain a name and an ordered list of steps. The CLI supports `backup`, `file_cleanup`, `file_rename`, `make_music_compatible`, `make_music_eq_compatible`, `lyrics_manager`, `metadata_manager`, and `fix_album_art`.

```json
{
  "name": "Prepare a library",
  "steps": [
    {
      "type": "backup",
      "config": {
        "backup_path": "/backups/music.zip"
      }
    },
    {
      "type": "make_music_compatible",
      "config": {}
    },
    {
      "type": "file_cleanup",
      "config": {}
    }
  ]
}
```

Run the workflow in preview mode first:

```sh
./snowsky workflow run workflow.json /path/to/music
./snowsky workflow run --apply workflow.json /path/to/music
```

The workflow runner is a limited CLI implementation, not a complete executor for every workflow action available in the older desktop app. Unsupported step types stop with an error.

## Compatibility

The CLI checks supported containers/codecs, selected sample-rate and bit-depth limits, multichannel WAV, and filename cases known to display poorly. It does not classify every file type a device may encounter, and it does not currently check metadata-tag ordering as part of `scan`.

See [COMPATIBILITY.md](COMPATIBILITY.md) for the exact CLI checks and their limitations. The [device specification notes](device_spec/SNOWSKY-FIRMWARE.md) record broader firmware research and should be treated as research findings, not as a complete description of current CLI behavior.

## Older desktop application

The Python/PySide6 desktop application remains in the source tree (`main.py`, `src/`). Its source setup and platform release packaging are separate from the Go CLI. For the desktop app, use Python 3.12 or newer, install `requirements.txt`, and provide `ffmpeg`/`ffprobe` on `PATH` when running from source:

```sh
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
python main.py
```

On Windows PowerShell, activate with `.\.venv\Scripts\Activate.ps1` and run `python main.py`.

## Contributing

Changes to compatibility behavior should update the related tests and [COMPATIBILITY.md](COMPATIBILITY.md). Run the Go tests with:

```sh
go test ./...
```

Please report bugs through the repository's [issue tracker](https://github.com/therosaer/snowsky-toolbox-reworked/issues). For security concerns, follow [SECURITY.md](SECURITY.md) and report them privately.
