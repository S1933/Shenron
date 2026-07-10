package opencode

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
)

const configFileName = "opencode.json"

// Adapter implements the OpenCode target adapter.
type Adapter struct {
	baseDir   string
	pivotDir  string
	fragments map[string]any
}

// NewAdapter creates an OpenCode adapter writing to the default config directory.
func NewAdapter() *Adapter {
	return &Adapter{
		baseDir:   fsutil.OpenCodePath(),
		fragments: make(map[string]any),
	}
}

// NewAdapterWithBaseDir creates an adapter with a custom base directory (for tests).
func NewAdapterWithBaseDir(baseDir, pivotDir string) *Adapter {
	return &Adapter{
		baseDir:   baseDir,
		pivotDir:  pivotDir,
		fragments: make(map[string]any),
	}
}

// SetPivotDir sets the pivot directory for promptFile resolution.
func (a *Adapter) SetPivotDir(dir string) {
	a.pivotDir = dir
}

// Name returns the adapter identifier.
func (a *Adapter) Name() string {
	return "opencode"
}

// ValidateAgent checks that an agent definition is valid for OpenCode.
func (a *Adapter) ValidateAgent(agent pivot.AgentDefinition) error {
	if agent.Mode != "primary" && agent.Mode != "subagent" {
		return fmt.Errorf("agent %q: mode must be primary or subagent", agent.ID)
	}
	return nil
}

// Fragments returns accumulated JSON fragments for opencode.json merge.
func (a *Adapter) Fragments() map[string]any {
	return a.fragments
}

// ResetFragments clears accumulated fragments before a new generation pass.
func (a *Adapter) ResetFragments() {
	a.fragments = make(map[string]any)
}

// GenerateAgent produces prompt files and accumulates the JSON fragment.
func (a *Adapter) GenerateAgent(agent pivot.AgentDefinition) (map[string]string, error) {
	if err := a.ValidateAgent(agent); err != nil {
		return nil, err
	}

	fragment, promptRel, promptContent, err := GenerateAgentFragment(agent, a.pivotDir)
	if err != nil {
		return nil, err
	}

	a.fragments["agent."+agent.ID] = fragment

	files := map[string]string{}
	if promptContent != "" || agent.SystemPrompt != "" || agent.PromptFile != "" {
		files[filepath.Join(a.baseDir, promptRel)] = promptContent
	}
	return files, nil
}

// GenerateCommand produces command template files and accumulates the JSON fragment.
func (a *Adapter) GenerateCommand(cmd pivot.CommandDefinition) (map[string]string, error) {
	fragment, cmdRel, cmdContent, err := GenerateCommandFragment(cmd)
	if err != nil {
		return nil, err
	}

	a.fragments["command."+cmd.ID] = fragment

	return map[string]string{
		filepath.Join(a.baseDir, cmdRel): cmdContent,
	}, nil
}

// TargetPaths returns paths this adapter writes to.
func (a *Adapter) TargetPaths() []string {
	return []string{
		filepath.Join(a.baseDir, configFileName),
		filepath.Join(a.baseDir, "prompts"),
		filepath.Join(a.baseDir, "command"),
	}
}

// ConfigPath returns the full path to opencode.json.
func (a *Adapter) ConfigPath() string {
	return filepath.Join(a.baseDir, configFileName)
}

// MergeFile merges fragments into an existing opencode.json, preserving unrelated keys.
func (a *Adapter) MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error) {
	if !strings.HasSuffix(filepath.Base(path), configFileName) {
		return nil, nil
	}

	root := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("parse existing JSON: %w", err)
		}
	}

	for key, fragment := range fragments {
		root[key] = fragment
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return append(out, '\n'), nil
}
