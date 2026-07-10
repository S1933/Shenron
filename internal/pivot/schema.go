package pivot

type PivotFile struct {
	Version  string              `yaml:"version"`
	Agents   []AgentDefinition   `yaml:"agents"`
	Commands []CommandDefinition `yaml:"commands"`
	Skills   []SkillReference    `yaml:"skills,omitempty"`
}

type AgentDefinition struct {
	ID           string         `yaml:"id"`
	Description  string         `yaml:"description"`
	Mode         string         `yaml:"mode"` // "primary" | "subagent"
	Model        string         `yaml:"model,omitempty"`
	Temperature  *float64       `yaml:"temperature,omitempty"`
	SystemPrompt string         `yaml:"systemPrompt,omitempty"`
	PromptFile   string         `yaml:"promptFile,omitempty"`
	Permissions  *Permissions   `yaml:"permissions,omitempty"`
	Extensions   map[string]any `yaml:"extensions,omitempty"`
}

type Permissions struct {
	Read      string            `yaml:"read,omitempty"`
	Edit      string            `yaml:"edit,omitempty"`
	Bash      any               `yaml:"bash,omitempty"`
	WebFetch  string            `yaml:"webfetch,omitempty"`
	WebSearch string            `yaml:"websearch,omitempty"`
	Tasks     map[string]string `yaml:"tasks,omitempty"`
}

type CommandDefinition struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Template    string `yaml:"template"`
	Agent       string `yaml:"agent,omitempty"`
	Model       string `yaml:"model,omitempty"`
}

type SkillReference struct {
	Name string `yaml:"name"`
}
