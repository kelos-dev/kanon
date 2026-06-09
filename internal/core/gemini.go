package core

import (
	"fmt"
	"path/filepath"
)

type geminiAdapter struct{}

func (geminiAdapter) Name() string {
	return AgentGemini
}

func (geminiAdapter) Render(cfg *Config, opts TargetOptions) ([]RenderedFile, error) {
	var files []RenderedFile
	targetRoot := filepath.Join(opts.UserHome, ".gemini")
	instructionPath := filepath.Join(targetRoot, "GEMINI.md")
	skillRoot := filepath.Join(targetRoot, "skills")
	if opts.Project != "" {
		targetRoot = filepath.Join(opts.Project, ".gemini")
		instructionPath = filepath.Join(opts.Project, "GEMINI.md")
		skillRoot = filepath.Join(targetRoot, "skills")
	}

	instructions, err := readInstruction(opts.KanonHome, cfg.Instructions.Files)
	if err != nil {
		return nil, err
	}
	if len(instructions) > 0 {
		files = append(files, RenderedFile{
			Agent:   AgentGemini,
			Path:    instructionPath,
			Content: instructions,
			Mode:    0o644,
		})
	}

	settings := map[string]any{}
	if servers := geminiMCPServers(cfg); len(servers) > 0 {
		settings["mcpServers"] = servers
	}
	hooks, err := geminiHooks(cfg)
	if err != nil {
		return nil, err
	}
	if len(hooks) > 0 {
		settings["hooks"] = hooks
	}
	if len(settings) > 0 {
		data, err := renderJSON(settings)
		if err != nil {
			return nil, err
		}
		files = append(files, RenderedFile{
			Agent:   AgentGemini,
			Path:    filepath.Join(targetRoot, "settings.json"),
			Content: data,
			Mode:    0o644,
		})
	}

	skills, err := renderSkills(cfg, opts, AgentGemini, skillRoot)
	if err != nil {
		return nil, err
	}
	files = append(files, skills...)
	return files, nil
}

// Import is not yet supported for Gemini CLI. The adapter is render-only for
// now (see issue #7); lifting GEMINI.md + settings.json back into the neutral
// config is tracked as later work. It returns an empty result so that an
// explicit `kanon import --agent gemini` is a harmless no-op rather than an
// error.
func (geminiAdapter) Import(opts ImportOptions) (*ImportResult, error) {
	return &ImportResult{
		Config: &Config{Version: 1},
		Files:  map[string][]byte{},
	}, nil
}

func geminiHooks(cfg *Config) (map[string]any, error) {
	grouped := map[string]any{}
	for _, hook := range cfg.Hooks {
		if !HasTarget(hook.Targets, AgentGemini) {
			continue
		}
		event, item, err := geminiHookItem(hook)
		if err != nil {
			return nil, err
		}
		grouped[event] = appendAny(grouped[event], item)
	}
	return grouped, nil
}

func geminiHookItem(hook Hook) (string, map[string]any, error) {
	event := hook.Event
	if event == "" {
		event = hook.Name
	}
	geminiEvent, ok := geminiHookEvent(event)
	if !ok {
		return "", nil, fmt.Errorf("hook %q targets gemini with unsupported event %q", hook.Name, event)
	}
	if hook.Type != "" && hook.Type != "command" {
		return "", nil, fmt.Errorf("hook %q targets gemini with unsupported type %q", hook.Name, hook.Type)
	}
	if hook.Command == "" {
		return "", nil, fmt.Errorf("hook %q targets gemini and requires command", hook.Name)
	}
	if len(hook.Args) > 0 {
		return "", nil, fmt.Errorf("hook %q targets gemini: args are not supported; include arguments in command", hook.Name)
	}
	if hook.Async {
		return "", nil, fmt.Errorf("hook %q targets gemini: async hooks are not supported", hook.Name)
	}
	handler := map[string]any{
		"name":    hook.Name,
		"type":    "command",
		"command": hook.Command,
	}
	if hook.Timeout > 0 {
		handler["timeout"] = hook.Timeout
	}
	item := map[string]any{"hooks": []any{handler}}
	if hook.Matcher != "" {
		item["matcher"] = hook.Matcher
	}
	return geminiEvent, item, nil
}

func geminiHookEvent(event string) (string, bool) {
	switch event {
	case "PreToolUse":
		return "BeforeTool", true
	case "PostToolUse":
		return "AfterTool", true
	case "BeforeTool", "AfterTool",
		"BeforeAgent", "AfterAgent",
		"BeforeModel", "BeforeToolSelection", "AfterModel",
		"SessionStart", "SessionEnd", "Notification", "PreCompress":
		return event, true
	default:
		return "", false
	}
}
