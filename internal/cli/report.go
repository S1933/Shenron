package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/diff"
)

// outputFormat selects how diff/push results are rendered.
type outputFormat string

const (
	formatText outputFormat = "text"
	formatJSON outputFormat = "json"
)

// parseOutputFormat validates a user-supplied --output value.
func parseOutputFormat(s string) (outputFormat, error) {
	switch s {
	case "", "text":
		return formatText, nil
	case "json":
		return formatJSON, nil
	default:
		return "", fmt.Errorf("unknown output format %q (want text or json)", s)
	}
}

// FileReport is one file's entry in a structured diff/push report.
type FileReport struct {
	Path       string `json:"path"`
	Adapter    string `json:"adapter"`
	Status     string `json:"status"`
	ResourceID string `json:"resourceId,omitempty"`
}

// DiffReport is the JSON shape emitted by `diff --output json`.
type DiffReport struct {
	Files      []FileReport `json:"files"`
	Orphaned   []string     `json:"orphaned,omitempty"`
	HasChanges bool         `json:"hasChanges"`
}

// PushReport is the JSON shape emitted by `push --output json`.
type PushReport struct {
	Written  []FileReport `json:"written"`
	Orphaned []string     `json:"orphaned,omitempty"`
	Wrote    bool         `json:"wrote"`
}

// diffStatusJSON maps a diff status onto a stable machine-readable token.
func diffStatusJSON(status diff.DiffStatus) string {
	switch status {
	case diff.StatusCreated:
		return "created"
	case diff.StatusModified:
		return "modified"
	case diff.StatusManuallyModified:
		return "manually-modified"
	case diff.StatusOrphaned:
		return "orphaned"
	default:
		return "unchanged"
	}
}

// resourceIDByPath indexes generated files so a report can annotate each path
// with the pivot resource it came from.
func resourceIDByPath(generated map[string][]adapter.GeneratedFile) map[string]string {
	out := map[string]string{}
	for _, files := range generated {
		for _, f := range files {
			out[f.Path] = f.ResourceID
		}
	}
	return out
}

// writeJSON marshals v as indented JSON with a trailing newline.
func writeJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// buildDiffReport computes the full structured diff across every adapter.
func buildDiffReport(generated map[string][]adapter.GeneratedFile, state *diff.StateFile, scope *diff.OrphanScope) (DiffReport, error) {
	ids := resourceIDByPath(generated)
	report := DiffReport{}

	for _, name := range sortedAdapterNames(generated) {
		results, err := diff.ComputeDiffs(contentMap(generated[name]), state, scope)
		if err != nil {
			return DiffReport{}, err
		}
		results = diff.FilterOrphaned(results)
		if diff.HasChanges(results) {
			report.HasChanges = true
		}
		for _, r := range results {
			report.Files = append(report.Files, FileReport{
				Path:       r.Path,
				Adapter:    name,
				Status:     diffStatusJSON(r.Status),
				ResourceID: ids[r.Path],
			})
		}
	}

	allResults, err := diff.ComputeDiffs(mergeGenerated(generated), state, scope)
	if err != nil {
		return DiffReport{}, err
	}
	for _, r := range diff.OrphanedOnly(allResults) {
		report.Orphaned = append(report.Orphaned, r.Path)
	}
	sort.Strings(report.Orphaned)
	if len(report.Orphaned) > 0 {
		report.HasChanges = true
	}

	return report, nil
}

// buildPushReport assembles the structured summary of a completed push.
func buildPushReport(logs []writeLog, orphans []diff.DiffResult, generated map[string][]adapter.GeneratedFile) PushReport {
	ids := resourceIDByPath(generated)
	report := PushReport{Wrote: len(logs) > 0}
	for _, l := range logs {
		report.Written = append(report.Written, FileReport{
			Path:       l.path,
			Adapter:    l.name,
			Status:     diffStatusJSON(l.status),
			ResourceID: ids[l.path],
		})
	}
	for _, r := range orphans {
		report.Orphaned = append(report.Orphaned, r.Path)
	}
	sort.Strings(report.Orphaned)
	return report
}
