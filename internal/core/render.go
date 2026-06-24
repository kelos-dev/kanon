package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
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
	rendered := map[string]string{}
	localOverrides := map[string]Skill{}
	claimedLocalNames := map[string]bool{}
	for _, skill := range cfg.Skills {
		if skill.Git != nil {
			continue
		}
		name, err := skillEntryName(skill)
		if err != nil {
			return nil, err
		}
		if name == "" {
			if !enabled(skill.Enabled) {
				continue
			}
			return nil, fmt.Errorf("skill name cannot be empty")
		}
		claimedLocalNames[name] = true
		localOverrides[name] = skill
	}
	localItems, err := localSkillDirectoryItems(opts.KanonHome)
	if err != nil {
		return nil, err
	}
	for _, item := range localItems {
		skill := localOverrides[item.Name]
		name, err := skillEntryName(skill)
		if err != nil {
			return nil, err
		}
		if name == "" && !claimedLocalNames[item.Name] {
			name = item.Name
		}
		if name == "" {
			continue
		}
		delete(localOverrides, item.Name)
		if !enabled(skill.Enabled) || !HasTarget(skill.Targets, agent) {
			continue
		}
		source := item.Path
		if skill.Path != "" {
			source = ResolvePath(opts.KanonHome, skill.Path)
		}
		if err := renderSkillDirectory(&files, rendered, agent, targetRoot, item.Name, source, fmt.Sprintf("skill %q", item.Name), ""); err != nil {
			return nil, err
		}
	}
	localNames := make([]string, 0, len(localOverrides))
	for name := range localOverrides {
		localNames = append(localNames, name)
	}
	sort.Strings(localNames)
	for _, name := range localNames {
		skill := localOverrides[name]
		if !enabled(skill.Enabled) || !HasTarget(skill.Targets, agent) {
			continue
		}
		source := localSkillSourcePath(opts.KanonHome, name, skill)
		if err := renderSkillDirectory(&files, rendered, agent, targetRoot, name, source, fmt.Sprintf("skill %q", name), ""); err != nil {
			return nil, err
		}
	}
	for _, skill := range cfg.Skills {
		if skill.Git == nil {
			continue
		}
		if !enabled(skill.Enabled) || !HasTarget(skill.Targets, agent) {
			continue
		}
		name, err := gitSkillProviderName(skill)
		if err != nil {
			return nil, fmt.Errorf("git skill provider: %w", err)
		}
		sourcePath, err := remoteSkillProviderPath(opts.KanonHome, name, *skill.Git, opts.SourceLock)
		if err != nil {
			return nil, err
		}
		items, err := selectedSkillDirectoryItems(name, sourcePath, skill.Include, skill.Exclude)
		if err != nil {
			return nil, err
		}
		owner := fmt.Sprintf("git skill provider %q", name)
		for _, item := range items {
			renderName := namespacedSkillName(name, item.Name)
			if err := renderSkillDirectory(&files, rendered, agent, targetRoot, renderName, item.Path, owner, renderName); err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}

func renderSkillDirectory(files *[]RenderedFile, rendered map[string]string, agent, targetRoot, name, source, owner, frontmatterName string) error {
	if existing, ok := rendered[name]; ok {
		return fmt.Errorf("skill %q from %s duplicates %s", name, owner, existing)
	}
	rendered[name] = owner
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("skill %q path must be a directory", name)
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
		if rel == "SKILL.md" && frontmatterName != "" {
			data, err = rewriteSkillFrontmatterName(data, frontmatterName)
			if err != nil {
				return fmt.Errorf("skill %q SKILL.md: %w", name, err)
			}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		*files = append(*files, RenderedFile{
			Agent:   agent,
			Path:    filepath.Join(targetRoot, name, rel),
			Content: data,
			Mode:    info.Mode().Perm(),
		})
		return nil
	})
	return err
}

func rewriteSkillFrontmatterName(data []byte, name string) ([]byte, error) {
	text := string(data)
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, errors.New("missing YAML frontmatter")
	}
	offset := len(lines[0])
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line != "---" && line != "..." {
			offset += len(lines[i])
			continue
		}
		var frontmatter map[string]any
		if err := yaml.Unmarshal([]byte(text[len(lines[0]):offset]), &frontmatter); err != nil {
			return nil, err
		}
		if frontmatter == nil {
			frontmatter = map[string]any{}
		}
		frontmatter["name"] = name
		encoded, err := yaml.Marshal(frontmatter)
		if err != nil {
			return nil, err
		}
		body := text[offset+len(lines[i]):]
		return []byte("---\n" + string(encoded) + "---\n" + body), nil
	}
	return nil, errors.New("unterminated YAML frontmatter")
}

func localSkillSourcePath(home, name string, skill Skill) string {
	source := skill.Path
	if source == "" {
		source = filepath.Join("skills", name)
	}
	return ResolvePath(home, source)
}

func remoteSkillProviderPath(home, id string, git GitSkill, lock *SourceLock) (string, error) {
	remote := gitSkillToRemoteSource(git)
	var lockedEntry *SourceLockEntry
	if entry, ok := sourceLockEntry(lock, gitSkillSourceOwner(id)); ok {
		if err := entryMatchesRemoteSource(entry, remote); err != nil {
			return "", err
		}
		lockedEntry = &entry
	}
	return materializeRemoteSkillSource(home, id, remote, lockedEntry)
}

func namespacedSkillName(providerID, skillName string) string {
	return providerID + ":" + skillName
}

func localSkillDirectoryItems(home string) ([]materializedSkillDirectoryItem, error) {
	root := filepath.Join(home, "skills")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("local skills: %w", err)
	}
	var items []materializedSkillDirectoryItem
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(path, "SKILL.md")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("skill %q: %w", name, err)
		}
		items = append(items, materializedSkillDirectoryItem{Name: name, Path: path})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func selectedSkillDirectoryItems(sourceName, root string, include, exclude []string) ([]materializedSkillDirectoryItem, error) {
	items, err := materializedSkillDirectoryItems(sourceName, root)
	if err != nil {
		return nil, err
	}
	byName := map[string]materializedSkillDirectoryItem{}
	for _, item := range items {
		byName[item.Name] = item
	}
	excluded := map[string]bool{}
	for _, skillName := range exclude {
		if _, ok := byName[skillName]; !ok {
			return nil, fmt.Errorf("git skill provider %q references unknown excluded skill %q", sourceName, skillName)
		}
		excluded[skillName] = true
	}
	if len(include) == 0 {
		var selected []materializedSkillDirectoryItem
		for _, item := range items {
			if !excluded[item.Name] {
				selected = append(selected, item)
			}
		}
		return selected, nil
	}
	selected := make([]materializedSkillDirectoryItem, 0, len(include))
	for _, skillName := range include {
		item, ok := byName[skillName]
		if !ok {
			return nil, fmt.Errorf("git skill provider %q references unknown included skill %q", sourceName, skillName)
		}
		if excluded[skillName] {
			continue
		}
		selected = append(selected, item)
	}
	return selected, nil
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
