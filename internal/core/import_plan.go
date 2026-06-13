package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ImportPlan struct {
	ApplyPlan *ApplyPlan
	Units     map[string]ImportUnit
}

type ImportUnit struct {
	Path   string
	Agent  string
	Config Config
	Files  map[string][]byte
	Skill  *Skill
}

type importUnitPreview struct {
	Kind         string               `yaml:"kind"`
	Instructions []string             `yaml:"instructions,omitempty"`
	Skill        *Skill               `yaml:"skill,omitempty"`
	MCPServers   map[string]MCPServer `yaml:"mcp_servers,omitempty"`
	Hook         *Hook                `yaml:"hook,omitempty"`
	Files        map[string]string    `yaml:"files,omitempty"`
}

func PlanImport(opts ImportOptions) (*ImportPlan, error) {
	result, err := ImportAll(opts)
	if err != nil {
		return nil, err
	}
	base, err := loadImportBaseConfig(opts.KanonHome)
	if err != nil {
		return nil, err
	}
	units, err := buildImportUnits(opts.KanonHome, result)
	if err != nil {
		return nil, err
	}
	plan := &ApplyPlan{}
	byPath := make(map[string]ImportUnit, len(units))
	for _, unit := range units {
		existing, err := importUnitPreviewData(opts.KanonHome, base, unit, false)
		if err != nil {
			return nil, err
		}
		desired, err := importUnitPreviewData(opts.KanonHome, base, unit, true)
		if err != nil {
			return nil, err
		}
		existingHash := HashBytes(existing)
		desiredHash := HashBytes(desired)
		if existingHash == desiredHash {
			continue
		}
		action := "update"
		if len(existing) == 0 {
			action = "create"
		}
		plan.Changes = append(plan.Changes, FileChange{
			Agent:        unit.Agent,
			Path:         unit.Path,
			Action:       action,
			ExistingHash: existingHash,
			DesiredHash:  desiredHash,
			File: RenderedFile{
				Agent:   unit.Agent,
				Path:    unit.Path,
				Content: desired,
				Mode:    0o644,
			},
			Existing: existing,
		})
		byPath[unit.Path] = unit
	}
	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].Path < plan.Changes[j].Path })
	return &ImportPlan{ApplyPlan: plan, Units: byPath}, nil
}

func WriteSelectedImport(plan *ImportPlan, selected map[string]bool, home string) error {
	if plan == nil || plan.ApplyPlan == nil {
		return errors.New("missing import plan")
	}
	if len(plan.ApplyPlan.Conflicts) > 0 {
		return fmt.Errorf("cannot import with %d conflict(s)", len(plan.ApplyPlan.Conflicts))
	}
	cfg, err := loadImportBaseConfig(home)
	if err != nil {
		return err
	}
	var files []importFile
	for path := range selected {
		unit, ok := plan.Units[path]
		if !ok {
			return fmt.Errorf("selected import unit no longer exists: %s", path)
		}
		mergeImportUnitConfig(cfg, unit.Config)
		for rel, data := range unit.Files {
			files = append(files, importFile{rel: rel, data: data})
		}
	}
	compactImplicitLocalSkills(cfg)
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	for _, file := range files {
		path := ResolvePath(home, file.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, file.data, 0o644); err != nil {
			return err
		}
	}
	return WriteConfig(filepath.Join(home, "kanon.yaml"), cfg)
}

type importFile struct {
	rel  string
	data []byte
}

func loadImportBaseConfig(home string) (*Config, error) {
	cfg, _, err := LoadConfig(home, "")
	if err == nil {
		return cfg, nil
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && errors.Is(pathErr.Err, os.ErrNotExist) {
		return &Config{Version: 1}, nil
	}
	return nil, err
}

func buildImportUnits(home string, result *ImportResult) ([]ImportUnit, error) {
	if result == nil || result.Config == nil {
		return nil, nil
	}
	var units []ImportUnit
	if len(result.Config.Instructions.Files) > 0 {
		files := selectFiles(result.Files, result.Config.Instructions.Files...)
		units = append(units, ImportUnit{
			Path:   unitPath(home, result.Config.Instructions.Files[0]),
			Agent:  AgentAll,
			Config: Config{Version: 1, Instructions: result.Config.Instructions},
			Files:  files,
		})
	}
	for _, skill := range result.Config.Skills {
		path := skill.Path
		if path == "" {
			path = filepath.Join("skills", skill.Name)
		}
		files := selectFilesUnder(result.Files, path)
		config := Config{Version: 1}
		config.Skills = []Skill{skill}
		previewSkill := skill
		units = append(units, ImportUnit{
			Path:   unitPath(home, filepath.Join(path, "SKILL.md")),
			Agent:  importTargetsAgent(skill.Targets),
			Config: config,
			Files:  files,
			Skill:  &previewSkill,
		})
	}
	if len(result.Config.MCP.Servers) > 0 {
		names := make([]string, 0, len(result.Config.MCP.Servers))
		for name := range result.Config.MCP.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			server := result.Config.MCP.Servers[name]
			units = append(units, ImportUnit{
				Path:  filepath.Join(home, "kanon.yaml") + "#mcp.servers." + name,
				Agent: importTargetsAgent(server.Targets),
				Config: Config{
					Version: 1,
					MCP:     MCPConfig{Servers: map[string]MCPServer{name: server}},
				},
				Files: map[string][]byte{},
			})
		}
	}
	for i, hook := range result.Config.Hooks {
		id := hook.Name
		if id == "" {
			id = fmt.Sprintf("%d", i+1)
		}
		units = append(units, ImportUnit{
			Path:   filepath.Join(home, "kanon.yaml") + "#hooks." + id,
			Agent:  importTargetsAgent(hook.Targets),
			Config: Config{Version: 1, Hooks: []Hook{hook}},
			Files:  map[string][]byte{},
		})
	}
	return units, nil
}

func importUnitPreviewData(home string, base *Config, unit ImportUnit, desired bool) ([]byte, error) {
	if desired {
		cfg := existingImportUnitConfig(base, unit.Config)
		mergeImportUnitConfig(&cfg, unit.Config)
		return marshalImportUnitPreview(cfg, unit.Files, importUnitPreviewSkill(cfg, unit.Skill))
	}
	existing := existingImportUnitConfig(base, unit.Config)
	files := map[string][]byte{}
	for rel := range unit.Files {
		data, err := readIfExists(ResolvePath(home, rel))
		if err != nil {
			return nil, err
		}
		if data != nil {
			files[rel] = data
		}
	}
	if isEmptyImportUnitConfig(existing) && len(files) == 0 {
		return nil, nil
	}
	return marshalImportUnitPreview(existing, files, existingImportUnitPreviewSkill(existing, files, unit.Skill))
}

func marshalImportUnitPreview(cfg Config, files map[string][]byte, previewSkill *Skill) ([]byte, error) {
	preview := importUnitPreview{Files: stringFiles(files)}
	if len(cfg.Instructions.Files) > 0 {
		preview.Kind = "instructions"
		preview.Instructions = cfg.Instructions.Files
	}
	if previewSkill != nil {
		preview.Kind = "skill"
		preview.Skill = previewSkill
	} else if len(cfg.Skills) > 0 {
		preview.Kind = "skill"
		skill := cfg.Skills[0]
		preview.Skill = &skill
	}
	if len(cfg.MCP.Servers) > 0 {
		preview.Kind = "mcp"
		preview.MCPServers = cfg.MCP.Servers
	}
	if len(cfg.Hooks) > 0 {
		preview.Kind = "hook"
		hook := cfg.Hooks[0]
		preview.Hook = &hook
	}
	return yamlMarshal(preview)
}

func existingImportUnitPreviewSkill(existing Config, files map[string][]byte, imported *Skill) *Skill {
	if imported == nil {
		return nil
	}
	if skill := importUnitPreviewSkill(existing, nil); skill != nil {
		return skill
	}
	if len(files) > 0 {
		skill := *imported
		return &skill
	}
	return nil
}

func importUnitPreviewSkill(cfg Config, fallback *Skill) *Skill {
	if len(cfg.Skills) > 0 {
		skill := cfg.Skills[0]
		return &skill
	}
	return fallback
}

func existingImportUnitConfig(base *Config, unit Config) Config {
	var cfg Config
	cfg.Version = 1
	for _, rel := range unit.Instructions.Files {
		if stringInSlice(base.Instructions.Files, rel) {
			cfg.Instructions.Files = append(cfg.Instructions.Files, rel)
		}
	}
	for _, skill := range unit.Skills {
		if existing, ok := findSkill(base.Skills, skill.Name); ok {
			cfg.Skills = append(cfg.Skills, existing)
		}
	}
	if len(unit.MCP.Servers) > 0 && len(base.MCP.Servers) > 0 {
		cfg.MCP.Servers = map[string]MCPServer{}
		for name := range unit.MCP.Servers {
			if existing, ok := base.MCP.Servers[name]; ok {
				cfg.MCP.Servers[name] = existing
			}
		}
	}
	for _, hook := range unit.Hooks {
		if existing, ok := findHook(base.Hooks, hook.Name); ok {
			cfg.Hooks = append(cfg.Hooks, existing)
		}
	}
	return cfg
}

func isEmptyImportUnitConfig(cfg Config) bool {
	return len(cfg.Instructions.Files) == 0 &&
		len(cfg.Skills) == 0 &&
		len(cfg.MCP.Servers) == 0 &&
		len(cfg.Hooks) == 0
}

func mergeImportUnitConfig(dst *Config, src Config) {
	if dst.Version == 0 {
		dst.Version = 1
	}
	for _, rel := range src.Instructions.Files {
		if !stringInSlice(dst.Instructions.Files, rel) {
			dst.Instructions.Files = append(dst.Instructions.Files, rel)
		}
	}
	for _, skill := range src.Skills {
		upsertSkill(&dst.Skills, skill)
	}
	if len(src.MCP.Servers) > 0 {
		ensureMCPServers(dst)
		for name, server := range src.MCP.Servers {
			dst.MCP.Servers[name] = server
		}
	}
	for _, hook := range src.Hooks {
		upsertHook(&dst.Hooks, hook)
	}
}

func upsertSkill(skills *[]Skill, next Skill) {
	for i, existing := range *skills {
		if existing.Name == next.Name {
			next.Targets = mergeImportTargets(existing.Targets, next.Targets)
			(*skills)[i] = next
			return
		}
	}
	*skills = append(*skills, next)
}

func upsertHook(hooks *[]Hook, next Hook) {
	for i, existing := range *hooks {
		if existing.Name == next.Name {
			(*hooks)[i] = next
			return
		}
	}
	*hooks = append(*hooks, next)
}

func findSkill(skills []Skill, name string) (Skill, bool) {
	for _, skill := range skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return Skill{}, false
}

func findHook(hooks []Hook, name string) (Hook, bool) {
	for _, hook := range hooks {
		if hook.Name == name {
			return hook, true
		}
	}
	return Hook{}, false
}

func selectFiles(files map[string][]byte, rels ...string) map[string][]byte {
	out := map[string][]byte{}
	for _, rel := range rels {
		if data, ok := files[rel]; ok {
			out[rel] = data
		}
	}
	return out
}

func selectFilesUnder(files map[string][]byte, dir string) map[string][]byte {
	out := map[string][]byte{}
	prefix := strings.TrimSuffix(filepath.Clean(dir), string(filepath.Separator)) + string(filepath.Separator)
	for rel, data := range files {
		clean := filepath.Clean(rel)
		if clean == filepath.Clean(dir) || strings.HasPrefix(clean, prefix) {
			out[rel] = data
		}
	}
	return out
}

func stringFiles(files map[string][]byte) map[string]string {
	if len(files) == 0 {
		return nil
	}
	out := make(map[string]string, len(files))
	for rel, data := range files {
		out[rel] = string(data)
	}
	return out
}

func importTargetsAgent(targets []string) string {
	if len(targets) == 1 && (targets[0] == AgentCodex || targets[0] == AgentClaude) {
		return targets[0]
	}
	return AgentAll
}

func unitPath(home, rel string) string {
	return ResolvePath(home, rel)
}

func stringInSlice(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func mergeImportTargets(existing, imported []string) []string {
	if len(existing) == 0 || len(imported) == 0 {
		return nil
	}
	return unionStrings(existing, imported)
}

func unionStrings(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, item := range list {
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}
