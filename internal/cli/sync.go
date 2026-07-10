package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jnuel/agentsync/internal/adapter"
	"github.com/jnuel/agentsync/internal/adapter/opencode"
	"github.com/jnuel/agentsync/internal/pivot"
)

type pivotDirSetter interface {
	SetPivotDir(string)
}

type fragmentAccumulator interface {
	ResetFragments()
	Fragments() map[string]any
	ConfigPath() string
}

// Generate produces the file map for each adapter from a parsed pivot file.
func Generate(pf *pivot.PivotFile, pivotDir string, adapters map[string]adapter.Adapter) (map[string]map[string]string, error) {
	out := make(map[string]map[string]string, len(adapters))

	for name, adpt := range adapters {
		if setter, ok := adpt.(pivotDirSetter); ok {
			setter.SetPivotDir(pivotDir)
		}
		if acc, ok := adpt.(fragmentAccumulator); ok {
			acc.ResetFragments()
		}

		files := make(map[string]string)

		for _, agent := range pf.Agents {
			agentFiles, err := adpt.GenerateAgent(agent)
			if err != nil {
				return nil, fmt.Errorf("%s: generate agent %q: %w", name, agent.ID, err)
			}
			for path, content := range agentFiles {
				files[path] = content
			}
		}

		for _, cmd := range pf.Commands {
			cmdFiles, err := adpt.GenerateCommand(cmd)
			if err != nil {
				return nil, fmt.Errorf("%s: generate command %q: %w", name, cmd.ID, err)
			}
			for path, content := range cmdFiles {
				files[path] = content
			}
		}

		if acc, ok := adpt.(fragmentAccumulator); ok {
			configPath := acc.ConfigPath()
			var existing []byte
			if data, err := os.ReadFile(configPath); err == nil {
				existing = data
			}
			merged, err := adpt.MergeFile(configPath, existing, acc.Fragments())
			if err != nil {
				return nil, fmt.Errorf("%s: merge %s: %w", name, filepath.Base(configPath), err)
			}
			if merged != nil {
				files[configPath] = string(merged)
			}
		}

		out[name] = files
	}

	return out, nil
}

// Ensure opencode.Adapter satisfies optional interfaces at compile time.
var (
	_ pivotDirSetter      = (*opencode.Adapter)(nil)
	_ fragmentAccumulator = (*opencode.Adapter)(nil)
)
