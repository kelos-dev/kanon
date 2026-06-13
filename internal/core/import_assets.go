package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

func importInstructions(result *ImportResult, opts ImportOptions) error {
	if opts.InstructionPolicy == InstructionPolicySkip {
		return nil
	}
	codexPath := filepath.Join(opts.UserHome, ".codex", "AGENTS.md")
	claudePath := filepath.Join(opts.UserHome, ".claude", "CLAUDE.md")
	if opts.Project != "" {
		codexPath = filepath.Join(opts.Project, "AGENTS.md")
		claudePath = filepath.Join(opts.Project, "CLAUDE.md")
	}
	var codex, claude []byte
	var err error
	if opts.Agent == AgentAll || opts.Agent == AgentCodex {
		codex, err = readIfExists(codexPath)
		if err != nil {
			return err
		}
	}
	if opts.Agent == AgentAll || opts.Agent == AgentClaude {
		claude, err = readIfExists(claudePath)
		if err != nil {
			return err
		}
	}
	content, err := chooseInstructionContent(codex, claude, opts.InstructionPolicy)
	if err != nil {
		return err
	}
	if len(content) == 0 {
		return nil
	}
	rel := filepath.Join("instructions", "imported.md")
	result.Config.Instructions.Files = []string{rel}
	result.Files[rel] = content
	return nil
}

func chooseInstructionContent(codex, claude []byte, policy InstructionPolicy) ([]byte, error) {
	codex = bytes.TrimSpace(codex)
	claude = bytes.TrimSpace(claude)
	switch policy {
	case InstructionPolicyCodex:
		if len(codex) == 0 {
			return nil, fmt.Errorf("instruction policy %q selected but Codex AGENTS.md was not found", policy)
		}
		return append(codex, '\n'), nil
	case InstructionPolicyClaude:
		if len(claude) == 0 {
			return nil, fmt.Errorf("instruction policy %q selected but Claude CLAUDE.md was not found", policy)
		}
		return append(claude, '\n'), nil
	case InstructionPolicyMerge:
		return mergeInstructionContent(codex, claude), nil
	case InstructionPolicyAuto:
		if len(codex) == 0 {
			return appendIfNotEmpty(claude), nil
		}
		if len(claude) == 0 || bytes.Equal(codex, claude) {
			return append(codex, '\n'), nil
		}
		return nil, fmt.Errorf("AGENTS.md and CLAUDE.md differ; rerun with --instruction-policy codex, claude, merge, or skip")
	default:
		return nil, nil
	}
}

func mergeInstructionContent(codex, claude []byte) []byte {
	if len(codex) == 0 {
		return appendIfNotEmpty(claude)
	}
	if len(claude) == 0 || bytes.Equal(codex, claude) {
		return append(codex, '\n')
	}
	var out bytes.Buffer
	out.Write(codex)
	out.WriteString("\n\n")
	out.Write(claude)
	out.WriteByte('\n')
	return out.Bytes()
}

func appendIfNotEmpty(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return append(data, '\n')
}

func importSkills(result *ImportResult, opts ImportOptions) error {
	type root struct {
		path   string
		target string
	}
	var roots []root
	if opts.Agent == AgentAll || opts.Agent == AgentCodex {
		roots = append(roots,
			root{path: filepath.Join(opts.UserHome, ".agents", "skills"), target: AgentCodex},
			root{path: filepath.Join(opts.UserHome, ".codex", "skills"), target: AgentCodex},
		)
	}
	if opts.Agent == AgentAll || opts.Agent == AgentClaude {
		roots = append(roots, root{path: filepath.Join(opts.UserHome, ".claude", "skills"), target: AgentClaude})
	}
	seen := map[string]string{}
	for _, root := range roots {
		entries, err := os.ReadDir(root.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			source := filepath.Join(root.path, entry.Name())
			if _, err := os.Stat(filepath.Join(source, "SKILL.md")); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			hash, files, err := readSkillFiles(source)
			if err != nil {
				return err
			}
			name := uniqueSkillName(entry.Name(), root.target, hash, seen)
			if existingHash, ok := seen[name]; ok && existingHash == hash {
				addSkillTarget(result.Config, name, root.target)
				continue
			}
			seen[name] = hash
			result.Config.Skills = append(result.Config.Skills, Skill{Name: name, Targets: []string{root.target}})
			for rel, data := range files {
				result.Files[filepath.Join("skills", name, rel)] = data
			}
		}
	}
	return nil
}

func readSkillFiles(source string) (string, map[string][]byte, error) {
	files := map[string][]byte{}
	hash := sha256.New()
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = data
		hash.Write([]byte(rel))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(hash.Sum(nil)), files, nil
}

func uniqueSkillName(base, target, hash string, seen map[string]string) string {
	if existing, ok := seen[base]; !ok || existing == hash {
		return base
	}
	candidate := target + "-" + base
	if existing, ok := seen[candidate]; !ok || existing == hash {
		return candidate
	}
	for i := 2; ; i++ {
		candidate = fmt.Sprintf("%s-%s-%d", target, base, i)
		if existing, ok := seen[candidate]; !ok || existing == hash {
			return candidate
		}
	}
}

func addSkillTarget(cfg *Config, name, target string) {
	for i := range cfg.Skills {
		if cfg.Skills[i].Name == name {
			if !slices.Contains(cfg.Skills[i].Targets, target) {
				cfg.Skills[i].Targets = append(cfg.Skills[i].Targets, target)
			}
			cfg.Skills[i].Targets = normalizeImportedSkillTargets(cfg.Skills[i].Targets)
			return
		}
	}
}

func normalizeImportedSkillTargets(targets []string) []string {
	if slices.Contains(targets, AgentCodex) && slices.Contains(targets, AgentClaude) {
		return nil
	}
	return targets
}
