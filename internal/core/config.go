package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	envRefPattern            = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)
	skillProviderNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

func DefaultHome() (string, error) {
	if value := os.Getenv("KANON_HOME"); value != "" {
		return expandHome(value)
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".config", "kanon"), nil
}

func LoadConfig(home, configPath string) (*Config, string, error) {
	var err error
	if home == "" {
		home, err = DefaultHome()
		if err != nil {
			return nil, "", err
		}
	}
	if configPath == "" {
		configPath = filepath.Join(home, "kanon.yaml")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, "", err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, "", err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return &cfg, configPath, nil
}

func WriteConfig(path string, cfg *Config) error {
	data, err := yamlMarshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func ValidateConfig(cfg *Config, home string) []error {
	var errs []error
	if cfg.Version != 1 {
		errs = append(errs, fmt.Errorf("unsupported version %d", cfg.Version))
	}
	for _, rel := range cfg.Instructions.Files {
		if strings.TrimSpace(rel) == "" {
			errs = append(errs, errors.New("instruction path cannot be empty"))
			continue
		}
		path := ResolvePath(home, rel)
		if _, err := os.Stat(path); err != nil {
			errs = append(errs, fmt.Errorf("instruction %q: %w", rel, err))
		}
	}
	gitSkillProviderNames := map[string]bool{}
	for _, skill := range cfg.Skills {
		if !enabled(skill.Enabled) {
			continue
		}
		name, nameErr := skillEntryName(skill)
		if nameErr != nil {
			errs = append(errs, nameErr)
			continue
		}
		if skill.Git != nil {
			label := gitSkillProviderLabel(skill)
			if skill.Path != "" {
				errs = append(errs, errors.New("git skill provider cannot be used with path"))
			}
			validateTargets(label, skill.Targets, &errs)
			if _, err := gitSkillProviderName(skill); err != nil {
				errs = append(errs, fmt.Errorf("%s %w", label, err))
			}
			validateGitSkill(label, skill.Git, &errs)
			validateSkillSelection(label, skill.Include, skill.Exclude, &errs)
			if skill.Git != nil {
				if providerName, err := gitSkillProviderName(skill); err == nil {
					if gitSkillProviderNames[providerName] {
						errs = append(errs, fmt.Errorf("git skill provider %q is duplicated", providerName))
					}
					gitSkillProviderNames[providerName] = true
				}
			}
			continue
		}
		if name == "" {
			errs = append(errs, errors.New("skill name cannot be empty"))
			continue
		}
		if len(skill.Include) > 0 {
			errs = append(errs, fmt.Errorf("skill %q cannot use include without git", name))
		}
		if len(skill.Exclude) > 0 {
			errs = append(errs, fmt.Errorf("skill %q cannot use exclude without git", name))
		}
		validateTargets(fmt.Sprintf("skill %q", name), skill.Targets, &errs)
		path := skill.Path
		if path == "" {
			path = filepath.Join("skills", name)
		}
		skillFile := filepath.Join(ResolvePath(home, path), "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			errs = append(errs, fmt.Errorf("skill %q: %w", name, err))
		}
	}
	for name, server := range cfg.MCP.Servers {
		if !enabled(server.Enabled) {
			continue
		}
		if strings.TrimSpace(name) == "" {
			errs = append(errs, errors.New("mcp server name cannot be empty"))
		}
		validateTargets(fmt.Sprintf("mcp server %q", name), server.Targets, &errs)
		if server.URL == "" && server.Command == "" {
			errs = append(errs, fmt.Errorf("mcp server %q requires url or command", name))
		}
	}
	for _, hook := range cfg.Hooks {
		if strings.TrimSpace(hook.Name) == "" {
			errs = append(errs, errors.New("hook name cannot be empty"))
		}
		validateTargets(fmt.Sprintf("hook %q", hook.Name), hook.Targets, &errs)
	}
	validateEnvRefs("config", cfg, &errs)
	return errs
}

func gitSkillProviderLabel(skill Skill) string {
	if skill.Git != nil {
		if name, err := gitSkillProviderName(skill); err == nil {
			return fmt.Sprintf("git skill provider %q", name)
		}
		if name := strings.TrimSpace(skill.Name); name != "" {
			return fmt.Sprintf("git skill provider %q", name)
		}
	}
	return "git skill provider"
}

func validateGitSkill(label string, git *GitSkill, errs *[]error) {
	if git == nil {
		*errs = append(*errs, fmt.Errorf("%s is required", label))
		return
	}
	if strings.TrimSpace(git.URL) == "" {
		*errs = append(*errs, fmt.Errorf("%s requires url", label))
	}
	if strings.TrimSpace(git.Ref) == "" {
		*errs = append(*errs, fmt.Errorf("%s requires ref", label))
	} else if strings.HasPrefix(git.Ref, "-") || strings.ContainsAny(git.Ref, "\x00\r\n") {
		*errs = append(*errs, fmt.Errorf("%s has invalid ref %q", label, git.Ref))
	}
	if _, err := cleanRemoteSubdir(git.Subdir); err != nil {
		*errs = append(*errs, fmt.Errorf("%s has invalid subdir %q: %w", label, git.Subdir, err))
	}
}

func skillEntryName(skill Skill) (string, error) {
	return strings.TrimSpace(skill.Name), nil
}

func gitSkillProviderName(skill Skill) (string, error) {
	if skill.Git == nil {
		return "", errors.New("requires git")
	}
	name, err := skillEntryName(skill)
	if err != nil {
		return "", err
	}
	if name == "" {
		name = deriveGitSkillProviderName(skill.Git.URL)
		if name == "" {
			return "", errors.New("requires name or a git url with a repository name")
		}
	}
	if !skillProviderNamePattern.MatchString(name) {
		return "", fmt.Errorf("has invalid name %q", name)
	}
	return name, nil
}

func deriveGitSkillProviderName(rawURL string) string {
	value := strings.TrimSpace(expandEnvRefs(rawURL))
	if value == "" {
		return ""
	}
	if i := strings.IndexAny(value, "?#"); i >= 0 {
		value = value[:i]
	}
	value = strings.TrimRight(value, "/")
	if value == "" {
		return ""
	}
	if i := strings.LastIndex(value, "/"); i >= 0 {
		value = value[i+1:]
	} else if i := strings.LastIndex(value, ":"); i >= 0 {
		value = value[i+1:]
	}
	value = strings.TrimSuffix(value, ".git")
	return strings.TrimSpace(value)
}

func validateSkillSelection(label string, include, exclude []string, errs *[]error) {
	includeSet := map[string]bool{}
	for _, name := range include {
		if strings.TrimSpace(name) == "" {
			*errs = append(*errs, fmt.Errorf("%s include cannot contain an empty skill name", label))
			continue
		}
		if includeSet[name] {
			*errs = append(*errs, fmt.Errorf("%s include has duplicate skill %q", label, name))
		}
		includeSet[name] = true
	}
	excludeSet := map[string]bool{}
	for _, name := range exclude {
		if strings.TrimSpace(name) == "" {
			*errs = append(*errs, fmt.Errorf("%s exclude cannot contain an empty skill name", label))
			continue
		}
		if excludeSet[name] {
			*errs = append(*errs, fmt.Errorf("%s exclude has duplicate skill %q", label, name))
		}
		if includeSet[name] {
			*errs = append(*errs, fmt.Errorf("%s cannot both include and exclude skill %q", label, name))
		}
		excludeSet[name] = true
	}
}

func ResolvePath(home, path string) string {
	if path == "" {
		return home
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		if expanded, err := expandHome(path); err == nil {
			return expanded
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(home, path)
}

func HasTarget(list []string, agent string) bool {
	if len(list) == 0 {
		return true
	}
	for _, item := range list {
		if item == AgentAll || item == agent {
			return true
		}
	}
	return false
}

func validateTargets(label string, targets []string, errs *[]error) {
	for _, target := range targets {
		if target != AgentAll && target != AgentCodex && target != AgentClaude {
			*errs = append(*errs, fmt.Errorf("%s has unsupported target %q", label, target))
		}
	}
}

func enabled(value *bool) bool {
	return value == nil || *value
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

func validateEnvRefs(label string, value any, errs *[]error) {
	switch typed := value.(type) {
	case string:
		if typed == legacyRedactedSecret {
			return
		}
		for _, match := range envRefPattern.FindAllStringSubmatch(typed, -1) {
			if match[2] == "" && os.Getenv(match[1]) == "" {
				*errs = append(*errs, fmt.Errorf("%s references unset environment variable %s", label, match[1]))
			}
		}
	case []string:
		for i, item := range typed {
			validateEnvRefs(fmt.Sprintf("%s[%d]", label, i), item, errs)
		}
	case map[string]string:
		for key, item := range typed {
			validateEnvRefs(fmt.Sprintf("%s.%s", label, key), item, errs)
		}
	case map[string]any:
		for key, item := range typed {
			validateEnvRefs(fmt.Sprintf("%s.%s", label, key), item, errs)
		}
	case []any:
		for i, item := range typed {
			validateEnvRefs(fmt.Sprintf("%s[%d]", label, i), item, errs)
		}
	case *Config:
		if typed != nil {
			validateEnvRefs(label, *typed, errs)
		}
	case Config:
		validateEnvRefs(label+".instructions", typed.Instructions, errs)
		validateEnvRefs(label+".skills", typed.Skills, errs)
		validateEnvRefs(label+".mcp", typed.MCP, errs)
		validateEnvRefs(label+".hooks", typed.Hooks, errs)
	case Instructions:
		validateEnvRefs(label+".files", typed.Files, errs)
	case []Skill:
		for i, item := range typed {
			validateEnvRefs(fmt.Sprintf("%s[%d]", label, i), item, errs)
		}
	case Skill:
		validateEnvRefs(label+".path", typed.Path, errs)
		validateEnvRefs(label+".name", typed.Name, errs)
		validateEnvRefs(label+".git", typed.Git, errs)
		validateEnvRefs(label+".include", typed.Include, errs)
		validateEnvRefs(label+".exclude", typed.Exclude, errs)
		validateEnvRefs(label+".targets", typed.Targets, errs)
	case *GitSkill:
		if typed != nil {
			validateEnvRefs(label, *typed, errs)
		}
	case GitSkill:
		validateEnvRefs(label+".url", typed.URL, errs)
		validateEnvRefs(label+".ref", typed.Ref, errs)
		validateEnvRefs(label+".subdir", typed.Subdir, errs)
	case *RemoteSource:
		if typed != nil {
			validateEnvRefs(label, *typed, errs)
		}
	case RemoteSource:
		validateEnvRefs(label+".type", typed.Type, errs)
		validateEnvRefs(label+".url", typed.URL, errs)
		validateEnvRefs(label+".ref", typed.Ref, errs)
		validateEnvRefs(label+".subdir", typed.Subdir, errs)
	case MCPConfig:
		for name, item := range typed.Servers {
			validateEnvRefs(label+".servers."+name, item, errs)
		}
	case MCPServer:
		validateEnvRefs(label+".command", typed.Command, errs)
		validateEnvRefs(label+".args", typed.Args, errs)
		validateEnvRefs(label+".env", typed.Env, errs)
		validateEnvRefs(label+".url", typed.URL, errs)
		validateEnvRefs(label+".headers", typed.Headers, errs)
		validateEnvRefs(label+".env_headers", typed.EnvHeaders, errs)
	case []Hook:
		for i, item := range typed {
			validateEnvRefs(fmt.Sprintf("%s[%d]", label, i), item, errs)
		}
	case Hook:
		validateEnvRefs(label+".command", typed.Command, errs)
		validateEnvRefs(label+".args", typed.Args, errs)
	}
}
