package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const redactedSecret = "KANON_REDACTED_SECRET_REVIEW_REQUIRED"
const legacyRedactedSecret = "${REVIEW_SECRET}"

func ImportAll(opts ImportOptions) (*ImportResult, error) {
	if opts.SecretPolicy == "" {
		opts.SecretPolicy = SecretPolicyKeep
	}
	if opts.InstructionPolicy == "" {
		opts.InstructionPolicy = InstructionPolicyAuto
	}
	if err := ValidateSecretPolicy(opts.SecretPolicy); err != nil {
		return nil, err
	}
	if err := ValidateInstructionPolicy(opts.InstructionPolicy); err != nil {
		return nil, err
	}
	merged := &ImportResult{
		Config: &Config{
			Version:  1,
			Metadata: map[string]string{"generated_by": "kanon import"},
		},
		Files: map[string][]byte{},
	}
	if err := importInstructions(merged, opts); err != nil {
		return nil, err
	}
	if err := importSkills(merged, opts); err != nil {
		return nil, err
	}
	for _, adapter := range adaptersFor(nil, opts.Agent) {
		result, err := adapter.Import(opts)
		if err != nil {
			return nil, err
		}
		mergeImport(merged, result)
	}
	return merged, nil
}

func ValidateSecretPolicy(policy SecretPolicy) error {
	switch policy {
	case SecretPolicyKeep:
		return nil
	default:
		return fmt.Errorf("unsupported secret policy %q; only %q is implemented", policy, SecretPolicyKeep)
	}
}

func ValidateInstructionPolicy(policy InstructionPolicy) error {
	switch policy {
	case InstructionPolicyAuto, InstructionPolicyCodex, InstructionPolicyClaude, InstructionPolicyMerge, InstructionPolicySkip:
		return nil
	default:
		return fmt.Errorf("unsupported instruction policy %q", policy)
	}
}

func WriteImport(home string, result *ImportResult, force bool) error {
	configPath := filepath.Join(home, "kanon.yaml")
	if _, err := os.Stat(configPath); err == nil && !force {
		return fmt.Errorf("%s already exists; re-run import with --force to replace it", configPath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for rel, data := range result.Files {
		path := ResolvePath(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	compactImplicitLocalSkills(result.Config)
	return WriteConfig(configPath, result.Config)
}

func ImportPreview(result *ImportResult) ([]byte, error) {
	if result == nil || result.Config == nil {
		return nil, errors.New("missing import result")
	}
	cfg := *result.Config
	cfg.Skills = append([]Skill(nil), result.Config.Skills...)
	compactImplicitLocalSkills(&cfg)
	return configToYAML(&cfg)
}

func configToYAML(cfg *Config) ([]byte, error) {
	return yamlMarshal(cfg)
}

func mergeImport(dst, src *ImportResult) {
	if src == nil || src.Config == nil {
		return
	}
	dst.Config.Instructions.Files = append(dst.Config.Instructions.Files, src.Config.Instructions.Files...)
	dst.Config.Skills = append(dst.Config.Skills, src.Config.Skills...)
	if len(src.Config.MCP.Servers) > 0 {
		ensureMCPServers(dst.Config)
		for name, server := range src.Config.MCP.Servers {
			dst.Config.MCP.Servers[name] = mergeMCPServer(dst.Config.MCP.Servers[name], server)
		}
	}
	dst.Config.Hooks = append(dst.Config.Hooks, src.Config.Hooks...)
	for rel, data := range src.Files {
		dst.Files[rel] = data
	}
	dst.Warnings = append(dst.Warnings, src.Warnings...)
	dst.UnmappedPath = append(dst.UnmappedPath, src.UnmappedPath...)
}

func compactImplicitLocalSkills(cfg *Config) {
	if cfg == nil {
		return
	}
	filtered := cfg.Skills[:0]
	for _, skill := range cfg.Skills {
		if skill.Git == nil &&
			skill.Path == "" &&
			len(skill.Targets) == 0 &&
			skill.Enabled == nil &&
			len(skill.Include) == 0 &&
			len(skill.Exclude) == 0 {
			continue
		}
		filtered = append(filtered, skill)
	}
	cfg.Skills = filtered
}

func readIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func tomlUnmarshal(data []byte, out any) error {
	return toml.Unmarshal(data, out)
}

type sanitizeContext struct {
	policy   SecretPolicy
	warnings *[]string
}

func sanitizeMap(input map[string]any, ctx sanitizeContext, path string) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		sanitized, keep := sanitizeValue(key, value, ctx, joinImportPath(path, key))
		if keep {
			out[key] = sanitized
		}
	}
	return out
}

func sanitizeValue(key string, value any, ctx sanitizeContext, path string) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeMap(typed, ctx, path), true
	case map[any]any:
		out := map[string]any{}
		for k, v := range typed {
			childKey := fmt.Sprint(k)
			sanitized, keep := sanitizeValue(childKey, v, ctx, joinImportPath(path, childKey))
			if keep {
				out[childKey] = sanitized
			}
		}
		return out, true
	case []any:
		out := make([]any, 0, len(typed))
		for i, item := range typed {
			sanitized, keep := sanitizeValue(key, item, ctx, fmt.Sprintf("%s[%d]", path, i))
			if keep {
				out = append(out, sanitized)
			}
		}
		return out, true
	case string:
		return sanitizeString(key, typed, ctx, path)
	default:
		return typed, true
	}
}

func sanitizeString(key, value string, ctx sanitizeContext, path string) (any, bool) {
	if !looksSecret(key, value) || isSecretReference(value) {
		return value, true
	}
	// TODO: add env-ref, omit, password-manager, and encrypted-secret policies.
	appendImportWarning(ctx, "kept possible plaintext secret at %s because secret policy is keep", path)
	return value, true
}

func looksSecret(key, value string) bool {
	lowerKey := strings.ToLower(key)
	if strings.Contains(lowerKey, "secret") ||
		strings.Contains(lowerKey, "password") ||
		strings.Contains(lowerKey, "passphrase") ||
		strings.Contains(lowerKey, "private_key") ||
		strings.Contains(lowerKey, "credential") ||
		lowerKey == "authorization" ||
		strings.HasSuffix(lowerKey, "_authorization") ||
		lowerKey == "cookie" ||
		strings.Contains(lowerKey, "api_key") ||
		strings.Contains(lowerKey, "api-key") ||
		strings.HasSuffix(lowerKey, "apikey") ||
		strings.HasSuffix(lowerKey, "token") ||
		lowerKey == "token" {
		return true
	}
	if strings.HasPrefix(value, "sk-") ||
		strings.HasPrefix(value, "ghp_") ||
		strings.HasPrefix(value, "github_pat_") ||
		strings.HasPrefix(strings.ToLower(value), "bearer ") ||
		strings.HasPrefix(strings.ToLower(value), "token ") {
		return true
	}
	return false
}

func isSecretReference(value string) bool {
	return envRefPattern.MatchString(value) ||
		value == redactedSecret ||
		value == legacyRedactedSecret ||
		strings.HasPrefix(value, "op://")
}

func appendImportWarning(ctx sanitizeContext, format string, args ...any) {
	if ctx.warnings == nil {
		return
	}
	*ctx.warnings = append(*ctx.warnings, fmt.Sprintf(format, args...))
}

func joinImportPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}
