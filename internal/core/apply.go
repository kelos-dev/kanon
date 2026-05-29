package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type State struct {
	Files map[string]StateFile `json:"files"`
}

type StateFile struct {
	Hash        string    `json:"hash"`
	LastApplied time.Time `json:"lastApplied"`
}

type ApplyOptions struct {
	KanonHome string
	Adopt     bool
}

type ApplyPlan struct {
	Changes   []FileChange
	Conflicts []FileConflict
}

type FileChange struct {
	Agent        string
	Path         string
	Action       string
	ExistingHash string
	DesiredHash  string
	File         RenderedFile
	Existing     []byte
}

type FileConflict struct {
	Agent  string
	Path   string
	Reason string
}

func PlanFiles(files []RenderedFile, opts ApplyOptions) (*ApplyPlan, *State, error) {
	state, err := LoadState(opts.KanonHome)
	if err != nil {
		return nil, nil, err
	}
	plan := &ApplyPlan{}
	for _, file := range files {
		desiredHash := HashBytes(file.Content)
		existing, err := os.ReadFile(file.Path)
		if errors.Is(err, os.ErrNotExist) {
			plan.Changes = append(plan.Changes, FileChange{
				Agent:       file.Agent,
				Path:        file.Path,
				Action:      "create",
				DesiredHash: desiredHash,
				File:        file,
			})
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		existingHash := HashBytes(existing)
		if existingHash == desiredHash {
			continue
		}
		record, hasRecord := state.Files[file.Path]
		if !opts.Adopt && (!hasRecord || record.Hash != existingHash) {
			reason := "existing file is not managed by kanon"
			if hasRecord {
				reason = "existing managed file changed outside kanon"
			}
			plan.Conflicts = append(plan.Conflicts, FileConflict{
				Agent:  file.Agent,
				Path:   file.Path,
				Reason: reason,
			})
			continue
		}
		plan.Changes = append(plan.Changes, FileChange{
			Agent:        file.Agent,
			Path:         file.Path,
			Action:       "update",
			ExistingHash: existingHash,
			DesiredHash:  desiredHash,
			File:         file,
			Existing:     existing,
		})
	}
	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].Path < plan.Changes[j].Path })
	sort.Slice(plan.Conflicts, func(i, j int) bool { return plan.Conflicts[i].Path < plan.Conflicts[j].Path })
	return plan, state, nil
}

func ApplyFiles(plan *ApplyPlan, state *State, opts ApplyOptions) error {
	if len(plan.Conflicts) > 0 {
		return fmt.Errorf("cannot apply with %d conflict(s)", len(plan.Conflicts))
	}
	if state.Files == nil {
		state.Files = map[string]StateFile{}
	}
	backupRoot := filepath.Join(opts.KanonHome, ".kanon", "backups", time.Now().UTC().Format("20060102T150405Z"))
	for _, change := range plan.Changes {
		if change.Action == "update" {
			if err := backupExisting(backupRoot, change.Path, change.Existing); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(change.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(change.Path, change.File.Content, change.File.Mode); err != nil {
			return err
		}
		if err := os.Chmod(change.Path, change.File.Mode); err != nil {
			return err
		}
		state.Files[change.Path] = StateFile{
			Hash:        change.DesiredHash,
			LastApplied: time.Now().UTC(),
		}
	}
	return SaveState(opts.KanonHome, state)
}

func LoadState(home string) (*State, error) {
	path := StatePath(home)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Files: map[string]StateFile{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Files == nil {
		state.Files = map[string]StateFile{}
	}
	return &state, nil
}

func SaveState(home string, state *State) error {
	path := StatePath(home)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func StatePath(home string) string {
	return filepath.Join(home, ".kanon", "state.json")
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func backupExisting(root, path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	name := strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
	name = strings.NewReplacer(string(filepath.Separator), "__", ":", "_").Replace(name)
	target := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o600)
}
