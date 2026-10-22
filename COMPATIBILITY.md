# CLI compatibility checks

This guide describes the checks performed by `snowsky scan` in the Go CLI. It is intentionally narrower than the device research in [`device_spec/SNOWSKY-FIRMWARE.md`](device_spec/SNOWSKY-FIRMWARE.md). The compatibility result is an estimate based on file metadata and observed device behavior, not a guarantee that a track will play or display correctly.

## Scan coverage

The scanner recognizes these extensions (case-insensitively):

`.mp1`, `.mp2`, `.mp3`, `.wma`, `.wav`, `.ape`, `.fla`, `.flac`, `.aac`, `.m4a`, `.ogg`, `.mp4`, `.3gp`, `.dff`, `.dsf`, `.cue`, `.lrc`, `.aiff`, `.opus`, and `.wv`.

Other files are omitted from scan results, not marked unsupported. `.cue` and `.lrc` sidecars are listed but are not probed as audio, so their compatibility remains `UNKNOWN`.

## Result meanings

| Status | Meaning |
| --- | --- |
| `COMPATIBLE` | The recognized codec and the limits checked by the CLI passed. This does not guarantee playback on every device or firmware version. |
| `LIMITED` | The CLI found a known display or playback caveat, such as an unsupported filename character or multichannel WAV. |
| `UNSUPPORTED` | The codec/container combination or a checked technical limit is not supported by the CLI's device profile. |
| `UNKNOWN` | No audio codec could be identified, probing failed, or the entry is a sidecar that is not probed. |

## Audio rules

The file extension must match a codec the CLI knows for that container:

| Extension(s) | Accepted codec(s) |
| --- | --- |
| `.mp1` | `mp1` |
| `.mp2` | `mp2` |
| `.mp3` | `mp3` |
| `.flac`, `.fla` | `flac` |
| `.wav` | `pcm_*`, `adpcm_ms`, or `adpcm_ima_wav` |
| `.ape` | `ape` |
| `.dsf`, `.dff` | A codec name containing `dsd`, or `pcm_dsd` |
| `.m4a`, `.mp4`, `.3gp` | `aac` or `alac` |
| `.ogg` | `vorbis` or `flac` |
| `.wma` | A codec beginning with `wmav`, or `wmalossless` |
| `.aac` | `aac` |

The CLI additionally applies these limits:

- FLAC/FLaC, WAV, and APE are marked `UNSUPPORTED` above 192 kHz or 24 bits per sample.
- DSF/DFF is marked `UNSUPPORTED` when FFprobe reports a sample rate that is not a multiple of 44.1 kHz.
- Multichannel WAV (more than two channels) is marked `LIMITED`.
- OPUS, AIFF, AIF, and WV files are marked `UNSUPPORTED` because they do not appear in the tested device browser.

The scan does not enforce sample-rate or bit-depth limits on MP3, AAC, M4A, OGG, or WMA. It also does not inspect every property used by the firmware. Check the file on the target device when results matter.

## Filename display

Emoji and several pictographic Unicode ranges are marked `LIMITED`. The checker also marks filenames containing characters from selected complex scripts—Devanagari, Bengali, Khmer, Burmese, and related ranges—as `LIMITED`. The audio may still play even when the device browser cannot render the filename properly. These checks are character-range heuristics, not complete Unicode-script detection.

## Embedded artwork

Artwork is checked separately with `snowsky art inspect`; the compatibility status shown by `scan` does not incorporate artwork results.

- Embedded JPEG (`mjpeg`) at up to 1000 × 1000 pixels is considered compatible unless it is progressive.
- Progressive JPEG is marked `UNSUPPORTED`.
- Non-JPEG artwork is marked `UNSUPPORTED`.
- Artwork larger than 1000 pixels in either dimension is marked `LIMITED` because it may load slowly; the progressive-JPEG check takes precedence and marks progressive images `UNSUPPORTED`.
- Tracks without embedded artwork are reported as `MISSING`.

`art fix` can re-encode and resize artwork in MP3, FLAC, and M4A-family containers. Applying a fix requires `--backup-dir`. Artwork download supports embedding into MP3, FLAC, and M4A-family containers and also requires a backup directory when applying.

## Tags and firmware findings

The Go CLI's compatibility scan does **not** validate tag encoding, Vorbis-comment order, or oversized comments. The device research indicates that core tags such as `TITLE`, `ARTIST`, `ALBUM`, `ALBUMARTIST`, `TRACKNUMBER`, `DISCNUMBER`, and `GENRE` matter to the device, and that oversized Vorbis comments before core tags can cause failures. See the [firmware analysis](device_spec/SNOWSKY-FIRMWARE.md) for those findings.

`metadata sanitize` is a separate edit operation: it keeps a core set of common tags and removes other tags when applied. Preview changes first, and use `--backup <path>` with `--apply` when you need a per-file original copy. It is not a firmware-aware tag-order validator.
