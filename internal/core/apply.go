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
	Agent       string    `json:"agent,omitempty"`
	Project     string    `json:"project,omitempty"`
	Prunable    bool      `json:"prunable,omitempty"`
}

type ApplyOptions struct {
	KanonHome string
	UserHome  string
	Adopt     bool
	Agent     string
	Project   string
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
	if err := planPrune(plan, state, files, opts); err != nil {
		return nil, nil, err
	}
	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].Path < plan.Changes[j].Path })
	sort.Slice(plan.Conflicts, func(i, j int) bool { return plan.Conflicts[i].Path < plan.Conflicts[j].Path })
	return plan, state, nil
}

// planPrune plans deletions for files kanon previously rendered (recorded in
// state as prunable) that the current render no longer produces, so the
// destination stays a pure projection of the source. It only considers state
// entries within the active agent/project scope, and only files marked
// prunable when applied (co-owned config files are never pruned).
func planPrune(plan *ApplyPlan, state *State, files []RenderedFile, opts ApplyOptions) error {
	rendered := make(map[string]bool, len(files))
	for _, file := range files {
		rendered[file.Path] = true
	}
	for path, record := range state.Files {
		if !record.Prunable || rendered[path] || !inScope(record, opts) {
			continue
		}
		existing, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			// File is already gone; drop the stale state record on apply.
			plan.Changes = append(plan.Changes, FileChange{
				Agent:        record.Agent,
				Path:         path,
				Action:       "delete",
				ExistingHash: record.Hash,
			})
			continue
		}
		if err != nil {
			return err
		}
		existingHash := HashBytes(existing)
		if !opts.Adopt && existingHash != record.Hash {
			plan.Conflicts = append(plan.Conflicts, FileConflict{
				Agent:  record.Agent,
				Path:   path,
				Reason: "managed file changed outside kanon; re-apply with --adopt to delete",
			})
			continue
		}
		plan.Changes = append(plan.Changes, FileChange{
			Agent:        record.Agent,
			Path:         path,
			Action:       "delete",
			ExistingHash: existingHash,
			Existing:     existing,
		})
	}
	return nil
}

// inScope reports whether a recorded file belongs to the agent/project that the
// current apply targets, so a scoped apply (for example --agent claude) never
// prunes another agent's or another project's files.
func inScope(record StateFile, opts ApplyOptions) bool {
	if record.Project != opts.Project {
		return false
	}
	return opts.Agent == "" || opts.Agent == AgentAll || record.Agent == opts.Agent
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
		if change.Action == "delete" {
			if err := backupExisting(backupRoot, change.Path, change.Existing); err != nil {
				return err
			}
			if err := os.Remove(change.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			removeEmptyParents(change.Path, opts.KanonHome, opts.UserHome, opts.Project)
			delete(state.Files, change.Path)
			continue
		}
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
			Agent:       change.File.Agent,
			Project:     opts.Project,
			Prunable:    change.File.Prunable,
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

// removeEmptyParents deletes now-empty ancestor directories of a pruned file,
// walking up until it hits a non-empty directory or a protected root, so that
// pruning the last file of a skill leaves no empty skills/<name>/ directory
// behind. The protected roots (Kanon home, user home, project) are never
// removed even when empty, bounding the walk so a scoped apply can never strip
// the destination root.
func removeEmptyParents(path string, roots ...string) {
	protected := make(map[string]bool, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		if abs, err := filepath.Abs(root); err == nil {
			protected[abs] = true
		}
	}
	dir := filepath.Dir(path)
	for {
		abs, err := filepath.Abs(dir)
		if err != nil || protected[abs] {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
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
