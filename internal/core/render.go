package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func RenderAll(cfg *Config, opts TargetOptions) ([]RenderedFile, error) {
	var files []RenderedFile
	for _, adapter := range adaptersFor(cfg, opts.Agent) {
		rendered, err := adapter.Render(cfg, opts)
		if err != nil {
			return nil, err
		}
		files = append(files, rendered...)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Path == files[j].Path {
			return files[i].Agent < files[j].Agent
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

// FormatRender prints the computed target state: the agent-native files that
// the adapters compile from the source state, each preceded by its agent and
// destination path. It does not compare against what is currently on disk.
func FormatRender(files []RenderedFile) string {
	if len(files) == 0 {
		return "No target files.\n"
	}
	var out strings.Builder
	for i, file := range files {
		if i > 0 {
			out.WriteByte('\n')
		}
		fmt.Fprintf(&out, "==> [%s] %s\n", file.Agent, file.Path)
		out.Write(file.Content)
		if len(file.Content) > 0 && file.Content[len(file.Content)-1] != '\n' {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// allAdapters is the registry of every adapter Kanon knows how to render.
func allAdapters() []Adapter {
	return []Adapter{codexAdapter{}, claudeAdapter{}, geminiAdapter{}}
}

// defaultAgents is the set rendered when a config does not opt in via the
// top-level agents: key. It stays codex+claude so that existing kanon.yaml
// files keep rendering exactly those two and never start writing files for a
// newly added adapter on the next apply.
var defaultAgents = []string{AgentCodex, AgentClaude}

func adapterByName(name string) Adapter {
	for _, adapter := range allAdapters() {
		if adapter.Name() == name {
			return adapter
		}
	}
	return nil
}

// adaptersFor resolves which adapters to run. A specific --agent selects just
// that adapter; "all" intersects the registry with the config's enabled agent
// list (cfg.Agents), defaulting to codex+claude when that list is absent. A
// nil config (e.g. during import, before a kanon.yaml exists) uses the default
// set as well, preserving the prior import behavior.
func adaptersFor(cfg *Config, agent string) []Adapter {
	if agent != AgentAll {
		if adapter := adapterByName(agent); adapter != nil {
			return []Adapter{adapter}
		}
		return nil
	}
	names := defaultAgents
	if cfg != nil && len(cfg.Agents) > 0 {
		names = cfg.Agents
	}
	var adapters []Adapter
	for _, name := range names {
		if adapter := adapterByName(name); adapter != nil {
			adapters = append(adapters, adapter)
		}
	}
	return adapters
}

func readInstruction(home string, paths []string) ([]byte, error) {
	var out bytes.Buffer
	for _, rel := range paths {
		path := ResolvePath(home, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.Write(bytes.TrimSpace(data))
	}
	if out.Len() == 0 {
		return nil, nil
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func renderSkills(cfg *Config, opts TargetOptions, agent, targetRoot string) ([]RenderedFile, error) {
	var files []RenderedFile
	for _, skill := range cfg.Skills {
		if !enabled(skill.Enabled) || !HasTarget(skill.Targets, agent) {
			continue
		}
		name := skill.Name
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("skill name cannot be empty")
		}
		source, err := skillSourcePath(opts.KanonHome, name, skill)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(source)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("skill %q path must be a directory", name)
		}
		err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
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
			info, err := entry.Info()
			if err != nil {
				return err
			}
			files = append(files, RenderedFile{
				Agent:   agent,
				Path:    filepath.Join(targetRoot, name, rel),
				Content: data,
				Mode:    info.Mode().Perm(),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func skillSourcePath(home, name string, skill Skill) (string, error) {
	if skill.Source != nil {
		return materializeRemoteSkill(home, name, *skill.Source)
	}
	source := skill.Path
	if source == "" {
		source = filepath.Join("skills", name)
	}
	return ResolvePath(home, source), nil
}

func hooksForAgent(cfg *Config, agent string) map[string]any {
	grouped := map[string]any{}
	for _, hook := range cfg.Hooks {
		if !HasTarget(hook.Targets, agent) {
			continue
		}
		event := hook.Event
		if event == "" {
			event = hook.Name
		}
		if event == "" {
			continue
		}
		handler := map[string]any{}
		if hook.Type != "" {
			handler["type"] = hook.Type
		} else if hook.Command != "" {
			handler["type"] = "command"
		}
		if hook.Command != "" {
			handler["command"] = hook.Command
		}
		if len(hook.Args) > 0 {
			handler["args"] = hook.Args
		}
		if hook.Timeout > 0 {
			handler["timeout"] = hook.Timeout
		}
		if hook.Async {
			handler["async"] = true
		}
		item := map[string]any{"hooks": []any{handler}}
		if hook.Matcher != "" {
			item["matcher"] = hook.Matcher
		}
		grouped[event] = appendAny(grouped[event], item)
	}
	return grouped
}

func appendAny(existing any, item any) []any {
	if existing == nil {
		return []any{item}
	}
	if list, ok := existing.([]any); ok {
		return append(list, item)
	}
	return []any{existing, item}
}

func renderJSON(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func renderTOML(value any) ([]byte, error) {
	data, err := toml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func codexMCPServers(cfg *Config) map[string]any {
	out := map[string]any{}
	names := sortedMCPNames(cfg)
	for _, name := range names {
		server := cfg.MCP.Servers[name]
		if !enabled(server.Enabled) || !HasTarget(server.Targets, AgentCodex) {
			continue
		}
		item := map[string]any{}
		if server.URL != "" {
			item["url"] = server.URL
		}
		if server.Command != "" {
			item["command"] = server.Command
		}
		if len(server.Args) > 0 {
			item["args"] = server.Args
		}
		if len(server.Env) > 0 {
			item["env"] = server.Env
		}
		if len(server.EnvVars) > 0 {
			item["env_vars"] = server.EnvVars
		}
		if len(server.Headers) > 0 {
			item["http_headers"] = server.Headers
		}
		if len(server.EnvHeaders) > 0 {
			item["env_http_headers"] = server.EnvHeaders
		}
		if server.BearerTokenEnvVar != "" {
			item["bearer_token_env_var"] = server.BearerTokenEnvVar
		}
		if server.StartupTimeoutSec > 0 {
			item["startup_timeout_sec"] = server.StartupTimeoutSec
		}
		if server.ToolTimeoutSec > 0 {
			item["tool_timeout_sec"] = server.ToolTimeoutSec
		}
		if len(server.EnabledTools) > 0 {
			item["enabled_tools"] = server.EnabledTools
		}
		if len(server.DisabledTools) > 0 {
			item["disabled_tools"] = server.DisabledTools
		}
		if server.DefaultApproval != "" {
			item["default_tool_approval"] = server.DefaultApproval
		}
		if len(server.Tools) > 0 {
			tools := map[string]any{}
			for toolName, policy := range server.Tools {
				tools[toolName] = map[string]any{
					"description":     policy.Description,
					"approval":        policy.Approval,
					"approval_prompt": policy.ApprovalPrompt,
				}
			}
			item["tools"] = tools
		}
		out[name] = item
	}
	return out
}

func claudeMCPServers(cfg *Config) map[string]any {
	out := map[string]any{}
	names := sortedMCPNames(cfg)
	for _, name := range names {
		server := cfg.MCP.Servers[name]
		if !enabled(server.Enabled) || !HasTarget(server.Targets, AgentClaude) {
			continue
		}
		item := map[string]any{}
		if server.Type != "" {
			item["type"] = server.Type
		} else if server.URL != "" {
			item["type"] = "http"
		} else {
			item["type"] = "stdio"
		}
		if server.URL != "" {
			item["url"] = server.URL
		}
		if server.Command != "" {
			item["command"] = server.Command
		}
		if len(server.Args) > 0 {
			item["args"] = server.Args
		}
		if len(server.Env) > 0 {
			item["env"] = server.Env
		}
		if len(server.Headers) > 0 {
			item["headers"] = server.Headers
		}
		if len(server.EnvHeaders) > 0 {
			headers := map[string]string{}
			if existing, ok := item["headers"].(map[string]string); ok {
				for key, value := range existing {
					headers[key] = value
				}
			}
			for key, value := range server.EnvHeaders {
				headers[key] = fmt.Sprintf("${%s}", value)
			}
			item["headers"] = headers
		}
		if server.StartupTimeoutSec > 0 {
			item["timeout"] = server.StartupTimeoutSec
		}
		out[name] = item
	}
	return out
}

func geminiMCPServers(cfg *Config) map[string]any {
	out := map[string]any{}
	names := sortedMCPNames(cfg)
	for _, name := range names {
		server := cfg.MCP.Servers[name]
		if !enabled(server.Enabled) || !HasTarget(server.Targets, AgentGemini) {
			continue
		}
		item := map[string]any{}
		if server.Command != "" {
			item["command"] = server.Command
		}
		if len(server.Args) > 0 {
			item["args"] = server.Args
		}
		if len(server.Env) > 0 {
			item["env"] = server.Env
		}
		if server.URL != "" {
			// Gemini CLI distinguishes streamable HTTP (httpUrl) from SSE (url).
			if server.Type == "sse" {
				item["url"] = server.URL
			} else {
				item["httpUrl"] = server.URL
			}
		}
		if len(server.Headers) > 0 {
			item["headers"] = server.Headers
		}
		if len(server.EnvHeaders) > 0 {
			headers := map[string]string{}
			if existing, ok := item["headers"].(map[string]string); ok {
				for key, value := range existing {
					headers[key] = value
				}
			}
			for key, value := range server.EnvHeaders {
				headers[key] = fmt.Sprintf("${%s}", value)
			}
			item["headers"] = headers
		}
		if len(server.EnabledTools) > 0 {
			item["includeTools"] = server.EnabledTools
		}
		if len(server.DisabledTools) > 0 {
			item["excludeTools"] = server.DisabledTools
		}
		out[name] = item
	}
	return out
}

func sortedMCPNames(cfg *Config) []string {
	names := make([]string, 0, len(cfg.MCP.Servers))
	for name := range cfg.MCP.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
