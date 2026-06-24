package core

import "io/fs"

const (
	AgentCodex  = "codex"
	AgentClaude = "claude"
	AgentGemini = "gemini"
	AgentAll    = "all"
)

type Config struct {
	Version      int               `yaml:"version"`
	Agents       []string          `yaml:"agents,omitempty"`
	Instructions Instructions      `yaml:"instructions"`
	Skills       []Skill           `yaml:"skills"`
	MCP          MCPConfig         `yaml:"mcp"`
	Hooks        []Hook            `yaml:"hooks"`
	Metadata     map[string]string `yaml:"metadata"`
}

type Instructions struct {
	Files []string `yaml:"files"`
}

type Skill struct {
	Name    string    `yaml:"name,omitempty"`
	Path    string    `yaml:"path,omitempty"`
	Git     *GitSkill `yaml:"git,omitempty"`
	Include []string  `yaml:"include,omitempty"`
	Exclude []string  `yaml:"exclude,omitempty"`
	Targets []string  `yaml:"targets,omitempty"`
	Enabled *bool     `yaml:"enabled,omitempty"`
}

type GitSkill struct {
	URL    string `yaml:"url"`
	Ref    string `yaml:"ref"`
	Subdir string `yaml:"subdir,omitempty"`
}

type RemoteSource struct {
	Type   string `yaml:"type"`
	URL    string `yaml:"url"`
	Ref    string `yaml:"ref"`
	Subdir string `yaml:"subdir"`
}

type SourceLock struct {
	Version int               `yaml:"version"`
	Sources []SourceLockEntry `yaml:"sources"`
}

type SourceLockEntry struct {
	Owner         string `yaml:"owner"`
	Type          string `yaml:"type"`
	URL           string `yaml:"url"`
	Ref           string `yaml:"ref"`
	Subdir        string `yaml:"subdir"`
	ResolvedRef   string `yaml:"resolved_ref"`
	ContentSHA256 string `yaml:"content_sha256"`
}

type MCPConfig struct {
	Servers map[string]MCPServer `yaml:"servers"`
}

type MCPServer struct {
	Type              string                   `yaml:"type"`
	Command           string                   `yaml:"command"`
	Args              []string                 `yaml:"args"`
	Env               map[string]string        `yaml:"env"`
	EnvVars           []string                 `yaml:"env_vars"`
	URL               string                   `yaml:"url"`
	Headers           map[string]string        `yaml:"headers"`
	EnvHeaders        map[string]string        `yaml:"env_headers"`
	BearerTokenEnvVar string                   `yaml:"bearer_token_env_var"`
	StartupTimeoutSec int                      `yaml:"startup_timeout_sec"`
	ToolTimeoutSec    int                      `yaml:"tool_timeout_sec"`
	EnabledTools      []string                 `yaml:"enabled_tools"`
	DisabledTools     []string                 `yaml:"disabled_tools"`
	DefaultApproval   string                   `yaml:"default_approval"`
	Tools             map[string]MCPToolPolicy `yaml:"tools"`
	Targets           []string                 `yaml:"targets,omitempty"`
	Enabled           *bool                    `yaml:"enabled"`
}

type MCPToolPolicy struct {
	Description    string `yaml:"description"`
	Approval       string `yaml:"approval"`
	ApprovalPrompt string `yaml:"approval_prompt"`
}

type Hook struct {
	Name    string   `yaml:"name"`
	Targets []string `yaml:"targets,omitempty"`
	Event   string   `yaml:"event"`
	Matcher string   `yaml:"matcher"`
	Type    string   `yaml:"type"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Timeout int      `yaml:"timeout"`
	Async   bool     `yaml:"async"`
}

type TargetOptions struct {
	KanonHome  string
	UserHome   string
	Project    string
	Agent      string
	SourceLock *SourceLock
}

type ImportOptions struct {
	TargetOptions     `yaml:",inline"`
	SecretPolicy      SecretPolicy
	InstructionPolicy InstructionPolicy
}

type SecretPolicy string

const (
	SecretPolicyKeep SecretPolicy = "keep"
)

type InstructionPolicy string

const (
	InstructionPolicyAuto   InstructionPolicy = "auto"
	InstructionPolicyCodex  InstructionPolicy = "codex"
	InstructionPolicyClaude InstructionPolicy = "claude"
	InstructionPolicyMerge  InstructionPolicy = "merge"
	InstructionPolicySkip   InstructionPolicy = "skip"
)

type RenderedFile struct {
	Agent   string
	Path    string
	Content []byte
	Mode    fs.FileMode
	Merge   FileMergeStrategy
}

type FileMergeStrategy string

const (
	FileMergeReplace        FileMergeStrategy = ""
	FileMergeCodexConfig    FileMergeStrategy = "codex_config"
	FileMergeClaudeSettings FileMergeStrategy = "claude_settings"
	FileMergeClaudeMCP      FileMergeStrategy = "claude_mcp"
)

type Adapter interface {
	Name() string
	Render(cfg *Config, opts TargetOptions) ([]RenderedFile, error)
	Import(opts ImportOptions) (*ImportResult, error)
}

type ImportResult struct {
	Config       *Config
	Files        map[string][]byte
	Warnings     []string
	UnmappedPath []string
}
