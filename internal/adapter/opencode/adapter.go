package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
)

const configFileName = "opencode.json"
const fileMode = 0o644

// Adapter implements the OpenCode target adapter.
type Adapter struct {
	baseDir  string
	pivotDir string
}

// NewAdapter creates an OpenCode adapter writing to the default config directory.
func NewAdapter() *Adapter {
	return &Adapter{baseDir: fsutil.OpenCodePath()}
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
	return "opencode"
}

// ValidateAgent checks that an agent definition is valid for OpenCode.
func (a *Adapter) ValidateAgent(agent pivot.AgentDefinition) error {
	if agent.Mode != "primary" && agent.Mode != "subagent" {
		return fmt.Errorf("agent %q: mode must be primary or subagent", agent.ID)
	}
	return nil
}

// Generate produces prompt/command body files and accumulates the JSON
// fragments that a later MergeFile/PruneManaged folds into opencode.json.
// Fragments are collected in a local map, so the adapter holds no state
// between calls.
func (a *Adapter) Generate(pf *pivot.PivotFile) (adapter.GenerationResult, error) {
	var files []adapter.GeneratedFile
	fragments := make(map[string]any)

	for _, ag := range pf.Agents {
		if err := a.ValidateAgent(ag); err != nil {
			return adapter.GenerationResult{}, err
		}
		fragment, promptRel, promptContent, err := GenerateAgentFragment(ag, a.pivotDir)
		if err != nil {
			return adapter.GenerationResult{}, fmt.Errorf("generate agent %q: %w", ag.ID, err)
		}
		fragments["agent."+ag.ID] = fragment
		if promptContent != "" || ag.SystemPrompt != "" || ag.PromptFile != "" {
			files = append(files, adapter.GeneratedFile{
				Path:       filepath.Join(a.baseDir, promptRel),
				Content:    []byte(promptContent),
				Mode:       fileMode,
				Adapter:    a.Name(),
				ResourceID: ag.ID,
			})
		}
	}

	for _, cmd := range pf.Commands {
		fragment, cmdRel, cmdContent, err := GenerateCommandFragment(cmd)
		if err != nil {
			return adapter.GenerationResult{}, fmt.Errorf("generate command %q: %w", cmd.ID, err)
		}
		fragments["command."+cmd.ID] = fragment
		files = append(files, adapter.GeneratedFile{
			Path:       filepath.Join(a.baseDir, cmdRel),
			Content:    []byte(cmdContent),
			Mode:       fileMode,
			Adapter:    a.Name(),
			ResourceID: cmd.ID,
		})
	}

	return adapter.GenerationResult{Files: files, Fragments: fragments}, nil
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

// fragmentGroups are the nested containers shenron-managed fragments live under.
var fragmentGroups = []string{"agent", "command"}

// MergeFile upserts fragments into the nested agent/command objects of an existing
// opencode.json. Managed entries are created or updated in place; every other key —
// including native-only agents/commands and unrelated top-level config — is preserved
// verbatim, and the original key ordering is kept so pushes produce minimal diffs.
func (a *Adapter) MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error) {
	if !strings.HasSuffix(filepath.Base(path), configFileName) {
		return nil, nil
	}

	root, err := parseOrderedObject(existing)
	if err != nil {
		return nil, fmt.Errorf("parse existing JSON: %w", err)
	}

	// Group fragments by their container ("agent"/"command") and leaf id, seeding each
	// container from the existing object so native entries and their order survive.
	containers := map[string]*orderedObject{}
	for _, key := range sortedKeys(fragments) {
		group, leaf, ok := splitFragmentKey(key)
		if !ok {
			raw, err := json.Marshal(fragments[key])
			if err != nil {
				return nil, fmt.Errorf("marshal fragment %q: %w", key, err)
			}
			root.set(key, raw)
			continue
		}

		container := containers[group]
		if container == nil {
			container = newOrderedObject()
			if existingRaw, ok := root.get(group); ok {
				if container, err = parseOrderedObject(existingRaw); err != nil {
					return nil, fmt.Errorf("parse existing %q object: %w", group, err)
				}
			}
			containers[group] = container
		}

		raw, err := json.Marshal(fragments[key])
		if err != nil {
			return nil, fmt.Errorf("marshal fragment %q: %w", key, err)
		}
		container.set(leaf, raw)
	}

	groupNames := make([]string, 0, len(containers))
	for group := range containers {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)
	for _, group := range groupNames {
		raw, err := containers[group].compact()
		if err != nil {
			return nil, fmt.Errorf("serialize %q object: %w", group, err)
		}
		root.set(group, raw)
	}

	compact, err := root.compact()
	if err != nil {
		return nil, fmt.Errorf("serialize JSON: %w", err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return nil, fmt.Errorf("indent JSON: %w", err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// PruneManaged removes leaves listed in `managed` that shenron previously
// wrote but that the current `fragments` no longer provides. It then upserts
// the current fragments (same logic as MergeFile). Native-only leaves are
// always preserved.
func (a *Adapter) PruneManaged(path string, existing []byte, managed map[string][]string, fragments map[string]any) ([]byte, error) {
	if !strings.HasSuffix(filepath.Base(path), configFileName) {
		return nil, nil
	}
	root, err := parseOrderedObject(existing)
	if err != nil {
		return nil, fmt.Errorf("parse existing JSON: %w", err)
	}

	// Build the set of current leaf ids per group from fragments.
	current := map[string]map[string]struct{}{}
	for key := range fragments {
		group, leaf, ok := splitFragmentKey(key)
		if !ok {
			continue
		}
		if current[group] == nil {
			current[group] = map[string]struct{}{}
		}
		current[group][leaf] = struct{}{}
	}

	// Prune: remove managed leaves absent from current fragments.
	for _, group := range fragmentGroups {
		managedIDs, hasManaged := managed[group]
		if !hasManaged {
			continue
		}
		containerRaw, hasContainer := root.get(group)
		if !hasContainer {
			continue
		}
		container, err := parseOrderedObject(containerRaw)
		if err != nil {
			return nil, fmt.Errorf("parse existing %q object for prune: %w", group, err)
		}
		for _, id := range managedIDs {
			if _, stillGenerated := current[group][id]; stillGenerated {
				continue
			}
			container.delete(id)
		}
		raw, err := container.compact()
		if err != nil {
			return nil, fmt.Errorf("serialize %q after prune: %w", group, err)
		}
		root.set(group, raw)
	}

	rootRaw, err := root.compact()
	if err != nil {
		return nil, fmt.Errorf("serialize JSON after prune: %w", err)
	}
	return a.MergeFile(path, rootRaw, fragments)
}

func splitFragmentKey(key string) (group, leaf string, ok bool) {
	for _, group := range fragmentGroups {
		if strings.HasPrefix(key, group+".") {
			return group, key[len(group)+1:], true
		}
	}
	return "", "", false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
