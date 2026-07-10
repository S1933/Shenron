package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

type fileChange struct {
	path   string
	status string // created, modified, unchanged
	diff   string
}

func compareFiles(generated map[string]string) ([]fileChange, bool) {
	var changes []fileChange
	hasChanges := false

	for path, content := range generated {
		existing, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			changes = append(changes, fileChange{
				path:   path,
				status: "created",
				diff:   unifiedDiff(path, "", content),
			})
			hasChanges = true
			continue
		}
		if err != nil {
			changes = append(changes, fileChange{
				path:   path,
				status: "modified",
				diff:   fmt.Sprintf("  (could not read existing file: %v)\n%s", err, unifiedDiff(path, "", content)),
			})
			hasChanges = true
			continue
		}

		if string(existing) == content {
			changes = append(changes, fileChange{path: path, status: "unchanged"})
			continue
		}

		changes = append(changes, fileChange{
			path:   path,
			status: "modified",
			diff:   unifiedDiff(path, string(existing), content),
		})
		hasChanges = true
	}

	return changes, hasChanges
}

func printDiffReport(changes []fileChange, hasChanges bool) {
	for _, c := range changes {
		switch c.status {
		case "created":
			fmt.Printf("  create  %s\n", c.path)
		case "modified":
			fmt.Printf("  modify  %s\n", c.path)
		case "unchanged":
			fmt.Printf("  ok      %s\n", c.path)
		}
		if c.diff != "" {
			fmt.Println(c.diff)
		}
	}
}

func unifiedDiff(path, oldContent, newContent string) string {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	var buf strings.Builder
	fmt.Fprintf(&buf, "--- %s\n", path)
	fmt.Fprintf(&buf, "+++ %s\n", path)

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
			fmt.Fprintf(&buf, "-%s\n", oldLine)
		}
		if i < len(newLines) {
			fmt.Fprintf(&buf, "+%s\n", newLine)
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
