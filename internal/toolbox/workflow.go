package toolbox

import (
	"encoding/json"
	"fmt"
	"os"
)

type workflowDefinition struct {
	Name  string         `json:"name"`
	Steps []workflowStep `json:"steps"`
}

type workflowStep struct {
	Type   string                 `json:"type"`
	Config map[string]interface{} `json:"config"`
}

func RunWorkflow(workflowPath, target string, apply bool) error {
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		return err
	}
	var workflow workflowDefinition
	if err := json.Unmarshal(data, &workflow); err != nil {
		return fmt.Errorf("parse workflow JSON: %w", err)
	}
	if len(workflow.Steps) == 0 {
		return fmt.Errorf("workflow contains no steps")
	}
	for index, step := range workflow.Steps {
		fmt.Printf("Step %d/%d: %s\n", index+1, len(workflow.Steps), step.Type)
		switch step.Type {
		case "backup":
			backupPath := configString(step.Config, "backup_path")
			if backupPath == "" {
				return fmt.Errorf("step %d: backup_path is not configured", index+1)
			}
			if !apply {
				fmt.Printf("Would create backup at %s\n", backupPath)
				continue
			}
			result, err := Backup(target, backupPath)
			if err != nil {
				return fmt.Errorf("step %d: %w", index+1, err)
			}
			fmt.Printf("Backed up %d files to %s\n", result.Files, result.Path)
		case "file_cleanup":
			result, err := Cleanup(target, apply)
			if err != nil {
				return fmt.Errorf("step %d: %w", index+1, err)
			}
			fmt.Printf("%s %d cleanup files and %d empty folders\n", actionVerb(apply), result.Files, result.Directories)
		case "file_rename":
			preset := configString(step.Config, "preset")
			if preset == "" {
				preset = "trackno_trackname"
			}
			results, err := Rename(target, preset, apply)
			if err != nil {
				return fmt.Errorf("step %d: %w", index+1, err)
			}
			fmt.Printf("%s %d rename candidates\n", actionVerb(apply), len(results))
		case "make_music_compatible", "make_music_eq_compatible":
			results, err := Convert(target, step.Type == "make_music_eq_compatible", apply, "")
			if err != nil {
				return fmt.Errorf("step %d: %w", index+1, err)
			}
			for _, result := range results {
				fmt.Printf("%s: %s\n", result.Path, result.Message)
			}
		case "lyrics_manager":
			if err := ExtractLyrics(target, apply); err != nil {
				return fmt.Errorf("step %d: %w", index+1, err)
			}
		case "metadata_manager":
			if err := workflowMetadata(target, step.Config, apply); err != nil {
				return fmt.Errorf("step %d: %w", index+1, err)
			}
		case "fix_album_art":
			backupDir := configString(step.Config, "backup_path")
			if apply && backupDir == "" {
				backupDir, err = os.MkdirTemp("", "snowsky-art-backup-*")
				if err != nil {
					return fmt.Errorf("step %d: create artwork backup folder: %w", index+1, err)
				}
				fmt.Printf("Artwork backups: %s\n", backupDir)
			}
			results, err := FixArtwork(target, apply, backupDir)
			if err != nil {
				return fmt.Errorf("step %d: %w", index+1, err)
			}
			for _, result := range results {
				fmt.Printf("%s: %s %s\n", result.Status, result.Path, result.Reason)
			}
		default:
			return fmt.Errorf("step %d: unknown or unsupported workflow action %q", index+1, step.Type)
		}
	}
	return nil
}

func workflowMetadata(root string, config map[string]interface{}, apply bool) error {
	key := configString(config, "tag_name")
	if key == "" {
		return fmt.Errorf("metadata step has no tag_name")
	}
	action := configString(config, "action")
	if action == "" {
		action = "Set"
	}
	value := configString(config, "tag_value")
	scan, err := Scan(root)
	if err != nil {
		return err
	}
	for _, track := range scan.Tracks {
		if track.Extension == ".lrc" || track.Extension == ".cue" {
			continue
		}
		mode := "set"
		if action == "remove" || action == "Remove" {
			mode = "remove"
		}
		if err := EditMetadata(track.Path, mode, key, value, apply, ""); err != nil {
			return fmt.Errorf("%s: %w", track.RelativePath, err)
		}
	}
	return nil
}

func configString(config map[string]interface{}, key string) string {
	if config == nil {
		return ""
	}
	value, _ := config[key].(string)
	return value
}

func actionVerb(apply bool) string {
	if apply {
		return "Processed"
	}
	return "Would process"
}
