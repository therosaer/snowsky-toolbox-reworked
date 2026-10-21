package toolbox

import (
	"path/filepath"
	"strings"
)

func EvaluateCompatibility(track Track) Compatibility {
	ext := strings.ToLower(track.Extension)
	if ext == ".scad" || ext == ".iso" || ext == ".dts" || ext == ".dtshd" {
		return Compatibility{Status: "UNSUPPORTED", Reason: "Audio format is not supported by the device"}
	}
	if ext == ".opus" || ext == ".aiff" || ext == ".aif" || ext == ".wv" {
		return Compatibility{Status: "UNSUPPORTED", Reason: "File format does not appear in the device browser"}
	}
	if reason := filenameIncompatibility(filepath.Base(track.Path)); reason != "" {
		return Compatibility{Status: "LIMITED", Reason: reason}
	}
	if track.Codec == "" {
		return Compatibility{Status: "UNKNOWN", Reason: "Audio codec could not be identified"}
	}
	if !codecMatchesExtension(ext, track.Codec) {
		return Compatibility{Status: "UNSUPPORTED", Reason: "Codec is not supported in this container"}
	}
	if (ext == ".flac" || ext == ".fla" || ext == ".wav" || ext == ".ape") && track.SampleRate > 192000 {
		return Compatibility{Status: "UNSUPPORTED", Reason: "Sample rate exceeds 192 kHz"}
	}
	if (ext == ".flac" || ext == ".fla" || ext == ".wav" || ext == ".ape") && track.BitsPerSample > 24 {
		return Compatibility{Status: "UNSUPPORTED", Reason: "Bit depth exceeds 24 bits"}
	}
	if (ext == ".dsf" || ext == ".dff") && track.SampleRate > 0 && track.SampleRate%44100 != 0 {
		return Compatibility{Status: "UNSUPPORTED", Reason: "DSD sample rate is not a supported DSD multiple"}
	}
	if track.Channels > 2 && ext == ".wav" {
		return Compatibility{Status: "LIMITED", Reason: "Multichannel WAV may not play correctly"}
	}
	return Compatibility{Status: "COMPATIBLE"}
}

func codecMatchesExtension(ext, codec string) bool {
	switch ext {
	case ".mp3":
		return codec == "mp3"
	case ".flac", ".fla":
		return codec == "flac"
	case ".wav":
		return strings.HasPrefix(codec, "pcm_") || codec == "adpcm_ms" || codec == "adpcm_ima_wav"
	case ".ape":
		return codec == "ape"
	case ".dsf", ".dff":
		return strings.Contains(codec, "dsd") || codec == "pcm_dsd"
	case ".m4a", ".m4b", ".m4p", ".mp4", ".3gp":
		return codec == "aac" || codec == "alac"
	case ".ogg", ".oga":
		return codec == "vorbis" || codec == "flac"
	case ".wma":
		return strings.HasPrefix(codec, "wmav") || codec == "wmalossless"
	case ".aac":
		return codec == "aac"
	case ".mp1", ".mp2":
		return codec == "mp1" || codec == "mp2"
	default:
		return true
	}
}

func filenameIncompatibility(name string) string {
	for _, r := range name {
		if r >= 0x1f300 && r <= 0x1f64f ||
			r >= 0x1f680 && r <= 0x1f6ff ||
			r >= 0x1f900 && r <= 0x1f9ff ||
			r >= 0x1fa70 && r <= 0x1faff ||
			r >= 0x2600 && r <= 0x26ff ||
			r >= 0x2700 && r <= 0x27bf {
			return "Filename contains emoji or unsupported pictographic symbols"
		}
		switch {
		case r >= 0x0900 && r <= 0x097f,
			r >= 0x0980 && r <= 0x09ff,
			r >= 0x1780 && r <= 0x17ff,
			r >= 0x19e0 && r <= 0x19ff,
			r >= 0x1000 && r <= 0x109f,
			r >= 0xaa60 && r <= 0xaa7f,
			r >= 0xa9e0 && r <= 0xa9ff:
			return "Filename contains a complex script not supported visually by the device"
		}
	}
	return ""
}
