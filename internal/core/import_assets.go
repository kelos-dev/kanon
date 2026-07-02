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

type instructionSource struct {
	agent    string
	path     string
	fallback bool
	content  []byte
}

func importInstructions(result *ImportResult, opts ImportOptions) error {
	if opts.InstructionPolicy == InstructionPolicySkip {
		return nil
	}
	var sources []instructionSource
	if opts.Agent == AgentAll || opts.Agent == AgentCodex {
		sources = append(sources, instructionSource{agent: AgentCodex, path: codexInstructionPath(opts)})
	}
	if opts.Agent == AgentAll || opts.Agent == AgentClaude {
		sources = append(sources, instructionSource{agent: AgentClaude, path: claudeInstructionPath(opts)})
	}
	if opts.Agent == AgentAll || opts.Agent == AgentOpenCode {
		sources = append(sources, openCodeInstructionSources(opts)...)
	}
	for i := range sources {
		data, err := readIfExists(sources[i].path)
		if err != nil {
			return err
		}
		sources[i].content = data
	}
	sources = effectiveInstructionSources(sources)
	content, err := chooseInstructionContentFromSources(sources, opts.InstructionPolicy)
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
	return chooseInstructionContentFromSources([]instructionSource{
		{agent: AgentCodex, content: codex},
		{agent: AgentClaude, content: claude},
	}, policy)
}

func effectiveInstructionSources(sources []instructionSource) []instructionSource {
	hasPrimary := map[string]bool{}
	for _, source := range sources {
		if !source.fallback && len(bytes.TrimSpace(source.content)) > 0 {
			hasPrimary[source.agent] = true
		}
	}
	out := sources[:0]
	for _, source := range sources {
		if source.fallback && hasPrimary[source.agent] {
			continue
		}
		out = append(out, source)
	}
	return out
}

func chooseInstructionContentFromSources(sources []instructionSource, policy InstructionPolicy) ([]byte, error) {
	for i := range sources {
		sources[i].content = bytes.TrimSpace(sources[i].content)
	}
	switch policy {
	case InstructionPolicyCodex:
		return selectedInstructionContent(sources, policy, AgentCodex)
	case InstructionPolicyClaude:
		return selectedInstructionContent(sources, policy, AgentClaude)
	case InstructionPolicyOpenCode:
		return selectedInstructionContent(sources, policy, AgentOpenCode)
	case InstructionPolicyMerge:
		return mergeInstructionSourceContent(sources), nil
	case InstructionPolicyAuto:
		var selected []instructionSource
		for _, source := range sources {
			if len(source.content) > 0 {
				selected = append(selected, source)
			}
		}
		if len(selected) == 0 {
			return nil, nil
		}
		first := selected[0].content
		for _, source := range selected[1:] {
			if !bytes.Equal(first, source.content) {
				return nil, fmt.Errorf("agent instruction files differ; rerun with --instruction-policy codex, claude, opencode, merge, or skip")
			}
		}
		return append(first, '\n'), nil
	default:
		return nil, nil
	}
}

func selectedInstructionContent(sources []instructionSource, policy InstructionPolicy, agent string) ([]byte, error) {
	for _, source := range sources {
		if source.agent == agent && len(source.content) > 0 {
			return append(source.content, '\n'), nil
		}
	}
	return nil, fmt.Errorf("instruction policy %q selected but %s was not found", policy, instructionLabel(agent))
}

func instructionLabel(agent string) string {
	switch agent {
	case AgentCodex:
		return "Codex AGENTS.md"
	case AgentClaude:
		return "Claude CLAUDE.md"
	case AgentOpenCode:
		return "OpenCode AGENTS.md"
	default:
		return "agent instructions"
	}
}

func mergeInstructionContent(codex, claude []byte) []byte {
	return mergeInstructionSourceContent([]instructionSource{
		{content: codex},
		{content: claude},
	})
}

func mergeInstructionSourceContent(sources []instructionSource) []byte {
	var contents [][]byte
	for _, source := range sources {
		content := bytes.TrimSpace(source.content)
		if len(content) == 0 {
			continue
		}
		duplicate := false
		for _, existing := range contents {
			if bytes.Equal(existing, content) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			contents = append(contents, content)
		}
	}
	if len(contents) == 0 {
		return nil
	}
	if len(contents) == 1 {
		return append(contents[0], '\n')
	}
	var out bytes.Buffer
	for i, content := range contents {
		if i > 0 {
			out.WriteString("\n\n")
		}
		out.Write(content)
	}
	out.WriteByte('\n')
	return out.Bytes()
}

func importSkills(result *ImportResult, opts ImportOptions) error {
	type root struct {
		path   string
		target string
	}
	var roots []root
	if opts.Agent == AgentAll || opts.Agent == AgentCodex {
		roots = append(roots,
			root{path: filepath.Join(skillImportBase(opts), ".agents", "skills"), target: AgentCodex},
			root{path: filepath.Join(skillImportBase(opts), ".codex", "skills"), target: AgentCodex},
		)
	}
	if opts.Agent == AgentAll || opts.Agent == AgentClaude {
		roots = append(roots, root{path: filepath.Join(skillImportBase(opts), ".claude", "skills"), target: AgentClaude})
	}
	if opts.Agent == AgentAll || opts.Agent == AgentOpenCode {
		for _, path := range openCodeSkillImportPaths(opts) {
			roots = append(roots, root{path: path, target: AgentOpenCode})
		}
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
			if len(cfg.Skills[i].Targets) == 0 {
				return
			}
			if !slices.Contains(cfg.Skills[i].Targets, target) {
				cfg.Skills[i].Targets = append(cfg.Skills[i].Targets, target)
			}
			cfg.Skills[i].Targets = normalizeImportedSkillTargets(cfg.Skills[i].Targets)
			return
		}
	}
}

func normalizeImportedSkillTargets(targets []string) []string {
	if slices.Contains(targets, AgentCodex) && slices.Contains(targets, AgentClaude) && slices.Contains(targets, AgentOpenCode) {
		return nil
	}
	return targets
}

func codexInstructionPath(opts ImportOptions) string {
	if opts.Project != "" {
		return filepath.Join(opts.Project, "AGENTS.md")
	}
	return filepath.Join(opts.UserHome, ".codex", "AGENTS.md")
}

func claudeInstructionPath(opts ImportOptions) string {
	if opts.Project != "" {
		return filepath.Join(opts.Project, "CLAUDE.md")
	}
	return filepath.Join(opts.UserHome, ".claude", "CLAUDE.md")
}

func openCodeInstructionSources(opts ImportOptions) []instructionSource {
	if opts.Project != "" {
		return []instructionSource{
			{agent: AgentOpenCode, path: filepath.Join(opts.Project, "AGENTS.md")},
			{agent: AgentOpenCode, path: filepath.Join(opts.Project, "CLAUDE.md"), fallback: true},
		}
	}
	return []instructionSource{
		{agent: AgentOpenCode, path: filepath.Join(opts.UserHome, ".config", "opencode", "AGENTS.md")},
		{agent: AgentOpenCode, path: filepath.Join(opts.UserHome, ".claude", "CLAUDE.md"), fallback: true},
	}
}

func skillImportBase(opts ImportOptions) string {
	if opts.Project != "" {
		return opts.Project
	}
	return opts.UserHome
}

func openCodeSkillImportPaths(opts ImportOptions) []string {
	if opts.Project != "" {
		return []string{
			filepath.Join(opts.Project, ".opencode", "skills"),
			filepath.Join(opts.Project, ".claude", "skills"),
			filepath.Join(opts.Project, ".agents", "skills"),
		}
	}
	return []string{
		filepath.Join(opts.UserHome, ".config", "opencode", "skills"),
		filepath.Join(opts.UserHome, ".claude", "skills"),
		filepath.Join(opts.UserHome, ".agents", "skills"),
	}
}
