package core

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

type claudeAdapter struct{}

type claudeDestination struct {
	targetRoot      string
	instructionPath string
	mcpPath         string
	skillRoot       string
}

func (claudeAdapter) Name() string {
	return AgentClaude
}

func (claudeAdapter) Render(cfg *Config, opts TargetOptions) ([]RenderedFile, error) {
	var files []RenderedFile
	dest := claudeDestinationFor(opts)

	instructions, err := readInstruction(opts.KanonHome, cfg.Instructions.Files)
	if err != nil {
		return nil, err
	}
	if len(instructions) > 0 {
		files = append(files, RenderedFile{
			Agent:   AgentClaude,
			Path:    dest.instructionPath,
			Content: instructions,
			Mode:    0o644,
		})
	}

	settings := map[string]any{}
	if hooks := hooksForAgent(cfg, AgentClaude); len(hooks) > 0 {
		settings["hooks"] = hooks
	}
	if len(settings) > 0 {
		data, err := renderJSON(settings)
		if err != nil {
			return nil, err
		}
		files = append(files, RenderedFile{
			Agent:   AgentClaude,
			Path:    filepath.Join(dest.targetRoot, "settings.json"),
			Content: data,
			Mode:    0o644,
			Merge:   FileMergeClaudeSettings,
		})
	}

	claudeJSON := map[string]any{}
	if servers := claudeMCPServers(cfg); len(servers) > 0 {
		claudeJSON["mcpServers"] = servers
	}
	if len(claudeJSON) > 0 {
		data, err := renderJSON(claudeJSON)
		if err != nil {
			return nil, err
		}
		files = append(files, RenderedFile{
			Agent:   AgentClaude,
			Path:    dest.mcpPath,
			Content: data,
			Mode:    0o644,
			Merge:   FileMergeClaudeMCP,
		})
	}

	skills, err := renderSkills(cfg, opts, AgentClaude, dest.skillRoot)
	if err != nil {
		return nil, err
	}
	files = append(files, skills...)
	return files, nil
}

func (claudeAdapter) Import(opts ImportOptions) (*ImportResult, error) {
	targetRoot := filepath.Join(opts.UserHome, ".claude")
	claudeJSONPath := filepath.Join(opts.UserHome, ".claude.json")
	if opts.Project != "" {
		targetRoot = filepath.Join(opts.Project, ".claude")
		claudeJSONPath = filepath.Join(opts.Project, ".mcp.json")
	}
	result := &ImportResult{
		Config: &Config{
			Version: 1,
		},
		Files: map[string][]byte{},
	}
	settingsPath := filepath.Join(targetRoot, "settings.json")
	if data, err := readIfExists(settingsPath); err == nil && len(data) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse %s: %v", settingsPath, err))
		} else {
			cleaned := sanitizeMap(raw, sanitizeContext{
				policy:   opts.SecretPolicy,
				warnings: &result.Warnings,
			}, "claude.settings")
			normalizeClaudeSettings(result.Config, cleaned, &result.Warnings)
			warnSkippedMap(&result.Warnings, "claude settings", cleaned)
		}
	} else if err != nil {
		return nil, err
	}
	if data, err := readIfExists(claudeJSONPath); err == nil && len(data) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse %s: %v", claudeJSONPath, err))
		} else {
			cleaned := sanitizeMap(raw, sanitizeContext{
				policy:   opts.SecretPolicy,
				warnings: &result.Warnings,
			}, "claude.claude_json")
			normalizeClaudeJSON(result.Config, cleaned, &result.Warnings)
			warnSkippedMap(&result.Warnings, "claude json", cleaned)
		}
	} else if err != nil {
		return nil, err
	}
	return result, nil
}

func claudeDestinationFor(opts TargetOptions) claudeDestination {
	dest := claudeDestination{
		targetRoot:      filepath.Join(opts.UserHome, ".claude"),
		instructionPath: filepath.Join(opts.UserHome, ".claude", "CLAUDE.md"),
		mcpPath:         filepath.Join(opts.UserHome, ".claude.json"),
		skillRoot:       filepath.Join(opts.UserHome, ".claude", "skills"),
	}
	if opts.Project != "" {
		dest.targetRoot = filepath.Join(opts.Project, ".claude")
		dest.instructionPath = filepath.Join(opts.Project, "CLAUDE.md")
		dest.mcpPath = filepath.Join(opts.Project, ".mcp.json")
		dest.skillRoot = filepath.Join(opts.Project, ".claude", "skills")
	}
	return dest
}
