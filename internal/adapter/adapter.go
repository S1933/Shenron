package adapter

import "github.com/jnuel/agentsync/internal/pivot"

type Adapter interface {
	Name() string
	ValidateAgent(pivot.AgentDefinition) error
	GenerateAgent(pivot.AgentDefinition) (map[string]string, error)
	GenerateCommand(pivot.CommandDefinition) (map[string]string, error)
	TargetPaths() []string
	MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error)
}
