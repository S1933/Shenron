package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/adapter/claude"
	"github.com/S1933/Shenron/internal/adapter/opencode"
	"github.com/S1933/Shenron/internal/diff"
	"github.com/S1933/Shenron/internal/pivot"
)

const configFileMode = 0o644

// Generate produces the generated files for each adapter from a parsed pivot
// file. state may be nil; when set, adapters that implement adapter.ManagedPruner
// use it to prune leaves they previously managed but the pivot no longer
// generates.
func Generate(pf *pivot.PivotFile, pivotDir string, adapters map[string]adapter.Adapter, state *diff.StateFile) (map[string][]adapter.GeneratedFile, error) {
	out := make(map[string][]adapter.GeneratedFile, len(adapters))

	for name, adpt := range adapters {
		if setter, ok := adpt.(adapter.PivotDirectoryAware); ok {
			setter.SetPivotDir(pivotDir)
		}

		result, err := adpt.Generate(pf)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		files := result.Files

		if merger, ok := adpt.(adapter.MergingAdapter); ok {
			configFile, err := mergeConfig(name, merger, adpt, result.Fragments, state)
			if err != nil {
				return nil, err
			}
			if configFile != nil {
				files = append(files, *configFile)
			}
		}

		out[name] = files
	}

	return out, nil
}

// mergeConfig folds accumulated fragments into the adapter's shared config file,
// pruning previously-managed leaves first when the adapter and state support it.
func mergeConfig(name string, merger adapter.MergingAdapter, adpt adapter.Adapter, fragments map[string]any, state *diff.StateFile) (*adapter.GeneratedFile, error) {
	configPath := merger.ConfigPath()

	var existing []byte
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("%s: read %s: %w", name, filepath.Base(configPath), err)
		}
	} else {
		existing = data
	}

	var merged []byte
	if pruner, ok := adpt.(adapter.ManagedPruner); ok && state != nil {
		merged, err = pruner.PruneManaged(configPath, existing, state.Managed(configPath), fragments)
	} else {
		merged, err = merger.MergeFile(configPath, existing, fragments)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: merge %s: %w", name, filepath.Base(configPath), err)
	}
	if merged == nil {
		return nil, nil
	}

	return &adapter.GeneratedFile{
		Path:    configPath,
		Content: merged,
		Mode:    configFileMode,
		Adapter: name,
	}, nil
}

// Ensure adapters satisfy the optional capability interfaces at compile time.
var (
	_ adapter.PivotDirectoryAware = (*claude.Adapter)(nil)
	_ adapter.PivotDirectoryAware = (*opencode.Adapter)(nil)
	_ adapter.MergingAdapter      = (*opencode.Adapter)(nil)
	_ adapter.ManagedPruner       = (*opencode.Adapter)(nil)
)
