package core

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

type codexAdapter struct{}

func (codexAdapter) Name() string {
	return AgentCodex
}

func (codexAdapter) Render(cfg *Config, opts TargetOptions) ([]RenderedFile, error) {
	var files []RenderedFile
	targetRoot := filepath.Join(opts.UserHome, ".codex")
	instructionPath := filepath.Join(targetRoot, "AGENTS.md")
	skillRoot := filepath.Join(opts.UserHome, ".agents", "skills")
	if opts.Project != "" {
		targetRoot = filepath.Join(opts.Project, ".codex")
		instructionPath = filepath.Join(opts.Project, "AGENTS.md")
		skillRoot = filepath.Join(opts.Project, ".agents", "skills")
	}

	instructions, err := readInstruction(opts.KanonHome, cfg.Instructions.Files)
	if err != nil {
		return nil, err
	}
	if len(instructions) > 0 {
		files = append(files, RenderedFile{
			Agent:    AgentCodex,
			Path:     instructionPath,
			Content:  instructions,
			Mode:     0o644,
			Prunable: true,
		})
	}

	configDoc := map[string]any{}
	if servers := codexMCPServers(cfg); len(servers) > 0 {
		configDoc["mcp_servers"] = servers
	}
	if len(configDoc) > 0 {
		data, err := renderTOML(configDoc)
		if err != nil {
			return nil, err
		}
		files = append(files, RenderedFile{
			Agent:   AgentCodex,
			Path:    filepath.Join(targetRoot, "config.toml"),
			Content: data,
			Mode:    0o644,
		})
	}

	hooks := hooksForAgent(cfg, AgentCodex)
	if len(hooks) > 0 {
		data, err := renderJSON(map[string]any{"hooks": hooks})
		if err != nil {
			return nil, err
		}
		files = append(files, RenderedFile{
			Agent:    AgentCodex,
			Path:     filepath.Join(targetRoot, "hooks.json"),
			Content:  data,
			Mode:     0o644,
			Prunable: true,
		})
	}

	skills, err := renderSkills(cfg, opts, AgentCodex, skillRoot)
	if err != nil {
		return nil, err
	}
	files = append(files, skills...)
	return files, nil
}

func (codexAdapter) Import(opts ImportOptions) (*ImportResult, error) {
	targetRoot := filepath.Join(opts.UserHome, ".codex")
	if opts.Project != "" {
		targetRoot = filepath.Join(opts.Project, ".codex")
	}
	result := &ImportResult{
		Config: &Config{
			Version: 1,
		},
		Files: map[string][]byte{},
	}
	configPath := filepath.Join(targetRoot, "config.toml")
	if data, err := readIfExists(configPath); err == nil && len(data) > 0 {
		var raw map[string]any
		if err := tomlUnmarshal(data, &raw); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse %s: %v", configPath, err))
		} else {
			cleaned := sanitizeMap(raw, sanitizeContext{
				policy:   opts.SecretPolicy,
				warnings: &result.Warnings,
			}, "codex.config")
			normalizeCodexConfig(result.Config, cleaned, &result.Warnings)
			warnSkippedMap(&result.Warnings, "codex config", cleaned)
		}
	} else if err != nil {
		return nil, err
	}
	hooksPath := filepath.Join(targetRoot, "hooks.json")
	if data, err := readIfExists(hooksPath); err == nil && len(data) > 0 {
		var hooks map[string]any
		if err := json.Unmarshal(data, &hooks); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse %s: %v", hooksPath, err))
		} else {
			cleaned := sanitizeMap(hooks, sanitizeContext{
				policy:   opts.SecretPolicy,
				warnings: &result.Warnings,
			}, "codex.hooks")
			normalizeCodexHooks(result.Config, cleaned, &result.Warnings)
			warnSkippedMap(&result.Warnings, "codex hooks", cleaned)
		}
	} else if err != nil {
		return nil, err
	}
	return result, nil
}
