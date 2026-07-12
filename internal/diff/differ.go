package diff

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiffStatus describes how a generated file compares to disk and state.
type DiffStatus int

const (
	StatusUnchanged DiffStatus = iota
	StatusCreated
	StatusModified
	StatusDeleted
	StatusManuallyModified
	StatusOrphaned
)

// DiffResult holds comparison details for one file.
type DiffResult struct {
	Path       string
	Status     DiffStatus
	OldContent string
	NewContent string
}

// OrphanScope limits orphan detection to specific adapters (e.g. when --target is set).
// Nil scope checks all tracked files. Non-nil scope with AdapterNames set only reports
// orphans owned by those adapters.
type OrphanScope struct {
	AdapterNames []string
	PathPrefixes []string
}

// ComputeDiffs compares generated content against disk and the last push state.
func ComputeDiffs(generated map[string]string, state *StateFile, scope *OrphanScope) ([]DiffResult, error) {
	if state == nil {
		state = emptyState()
	}

	paths := make([]string, 0, len(generated))
	for path := range generated {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var results []DiffResult
	seen := make(map[string]struct{}, len(generated))

	for _, path := range paths {
		newContent := generated[path]
		seen[path] = struct{}{}

		existing, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			results = append(results, DiffResult{
				Path:       path,
				Status:     StatusCreated,
				NewContent: newContent,
			})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		oldContent := string(existing)
		if oldContent == newContent {
			results = append(results, DiffResult{
				Path:       path,
				Status:     StatusUnchanged,
				OldContent: oldContent,
				NewContent: newContent,
			})
			continue
		}

		status := StatusModified
		if fileState, ok := state.Files[path]; ok {
			existingHash := HashContent(existing)
			if existingHash != fileState.Hash {
				status = StatusManuallyModified
			}
		}

		results = append(results, DiffResult{
			Path:       path,
			Status:     status,
			OldContent: oldContent,
			NewContent: newContent,
		})
	}

	statePaths := make([]string, 0, len(state.Files))
	for path := range state.Files {
		statePaths = append(statePaths, path)
	}
	sort.Strings(statePaths)

	for _, path := range statePaths {
		if _, ok := seen[path]; !ok {
			fileState := state.Files[path]
			if scope != nil && len(scope.AdapterNames) > 0 && !orphanInScope(scope, path, fileState.Adapter) {
				continue
			}
			oldContent, _ := os.ReadFile(path)
			results = append(results, DiffResult{
				Path:       path,
				Status:     StatusOrphaned,
				OldContent: string(oldContent),
			})
		}
	}

	return results, nil
}

func orphanInScope(scope *OrphanScope, path, adapter string) bool {
	if adapter != "" {
		for _, name := range scope.AdapterNames {
			if name == adapter {
				return true
			}
		}
		return false
	}
	for _, prefix := range scope.PathPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// FormatDiff renders diff results as unified diff output, optionally colorized.
func FormatDiff(results []DiffResult, colored bool) string {
	var buf strings.Builder
	for _, r := range results {
		switch r.Status {
		case StatusUnchanged:
			continue
		case StatusOrphaned:
			writeLine(&buf, colored, colorYellow, fmt.Sprintf("warning: orphaned %s (removed from pivot, still on disk)", r.Path))
			continue
		case StatusManuallyModified:
			writeLine(&buf, colored, colorYellow, fmt.Sprintf("warning: manually modified %s", r.Path))
		}

		header := statusLabel(r.Status)
		writeLine(&buf, colored, colorBold, fmt.Sprintf("%s %s", header, r.Path))

		if r.Status == StatusCreated || r.Status == StatusModified || r.Status == StatusManuallyModified {
			buf.WriteString(unifiedDiff(r.Path, r.OldContent, r.NewContent, colored))
		}
	}
	return buf.String()
}

func statusLabel(status DiffStatus) string {
	switch status {
	case StatusCreated:
		return "create"
	case StatusModified:
		return "modify"
	case StatusDeleted:
		return "delete"
	case StatusManuallyModified:
		return "manual"
	default:
		return "change"
	}
}

func unifiedDiff(path, oldContent, newContent string, colored bool) string {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	var buf strings.Builder
	writeLine(&buf, colored, colorNone, fmt.Sprintf("--- a/%s", path))
	writeLine(&buf, colored, colorNone, fmt.Sprintf("+++ b/%s", path))

	max := len(oldLines)
	if len(newLines) > max {
		max = len(newLines)
	}

	for i := 0; i < max; i++ {
		var oldLine, newLine string
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}
		if oldLine == newLine {
			continue
		}
		if i < len(oldLines) {
			writeLine(&buf, colored, colorRed, "-"+oldLine)
		}
		if i < len(newLines) {
			writeLine(&buf, colored, colorGreen, "+"+newLine)
		}
	}
	return buf.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	sc := bufio.NewScanner(bytes.NewReader([]byte(s)))
	sc.Split(bufio.ScanLines)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

const (
	colorNone   = ""
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBold   = "\033[1m"
	colorReset  = "\033[0m"
)

func writeLine(buf *strings.Builder, colored bool, color, line string) {
	if colored && color != "" {
		buf.WriteString(color)
	}
	buf.WriteString(line)
	if colored && color != "" {
		buf.WriteString(colorReset)
	}
	buf.WriteByte('\n')
}

// SupportsColor reports whether stdout is a terminal and coloring is allowed.
func SupportsColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// FilterOrphaned removes orphaned entries from diff results.
func FilterOrphaned(results []DiffResult) []DiffResult {
	filtered := make([]DiffResult, 0, len(results))
	for _, r := range results {
		if r.Status != StatusOrphaned {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// OrphanedOnly returns only orphaned diff results.
func OrphanedOnly(results []DiffResult) []DiffResult {
	var orphaned []DiffResult
	for _, r := range results {
		if r.Status == StatusOrphaned {
			orphaned = append(orphaned, r)
		}
	}
	return orphaned
}

// HasManualEdits returns true if any result is manually modified.
func HasManualEdits(results []DiffResult) bool {
	for _, r := range results {
		if r.Status == StatusManuallyModified {
			return true
		}
	}
	return false
}

// ManualEditPaths returns paths with manual modifications.
func ManualEditPaths(results []DiffResult) []string {
	var paths []string
	for _, r := range results {
		if r.Status == StatusManuallyModified {
			paths = append(paths, r.Path)
		}
	}
	return paths
}

// HasChanges returns true if any result would change disk state.
func HasChanges(results []DiffResult) bool {
	for _, r := range results {
		switch r.Status {
		case StatusCreated, StatusModified, StatusManuallyModified, StatusDeleted:
			return true
		}
	}
	return false
}
