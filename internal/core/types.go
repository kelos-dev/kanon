package core

import "io/fs"

const (
	AgentCodex  = "codex"
	AgentClaude = "claude"
	AgentAll    = "all"
)

type Config struct {
	Version      int               `yaml:"version"`
	Instructions Instructions      `yaml:"instructions"`
	Skills       []Skill           `yaml:"skills"`
	MCP          MCPConfig         `yaml:"mcp"`
	Hooks        []Hook            `yaml:"hooks"`
	Permissions  Permissions       `yaml:"permissions"`
	Metadata     map[string]string `yaml:"metadata"`
}

type Instructions struct {
	Files []string `yaml:"files"`
}

type Skill struct {
	Name    string   `yaml:"name"`
	Path    string   `yaml:"path"`
	Targets []string `yaml:"targets"`
	Enabled *bool    `yaml:"enabled"`
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
	Targets           []string                 `yaml:"targets"`
	Enabled           *bool                    `yaml:"enabled"`
}

type MCPToolPolicy struct {
	Description    string `yaml:"description"`
	Approval       string `yaml:"approval"`
	ApprovalPrompt string `yaml:"approval_prompt"`
}

type Hook struct {
	Name    string   `yaml:"name"`
	Targets []string `yaml:"targets"`
	Event   string   `yaml:"event"`
	Matcher string   `yaml:"matcher"`
	Type    string   `yaml:"type"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Timeout int      `yaml:"timeout"`
	Async   bool     `yaml:"async"`
}

type Permissions struct {
	Allow                 []string         `yaml:"allow"`
	Ask                   []string         `yaml:"ask"`
	Deny                  []string         `yaml:"deny"`
	ApprovalPolicy        string           `yaml:"approval_policy"`
	SandboxMode           string           `yaml:"sandbox_mode"`
	DefaultPermissions    string           `yaml:"default_permissions"`
	DefaultMode           string           `yaml:"default_mode"`
	AdditionalDirectories []string         `yaml:"additional_directories"`
	Rules                 []PermissionRule `yaml:"rules"`
}

type PermissionRule struct {
	Pattern       []string `yaml:"pattern"`
	Decision      string   `yaml:"decision"`
	Justification string   `yaml:"justification"`
}

type TargetOptions struct {
	KanonHome string
	UserHome  string
	Project   string
	Agent     string
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
}

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
