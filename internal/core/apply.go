package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

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

func PlanFiles(files []RenderedFile, _ ApplyOptions) (*ApplyPlan, error) {
	plan := &ApplyPlan{}
	for _, file := range files {
		desired := file.Content
		existing, err := os.ReadFile(file.Path)
		if errors.Is(err, os.ErrNotExist) {
			plan.Changes = append(plan.Changes, FileChange{
				Agent:       file.Agent,
				Path:        file.Path,
				Action:      "create",
				DesiredHash: HashBytes(desired),
				File:        file,
			})
			continue
		}
		if err != nil {
			return nil, err
		}
		if file.Merge != FileMergeReplace {
			desired, err = MergeRenderedContent(file.Merge, file.Path, existing, file.Content)
			if err != nil {
				return nil, err
			}
			file.Content = desired
		}
		existingHash := HashBytes(existing)
		desiredHash := HashBytes(desired)
		if existingHash == desiredHash {
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
	return plan, nil
}

func ApplyFiles(plan *ApplyPlan, _ ApplyOptions) error {
	if len(plan.Conflicts) > 0 {
		return fmt.Errorf("cannot apply with %d conflict(s)", len(plan.Conflicts))
	}
	for _, change := range plan.Changes {
		switch change.Action {
		case "create", "update":
		default:
			return fmt.Errorf("unsupported file action %q for %s", change.Action, change.Path)
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
	}
	return nil
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
