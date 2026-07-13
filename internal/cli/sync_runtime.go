package cli

// This file is the internal sync runtime shared by the package flow. It
// exists so the package flow has a single point of contact with the pivot
// parser, the diff engine, and the atomic write primitives. There is no
// user-facing CLI here; the only exported entry points are programmatic
// helpers consumed by tests.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/diff"
	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
)

// ErrManualEdits indicates push was refused due to manual native edits.
var ErrManualEdits = errors.New("manual edits detected")

// DiffOptions configures diff for testing and programmatic use.
type DiffOptions struct {
	ConfigPath string
	Target     string
	Adapters   map[string]adapter.Adapter
}

// RunDiff shows differences between pivot and native configs.
func RunDiff(opts DiffOptions) error {
	return runDiffAt(opts.ConfigPath, opts.Target, opts.Adapters, "", os.Stdout, os.Stderr)
}

// CaptureOutput runs fn while capturing stdout and stderr separately.
func CaptureOutput(fn func() error) (stdout, stderr string, err error) {
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		_ = wOut.Close()
		return "", "", err
	}
	os.Stdout = wOut
	os.Stderr = wErr

	runErr := fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	outData, _ := io.ReadAll(rOut)
	errData, _ := io.ReadAll(rErr)
	return string(outData), string(errData), runErr
}

// PushOptions configures push for testing and programmatic use.
type PushOptions struct {
	ConfigPath string
	Target     string
	Force      bool
	Adapters   map[string]adapter.Adapter
}

// RunPush pushes pivot config to native CLI configs.
func RunPush(opts PushOptions) error {
	return runPushAt(opts.ConfigPath, opts.Target, opts.Force, opts.Adapters, "", nil, nil, os.Stdout, os.Stderr)
}

type pushPreflight func(generated map[string][]adapter.GeneratedFile, state *diff.StateFile, adapters map[string]adapter.Adapter) error
type pushPostflight func(generated map[string][]adapter.GeneratedFile, state *diff.StateFile) error

// runDiffAt and runPushAt are the library entry points used by both the
// public Go API and the package flow. They accept an explicit stateDir so the
// package flow can keep its state under ~/.shenron/packages/state/<name>/.

func runDiffAt(configPath, target string, adapters map[string]adapter.Adapter, stateDir string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	_, generated, state, resolved, err := prepareSyncAt(configPath, target, adapters, stateDir)
	if err != nil {
		return err
	}

	scope := buildOrphanScope(resolved)
	colored := diff.SupportsColor()
	hasChanges := false

	for _, name := range sortedAdapterNames(generated) {
		files := generated[name]
		results, err := diff.ComputeDiffs(contentMap(files), state, scope)
		if err != nil {
			return err
		}
		results = diff.FilterOrphaned(results)

		if !diff.HasChanges(results) {
			fmt.Fprintf(stdout, "[%s] No changes\n", name)
			continue
		}

		hasChanges = true
		fmt.Fprintf(stdout, "[%s]\n", name)
		fmt.Fprint(stdout, diff.FormatDiff(results, colored))
	}

	merged := mergeGenerated(generated)
	allResults, err := diff.ComputeDiffs(merged, state, scope)
	if err != nil {
		return err
	}
	orphaned := diff.OrphanedOnly(allResults)
	sort.Slice(orphaned, func(i, j int) bool {
		return orphaned[i].Path < orphaned[j].Path
	})
	for _, r := range orphaned {
		hasChanges = true
		fmt.Fprintf(stderr, "warning: orphaned %s (removed from pivot, still on disk)\n", r.Path)
	}

	if !hasChanges {
		fmt.Fprintln(stdout, "No changes")
	}

	return nil
}

func runPushAt(configPath, target string, force bool, adapters map[string]adapter.Adapter, stateDir string, preflight pushPreflight, postflight pushPostflight, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	pivotDir, generated, state, adapters, err := prepareSyncAt(configPath, target, adapters, stateDir)
	if err != nil {
		return err
	}
	if stateDir == "" {
		stateDir = pivotDir
	}
	if preflight != nil {
		if err := preflight(generated, state, adapters); err != nil {
			return err
		}
	}

	// Persist state (including Managed) before native writes so a crash mid-push
	// never leaves the package blocked on its own entries.
	if err := diff.SaveState(stateDir, state); err != nil {
		return err
	}

	scope := buildOrphanScope(adapters)
	merged := mergeGenerated(generated)
	results, err := diff.ComputeDiffs(merged, state, scope)
	if err != nil {
		return err
	}

	if diff.HasManualEdits(results) && !force {
		paths := diff.ManualEditPaths(results)
		sort.Strings(paths)
		var b strings.Builder
		b.WriteString("refusing to push: manually edited files detected:\n")
		for _, p := range paths {
			fmt.Fprintf(&b, "  %s\n", p)
		}
		b.WriteString("use --force to overwrite")
		return fmt.Errorf("%w: %s", ErrManualEdits, strings.TrimSpace(b.String()))
	}

	printOrphanWarnings(stderr, diff.OrphanedOnly(results))

	wroteAny := false
	for _, name := range sortedAdapterNames(generated) {
		files := generated[name]
		byPath := indexByPath(files)
		adapterResults, err := diff.ComputeDiffs(contentMap(files), state, scope)
		if err != nil {
			return err
		}
		adapterResults = diff.FilterOrphaned(adapterResults)

		for _, r := range adapterResults {
			switch r.Status {
			case diff.StatusCreated, diff.StatusModified, diff.StatusManuallyModified:
				gf := byPath[r.Path]
				if err := fsutil.WriteFileAtomic(r.Path, gf.Content, gf.Mode); err != nil {
					return fmt.Errorf("write %s: %w", r.Path, err)
				}
				state.SetFile(r.Path, name, gf.Content)
				fmt.Fprintf(stdout, "[%s] wrote %s (%s)\n", name, r.Path, diffStatusName(r.Status))
				wroteAny = true
			case diff.StatusUnchanged:
				state.SetFile(r.Path, name, []byte(r.NewContent))
			}
		}
	}
	if postflight != nil {
		if err := postflight(generated, state); err != nil {
			return err
		}
	}

	if err := diff.SaveState(stateDir, state); err != nil {
		return err
	}

	if !wroteAny {
		fmt.Fprintln(stdout, "No changes")
	} else {
		fmt.Fprintf(stdout, "state updated: %s\n", filepath.Join(stateDir, ".shenron-state.json"))
	}

	return nil
}

func diffStatusName(status diff.DiffStatus) string {
	switch status {
	case diff.StatusCreated:
		return "created"
	case diff.StatusModified:
		return "modified"
	case diff.StatusManuallyModified:
		return "forced"
	default:
		return "updated"
	}
}

func printOrphanWarnings(stderr io.Writer, results []diff.DiffResult) {
	for _, r := range results {
		if r.Status == diff.StatusOrphaned {
			fmt.Fprintf(stderr, "warning: orphaned %s (removed from pivot, still on disk)\n", r.Path)
		}
	}
}

// mergeGenerated flattens every adapter's files into a single path->content map
// for whole-tree diff and orphan detection.
func mergeGenerated(generated map[string][]adapter.GeneratedFile) map[string]string {
	merged := make(map[string]string)
	for _, files := range generated {
		for _, f := range files {
			merged[f.Path] = string(f.Content)
		}
	}
	return merged
}

// contentMap projects a slice of generated files onto a path->content map for
// the diff engine.
func contentMap(files []adapter.GeneratedFile) map[string]string {
	out := make(map[string]string, len(files))
	for _, f := range files {
		out[f.Path] = string(f.Content)
	}
	return out
}

// indexByPath keys generated files by their destination path so the write loop
// can recover each file's mode and content.
func indexByPath(files []adapter.GeneratedFile) map[string]adapter.GeneratedFile {
	out := make(map[string]adapter.GeneratedFile, len(files))
	for _, f := range files {
		out[f.Path] = f
	}
	return out
}

func prepareSyncAt(configPath, target string, adapters map[string]adapter.Adapter, stateDir string) (pivotDir string, generated map[string][]adapter.GeneratedFile, state *diff.StateFile, resolved map[string]adapter.Adapter, err error) {
	path, err := pivot.Discover(configPath)
	if err != nil {
		return "", nil, nil, nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, nil, nil, fmt.Errorf("read pivot file: %w", err)
	}

	pivotDir = filepath.Dir(path)
	pf, err := pivot.Parse(data, pivotDir)
	if err != nil {
		return "", nil, nil, nil, err
	}

	if adapters == nil {
		adapters, err = ResolveTargets(target)
		if err != nil {
			return "", nil, nil, nil, err
		}
	}

	if stateDir == "" {
		stateDir = pivotDir
	}
	state, err = diff.LoadState(stateDir)
	if err != nil {
		return "", nil, nil, nil, err
	}

	generated, err = Generate(pf, pivotDir, adapters, state)
	if err != nil {
		return "", nil, nil, nil, err
	}

	return pivotDir, generated, state, adapters, nil
}

func buildOrphanScope(adapters map[string]adapter.Adapter) *diff.OrphanScope {
	if len(adapters) == 0 {
		return nil
	}
	scope := &diff.OrphanScope{}
	for name, adpt := range adapters {
		scope.AdapterNames = append(scope.AdapterNames, name)
		scope.PathPrefixes = append(scope.PathPrefixes, adpt.TargetPaths()...)
	}
	sort.Strings(scope.AdapterNames)
	sort.Strings(scope.PathPrefixes)
	return scope
}

func sortedAdapterNames(generated map[string][]adapter.GeneratedFile) []string {
	names := make([]string, 0, len(generated))
	for name := range generated {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
