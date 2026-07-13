package claude

import (
	"fmt"
	"path/filepath"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
)

const fileMode = 0o644

// Adapter implements the Claude Code target adapter.
type Adapter struct {
	baseDir  string
	pivotDir string
}

// NewAdapter creates a Claude Code adapter.
func NewAdapter() *Adapter {
	return &Adapter{baseDir: fsutil.ClaudePath()}
}

// NewAdapterWithBaseDir creates an adapter with a custom base directory (for tests).
func NewAdapterWithBaseDir(baseDir, pivotDir string) *Adapter {
	return &Adapter{baseDir: baseDir, pivotDir: pivotDir}
}

// SetPivotDir sets the pivot directory for promptFile resolution.
func (a *Adapter) SetPivotDir(dir string) {
	a.pivotDir = dir
}

// Name returns the adapter identifier.
func (a *Adapter) Name() string {
	return "claude-code"
}

// ValidateAgent checks that an agent definition is valid for Claude Code.
func (a *Adapter) ValidateAgent(agent pivot.AgentDefinition) error {
	if agent.Mode != "primary" && agent.Mode != "subagent" {
		return fmt.Errorf("agent %q: mode must be primary or subagent", agent.ID)
	}
	return nil
}

// Generate produces one Markdown file per agent and command.
func (a *Adapter) Generate(pf *pivot.PivotFile) (adapter.GenerationResult, error) {
	var files []adapter.GeneratedFile

	for _, ag := range pf.Agents {
		if err := a.ValidateAgent(ag); err != nil {
			return adapter.GenerationResult{}, err
		}
		generated, err := generateAgentFile(ag, a.pivotDir, a.baseDir)
		if err != nil {
			return adapter.GenerationResult{}, fmt.Errorf("generate agent %q: %w", ag.ID, err)
		}
		files = append(files, a.filesFrom(generated, ag.ID)...)
	}

	for _, cmd := range pf.Commands {
		generated, err := generateCommandFile(cmd, a.baseDir)
		if err != nil {
			return adapter.GenerationResult{}, fmt.Errorf("generate command %q: %w", cmd.ID, err)
		}
		files = append(files, a.filesFrom(generated, cmd.ID)...)
	}

	return adapter.GenerationResult{Files: files}, nil
}

// filesFrom converts a path->content map into GeneratedFile records.
func (a *Adapter) filesFrom(generated map[string]string, resourceID string) []adapter.GeneratedFile {
	files := make([]adapter.GeneratedFile, 0, len(generated))
	for path, content := range generated {
		files = append(files, adapter.GeneratedFile{
			Path:       path,
			Content:    []byte(content),
			Mode:       fileMode,
			Adapter:    a.Name(),
			ResourceID: resourceID,
		})
	}
	return files
}

// TargetPaths returns paths this adapter writes to.
func (a *Adapter) TargetPaths() []string {
	return []string{
		filepath.Join(a.baseDir, "agents"),
		filepath.Join(a.baseDir, "commands"),
	}
}
