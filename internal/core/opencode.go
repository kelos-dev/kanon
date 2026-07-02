package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var openCodeEnvRefPattern = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

type openCodeAdapter struct{}

type openCodeDestination struct {
	instructionPath string
	configPath      string
	skillRoot       string
}

func (openCodeAdapter) Name() string {
	return AgentOpenCode
}

func (openCodeAdapter) Render(cfg *Config, opts TargetOptions) ([]RenderedFile, error) {
	var files []RenderedFile
	dest := openCodeDestinationFor(opts)

	instructions, err := readInstruction(opts.KanonHome, cfg.Instructions.Files)
	if err != nil {
		return nil, err
	}
	if len(instructions) > 0 {
		files = append(files, RenderedFile{
			Agent:   AgentOpenCode,
			Path:    dest.instructionPath,
			Content: instructions,
			Mode:    0o644,
		})
	}

	configDoc := map[string]any{}
	if servers := openCodeMCPServers(cfg); len(servers) > 0 {
		configDoc["mcp"] = servers
	}
	if len(configDoc) > 0 {
		data, err := renderJSON(configDoc)
		if err != nil {
			return nil, err
		}
		files = append(files, RenderedFile{
			Agent:   AgentOpenCode,
			Path:    dest.configPath,
			Content: data,
			Mode:    0o644,
			Merge:   FileMergeOpenCodeConfig,
		})
	}

	skills, err := renderSkills(cfg, opts, AgentOpenCode, dest.skillRoot)
	if err != nil {
		return nil, err
	}
	files = append(files, skills...)
	return files, nil
}

func (openCodeAdapter) Import(opts ImportOptions) (*ImportResult, error) {
	configPath := openCodeDestinationFor(opts.TargetOptions).configPath
	result := &ImportResult{
		Config: &Config{
			Version: 1,
		},
		Files: map[string][]byte{},
	}
	if data, err := readIfExists(configPath); err == nil && len(data) > 0 {
		var raw map[string]any
		if err := openCodeJSONUnmarshal(data, &raw); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse %s: %v", configPath, err))
		} else {
			cleaned := sanitizeMap(raw, sanitizeContext{
				policy:   opts.SecretPolicy,
				warnings: &result.Warnings,
			}, "opencode.config")
			normalizeOpenCodeConfig(result.Config, cleaned, &result.Warnings)
			warnSkippedMap(&result.Warnings, "opencode config", cleaned)
		}
	} else if err != nil {
		return nil, err
	}
	return result, nil
}

func openCodeDestinationFor(opts TargetOptions) openCodeDestination {
	dest := openCodeDestination{
		instructionPath: filepath.Join(opts.UserHome, ".config", "opencode", "AGENTS.md"),
		configPath:      filepath.Join(opts.UserHome, ".config", "opencode", "opencode.json"),
		skillRoot:       filepath.Join(opts.UserHome, ".config", "opencode", "skills"),
	}
	if opts.Project != "" {
		dest.instructionPath = filepath.Join(opts.Project, "AGENTS.md")
		dest.configPath = filepath.Join(opts.Project, "opencode.json")
		dest.skillRoot = filepath.Join(opts.Project, ".opencode", "skills")
	}
	return dest
}

func openCodeMCPServers(cfg *Config) map[string]any {
	out := map[string]any{}
	names := sortedMCPNames(cfg)
	for _, name := range names {
		server := cfg.MCP.Servers[name]
		if !enabled(server.Enabled) || !HasTarget(server.Targets, AgentOpenCode) {
			continue
		}
		item := map[string]any{}
		if server.URL != "" {
			item["type"] = "remote"
			item["url"] = server.URL
			if headers := openCodeHeaders(server); len(headers) > 0 {
				item["headers"] = headers
			}
		} else {
			item["type"] = "local"
			item["command"] = append([]string{server.Command}, server.Args...)
			if len(server.Env) > 0 {
				item["environment"] = openCodeEnvironment(server.Env)
			}
		}
		if server.StartupTimeoutSec > 0 {
			item["timeout"] = server.StartupTimeoutSec * 1000
		}
		if server.OpenCodeEnabled != nil {
			item["enabled"] = *server.OpenCodeEnabled
		}
		out[name] = item
	}
	return out
}

func openCodeHeaders(server MCPServer) map[string]string {
	headers := map[string]string{}
	for key, value := range server.Headers {
		if envName, ok := openCodeEnvNameFromKanonRef(value); ok {
			headers[key] = fmt.Sprintf("{env:%s}", envName)
			continue
		}
		headers[key] = value
	}
	for key, value := range server.EnvHeaders {
		headers[key] = fmt.Sprintf("{env:%s}", value)
	}
	if server.BearerTokenEnvVar != "" {
		if _, ok := headers["Authorization"]; !ok {
			headers["Authorization"] = fmt.Sprintf("Bearer {env:%s}", server.BearerTokenEnvVar)
		}
	}
	return headers
}

func openCodeEnvironment(values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		if envName, ok := openCodeEnvNameFromKanonRef(value); ok {
			out[key] = fmt.Sprintf("{env:%s}", envName)
			continue
		}
		out[key] = value
	}
	return out
}

func normalizeOpenCodeConfig(cfg *Config, raw map[string]any, warnings *[]string) {
	if raw == nil {
		return
	}
	servers, ok := raw["mcp"].(map[string]any)
	if !ok {
		return
	}
	ensureMCPServers(cfg)
	for name, value := range servers {
		server, ok := normalizeOpenCodeMCPServer(name, value, warnings)
		if ok {
			cfg.MCP.Servers[name] = mergeMCPServer(cfg.MCP.Servers[name], server)
			delete(servers, name)
		}
	}
	if len(servers) == 0 {
		delete(raw, "mcp")
	}
}

func normalizeOpenCodeMCPServer(name string, value any, warnings *[]string) (MCPServer, bool) {
	raw, ok := value.(map[string]any)
	if !ok {
		appendNormalizeWarning(warnings, "left MCP server %q raw for %s because it is not an object", name, AgentOpenCode)
		return MCPServer{}, false
	}
	server := MCPServer{Targets: []string{AgentOpenCode}}
	remainder := copyMap(raw)
	if value, ok := stringValue(raw["type"]); ok {
		server.Type = value
		delete(remainder, "type")
	}
	if values, ok := stringList(raw["command"]); ok && len(values) > 0 {
		server.Command = values[0]
		server.Args = values[1:]
		delete(remainder, "command")
	} else if value, ok := stringValue(raw["command"]); ok {
		server.Command = value
		delete(remainder, "command")
	}
	if values, ok := stringMap(raw["environment"]); ok {
		server.Env = normalizeOpenCodeEnvMap(values)
		delete(remainder, "environment")
	}
	if values, ok := stringMap(raw["env"]); ok {
		server.Env = normalizeOpenCodeEnvMap(values)
		delete(remainder, "env")
	}
	if value, ok := stringValue(raw["url"]); ok {
		server.URL = value
		delete(remainder, "url")
	}
	if values, ok := stringMap(raw["headers"]); ok {
		server.Headers, server.EnvHeaders, server.BearerTokenEnvVar = splitOpenCodeHeaders(values)
		delete(remainder, "headers")
	}
	if value, ok := intValue(raw["timeout"]); ok {
		server.StartupTimeoutSec = millisecondsToSeconds(value)
		delete(remainder, "timeout")
	}
	if value, ok := boolValue(raw["enabled"]); ok {
		server.OpenCodeEnabled = &value
		delete(remainder, "enabled")
	}
	if server.Command == "" && server.URL == "" {
		appendNormalizeWarning(warnings, "left MCP server %q raw for %s because it has no command or url", name, AgentOpenCode)
		return MCPServer{}, false
	}
	warnSkippedMap(warnings, fmt.Sprintf("%s mcp server %q", AgentOpenCode, name), remainder)
	return server, true
}

func splitOpenCodeHeaders(values map[string]string) (map[string]string, map[string]string, string) {
	literal := map[string]string{}
	env := map[string]string{}
	var bearerTokenEnvVar string
	for key, value := range values {
		if strings.EqualFold(key, "Authorization") {
			if envName, ok := openCodeBearerEnvName(value); ok {
				bearerTokenEnvVar = envName
				continue
			}
		}
		if envName, ok := openCodeEnvRefName(value); ok {
			env[key] = envName
			continue
		}
		literal[key] = value
	}
	return literal, env, bearerTokenEnvVar
}

func normalizeOpenCodeEnvMap(values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		if envName, ok := openCodeEnvRefName(value); ok {
			out[key] = fmt.Sprintf("${%s}", envName)
			continue
		}
		out[key] = value
	}
	return out
}

func openCodeEnvNameFromKanonRef(value string) (string, bool) {
	matches := envRefPattern.FindStringSubmatch(value)
	if len(matches) == 0 || matches[0] != value {
		return "", false
	}
	return matches[1], true
}

func openCodeBearerEnvName(value string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	return openCodeEnvRefName(strings.TrimPrefix(value, prefix))
}

func openCodeEnvRefName(value string) (string, bool) {
	matches := openCodeEnvRefPattern.FindStringSubmatch(value)
	if len(matches) == 0 || matches[0] != value {
		return "", false
	}
	return matches[1], true
}

func millisecondsToSeconds(value int) int {
	if value <= 0 {
		return 0
	}
	return (value + 999) / 1000
}

func openCodeJSONUnmarshal(data []byte, out any) error {
	return json.Unmarshal(stripJSONTrailingCommas(stripJSONComments(data)), out)
}

func stripJSONComments(data []byte) []byte {
	var out bytes.Buffer
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(data) {
			next := data[i+1]
			if next == '/' {
				i += 2
				for i < len(data) && data[i] != '\n' && data[i] != '\r' {
					i++
				}
				if i < len(data) {
					out.WriteByte(data[i])
				}
				continue
			}
			if next == '*' {
				i += 2
				for i < len(data)-1 {
					if data[i] == '\n' || data[i] == '\r' {
						out.WriteByte(data[i])
					}
					if data[i] == '*' && data[i+1] == '/' {
						i++
						break
					}
					i++
				}
				continue
			}
		}
		out.WriteByte(ch)
	}
	return out.Bytes()
}

func stripJSONTrailingCommas(data []byte) []byte {
	var out bytes.Buffer
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\n' || data[j] == '\r') {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}
		out.WriteByte(ch)
	}
	return out.Bytes()
}

func openCodeSkillName(value string) string {
	lower := strings.ToLower(value)
	var out strings.Builder
	lastHyphen := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen && out.Len() > 0 {
			out.WriteByte('-')
			lastHyphen = true
		}
	}
	name := strings.Trim(out.String(), "-")
	if name == "" {
		return "skill"
	}
	if len(name) > 64 {
		name = strings.TrimRight(name[:64], "-")
	}
	if name == "" {
		return "skill"
	}
	return name
}
