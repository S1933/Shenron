package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/S1933/Shenron/internal/adapter"
	shenronpackage "github.com/S1933/Shenron/internal/package"
)

// ExplainedFile is one native file a package translates into for a target.
type ExplainedFile struct {
	ResourceID string `json:"resourceId,omitempty"`
	Adapter    string `json:"adapter"`
	Path       string `json:"path"`
	Content    string `json:"content"`
}

// ExplainReport is the structured output of `explain` (also the JSON shape).
type ExplainReport struct {
	Package string          `json:"package"`
	Target  string          `json:"target"`
	Files   []ExplainedFile `json:"files"`
}

// ExplainOptions configures the explain command for tests and embedding.
type ExplainOptions struct {
	Store    *shenronpackage.Store
	Name     string
	Target   string // required: claude-code | codex | opencode
	Adapters map[string]adapter.Adapter
	Format   string // "text" (default) or "json"
	Output   io.Writer
}

// RunExplain shows, for one target, the native files an installed package
// translates into. It is a pure preview: the translation is computed from the
// pivot alone, ignoring whatever is currently on disk, so it answers "what does
// this package become for target X?".
func RunExplain(opts ExplainOptions) error {
	if opts.Target == "" {
		return fmt.Errorf("explain requires --target (claude-code, codex, or opencode)")
	}
	format, err := parseOutputFormat(opts.Format)
	if err != nil {
		return err
	}

	store := packageStore(opts.Store)
	installed, pkg, err := store.Load(opts.Name)
	if err != nil {
		return err
	}
	output := packageOutput(opts.Output)

	adapters := opts.Adapters
	if adapters == nil {
		adapters, err = ResolveTargets(opts.Target)
		if err != nil {
			return err
		}
	}
	adpt, ok := adapters[opts.Target]
	if !ok {
		return errUnknownTarget(opts.Target)
	}

	if setter, ok := adpt.(adapter.PivotDirectoryAware); ok {
		setter.SetPivotDir(installed.Root)
	}

	result, err := adpt.Generate(pkg.Pivot)
	if err != nil {
		return fmt.Errorf("%s: %w", opts.Target, err)
	}
	files := result.Files

	// For merging adapters, fold the fragments into a fresh config so the
	// preview shows the opencode.json translation with no host entries.
	if merger, ok := adpt.(adapter.MergingAdapter); ok {
		merged, err := merger.MergeFile(merger.ConfigPath(), nil, result.Fragments)
		if err != nil {
			return fmt.Errorf("%s: merge preview: %w", opts.Target, err)
		}
		if merged != nil {
			files = append(files, adapter.GeneratedFile{
				Path:    merger.ConfigPath(),
				Content: merged,
				Adapter: opts.Target,
			})
		}
	}

	report := ExplainReport{Package: installed.Name, Target: opts.Target}
	for _, f := range files {
		report.Files = append(report.Files, ExplainedFile{
			ResourceID: f.ResourceID,
			Adapter:    f.Adapter,
			Path:       f.Path,
			Content:    string(f.Content),
		})
	}
	sort.Slice(report.Files, func(i, j int) bool { return report.Files[i].Path < report.Files[j].Path })

	if format == formatJSON {
		return writeJSON(output, report)
	}
	return writeExplainText(output, report)
}

func writeExplainText(w io.Writer, report ExplainReport) error {
	if _, err := fmt.Fprintf(w, "%s -> %s\n", report.Package, report.Target); err != nil {
		return err
	}
	for _, f := range report.Files {
		label := f.Path
		if f.ResourceID != "" {
			label = fmt.Sprintf("%s (from %s)", f.Path, f.ResourceID)
		}
		if _, err := fmt.Fprintf(w, "\n--- %s ---\n%s\n", label, f.Content); err != nil {
			return err
		}
	}
	return nil
}
