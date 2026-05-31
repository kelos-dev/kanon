package core

import (
	"fmt"
	"slices"
)

func normalizeCodexConfig(cfg *Config, raw map[string]any, warnings *[]string) {
	if raw == nil {
		return
	}
	if servers, ok := raw["mcp_servers"].(map[string]any); ok {
		ensureMCPServers(cfg)
		for name, value := range servers {
			server, ok := normalizeMCPServer(name, value, AgentCodex, warnings)
			if ok {
				cfg.MCP.Servers[name] = mergeMCPServer(cfg.MCP.Servers[name], server)
				delete(servers, name)
			}
		}
		if len(servers) == 0 {
			delete(raw, "mcp_servers")
		}
	}
	if profiles, ok := raw["profiles"].(map[string]any); ok {
		warnSkippedMap(warnings, "codex profiles", profiles)
		delete(raw, "profiles")
	}
}

func normalizeCodexHooks(cfg *Config, raw map[string]any, warnings *[]string) {
	hooksValue, ok := raw["hooks"]
	if !ok {
		return
	}
	hooksMap, ok := hooksValue.(map[string]any)
	if !ok {
		appendNormalizeWarning(warnings, "left Codex hooks raw because hooks is not an object")
		return
	}
	cfg.Hooks = append(cfg.Hooks, normalizeHookMap(hooksMap, AgentCodex, warnings)...)
	delete(raw, "hooks")
}

func normalizeClaudeSettings(cfg *Config, raw map[string]any, warnings *[]string) {
	if raw == nil {
		return
	}
	if hooksMap, ok := raw["hooks"].(map[string]any); ok {
		cfg.Hooks = append(cfg.Hooks, normalizeHookMap(hooksMap, AgentClaude, warnings)...)
		delete(raw, "hooks")
	}
}

func normalizeClaudeJSON(cfg *Config, raw map[string]any, warnings *[]string) {
	if raw == nil {
		return
	}
	servers, ok := raw["mcpServers"].(map[string]any)
	if !ok {
		return
	}
	ensureMCPServers(cfg)
	for name, value := range servers {
		server, ok := normalizeMCPServer(name, value, AgentClaude, warnings)
		if ok {
			cfg.MCP.Servers[name] = mergeMCPServer(cfg.MCP.Servers[name], server)
			delete(servers, name)
		}
	}
	if len(servers) == 0 {
		delete(raw, "mcpServers")
	}
}

func normalizeMCPServer(name string, value any, agent string, warnings *[]string) (MCPServer, bool) {
	raw, ok := value.(map[string]any)
	if !ok {
		appendNormalizeWarning(warnings, "left MCP server %q raw for %s because it is not an object", name, agent)
		return MCPServer{}, false
	}
	server := MCPServer{Targets: []string{agent}}
	remainder := copyMap(raw)
	if value, ok := stringValue(raw["type"]); ok {
		server.Type = value
		delete(remainder, "type")
	}
	if value, ok := stringValue(raw["command"]); ok {
		server.Command = value
		delete(remainder, "command")
	}
	if values, ok := stringList(raw["args"]); ok {
		server.Args = values
		delete(remainder, "args")
	}
	if values, ok := stringMap(raw["env"]); ok {
		server.Env = values
		delete(remainder, "env")
	}
	if values, ok := stringList(raw["env_vars"]); ok {
		server.EnvVars = values
		delete(remainder, "env_vars")
	}
	if value, ok := stringValue(raw["url"]); ok {
		server.URL = value
		delete(remainder, "url")
	}
	if values, ok := stringMap(raw["headers"]); ok {
		server.Headers, server.EnvHeaders = splitLiteralAndEnvHeaders(values)
		delete(remainder, "headers")
	}
	if values, ok := stringMap(raw["http_headers"]); ok {
		server.Headers, server.EnvHeaders = splitLiteralAndEnvHeaders(values)
		delete(remainder, "http_headers")
	}
	if values, ok := stringMap(raw["env_headers"]); ok {
		server.EnvHeaders = values
		delete(remainder, "env_headers")
	}
	if values, ok := stringMap(raw["env_http_headers"]); ok {
		server.EnvHeaders = values
		delete(remainder, "env_http_headers")
	}
	if value, ok := stringValue(raw["bearer_token_env_var"]); ok {
		server.BearerTokenEnvVar = value
		delete(remainder, "bearer_token_env_var")
	}
	if value, ok := intValue(raw["startup_timeout_sec"]); ok {
		server.StartupTimeoutSec = value
		delete(remainder, "startup_timeout_sec")
	}
	if value, ok := intValue(raw["tool_timeout_sec"]); ok {
		server.ToolTimeoutSec = value
		delete(remainder, "tool_timeout_sec")
	}
	if value, ok := intValue(raw["timeout"]); ok {
		server.StartupTimeoutSec = value
		delete(remainder, "timeout")
	}
	if values, ok := stringList(raw["enabled_tools"]); ok {
		server.EnabledTools = values
		delete(remainder, "enabled_tools")
	}
	if values, ok := stringList(raw["disabled_tools"]); ok {
		server.DisabledTools = values
		delete(remainder, "disabled_tools")
	}
	if value, ok := stringValue(raw["default_tool_approval"]); ok {
		server.DefaultApproval = value
		delete(remainder, "default_tool_approval")
	}
	if server.Command == "" && server.URL == "" {
		appendNormalizeWarning(warnings, "left MCP server %q raw for %s because it has no command or url", name, agent)
		return MCPServer{}, false
	}
	warnSkippedMap(warnings, fmt.Sprintf("%s mcp server %q", agent, name), remainder)
	return server, true
}

func normalizeHookMap(hooksMap map[string]any, agent string, warnings *[]string) []Hook {
	var hooks []Hook
	for event, value := range hooksMap {
		items, ok := value.([]any)
		if !ok {
			appendNormalizeWarning(warnings, "left %s hook event %q raw because it is not a list", agent, event)
			continue
		}
		for _, item := range items {
			hookMap, ok := item.(map[string]any)
			if !ok {
				appendNormalizeWarning(warnings, "left %s hook event %q item raw because it is not an object", agent, event)
				continue
			}
			handlers, ok := hookMap["hooks"].([]any)
			if !ok {
				appendNormalizeWarning(warnings, "left %s hook event %q item raw because hooks is not a list", agent, event)
				continue
			}
			matcher, _ := stringValue(hookMap["matcher"])
			for i, handler := range handlers {
				handlerMap, ok := handler.(map[string]any)
				if !ok {
					appendNormalizeWarning(warnings, "left %s hook event %q handler raw because it is not an object", agent, event)
					continue
				}
				hook := Hook{
					Name:    fmt.Sprintf("%s-%s-%d", agent, event, i+1),
					Targets: []string{agent},
					Event:   event,
					Matcher: matcher,
				}
				if value, ok := stringValue(handlerMap["type"]); ok {
					hook.Type = value
					delete(handlerMap, "type")
				}
				if value, ok := stringValue(handlerMap["command"]); ok {
					hook.Command = value
					delete(handlerMap, "command")
				}
				if values, ok := stringList(handlerMap["args"]); ok {
					hook.Args = values
					delete(handlerMap, "args")
				}
				if value, ok := intValue(handlerMap["timeout"]); ok {
					hook.Timeout = value
					delete(handlerMap, "timeout")
				}
				if value, ok := boolValue(handlerMap["async"]); ok {
					hook.Async = value
					delete(handlerMap, "async")
				}
				warnSkippedMap(warnings, fmt.Sprintf("%s hook %q", agent, event), handlerMap)
				hooks = append(hooks, hook)
			}
		}
	}
	return hooks
}

func mergeMCPServer(existing, incoming MCPServer) MCPServer {
	out := existing
	if out.Type == "" {
		out.Type = incoming.Type
	}
	if out.Command == "" {
		out.Command = incoming.Command
	}
	if len(out.Args) == 0 {
		out.Args = incoming.Args
	}
	if out.Env == nil {
		out.Env = incoming.Env
	} else {
		for key, value := range incoming.Env {
			out.Env[key] = value
		}
	}
	if len(out.EnvVars) == 0 {
		out.EnvVars = incoming.EnvVars
	}
	if out.URL == "" {
		out.URL = incoming.URL
	}
	if out.Headers == nil {
		out.Headers = incoming.Headers
	} else {
		for key, value := range incoming.Headers {
			out.Headers[key] = value
		}
	}
	if out.EnvHeaders == nil {
		out.EnvHeaders = incoming.EnvHeaders
	} else {
		for key, value := range incoming.EnvHeaders {
			out.EnvHeaders[key] = value
		}
	}
	if out.BearerTokenEnvVar == "" {
		out.BearerTokenEnvVar = incoming.BearerTokenEnvVar
	}
	if out.StartupTimeoutSec == 0 {
		out.StartupTimeoutSec = incoming.StartupTimeoutSec
	}
	if out.ToolTimeoutSec == 0 {
		out.ToolTimeoutSec = incoming.ToolTimeoutSec
	}
	if len(out.EnabledTools) == 0 {
		out.EnabledTools = incoming.EnabledTools
	}
	if len(out.DisabledTools) == 0 {
		out.DisabledTools = incoming.DisabledTools
	}
	if out.DefaultApproval == "" {
		out.DefaultApproval = incoming.DefaultApproval
	}
	out.Targets = mergeTargets(out.Targets, incoming.Targets)
	return out
}

func mergeTargets(existing, incoming []string) []string {
	out := slices.Clone(existing)
	for _, agent := range incoming {
		if !slices.Contains(out, agent) {
			out = append(out, agent)
		}
	}
	return out
}

func warnSkippedMap(warnings *[]string, label string, values map[string]any) {
	for key := range values {
		appendNormalizeWarning(warnings, "skipped unsupported %s field %q", label, key)
	}
}

func ensureMCPServers(cfg *Config) {
	if cfg.MCP.Servers == nil {
		cfg.MCP.Servers = map[string]MCPServer{}
	}
}

func copyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func stringValue(value any) (string, bool) {
	typed, ok := value.(string)
	return typed, ok
}

func stringList(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed, true
		}
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		value, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, value)
	}
	return out, true
}

func stringMap(value any) (map[string]string, bool) {
	raw, ok := value.(map[string]any)
	if !ok {
		if typed, ok := value.(map[string]string); ok {
			return typed, true
		}
		return nil, false
	}
	out := map[string]string{}
	for key, item := range raw {
		value, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[key] = value
	}
	return out, true
}

func splitLiteralAndEnvHeaders(values map[string]string) (map[string]string, map[string]string) {
	literal := map[string]string{}
	env := map[string]string{}
	for key, value := range values {
		if envName, ok := envRefName(value); ok {
			env[key] = envName
			continue
		}
		literal[key] = value
	}
	return literal, env
}

func envRefName(value string) (string, bool) {
	matches := envRefPattern.FindStringSubmatch(value)
	if len(matches) == 0 || matches[0] != value || matches[2] != "" {
		return "", false
	}
	return matches[1], true
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	default:
		return 0, false
	}
}

func boolValue(value any) (bool, bool) {
	typed, ok := value.(bool)
	return typed, ok
}

func appendNormalizeWarning(warnings *[]string, format string, args ...any) {
	if warnings == nil {
		return
	}
	*warnings = append(*warnings, fmt.Sprintf(format, args...))
}
