package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/cli"
	shenronpackage "github.com/S1933/Shenron/internal/package"
)

func installExplainPackage(t *testing.T) *shenronpackage.Store {
	t.Helper()
	source := writeCLIPackage(t, "1.0.0")
	writeCLIFile(t, filepath.Join(source, shenronpackage.PivotFileName), `version: "1"
agents:
  - id: build
    description: Build the project.
    mode: subagent
    systemPrompt: Build carefully.
commands: []
`)
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source, Output: io.Discard}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestExplainRequiresTarget(t *testing.T) {
	store := installExplainPackage(t)
	if err := cli.RunExplain(cli.ExplainOptions{Store: store, Name: "acme-reviewers", Output: io.Discard}); err == nil {
		t.Fatal("expected error when --target is missing")
	}
}

func TestExplainCodexJSON(t *testing.T) {
	store := installExplainPackage(t)
	var buf bytes.Buffer
	if err := cli.RunExplain(cli.ExplainOptions{Store: store, Name: "acme-reviewers", Target: "codex", Format: "json", Output: &buf}); err != nil {
		t.Fatalf("explain: %v", err)
	}

	var report cli.ExplainReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("explain output is not valid JSON: %v\n%s", err, buf.String())
	}
	if report.Target != "codex" || report.Package != "acme-reviewers" {
		t.Fatalf("unexpected report header: %+v", report)
	}

	var found bool
	for _, f := range report.Files {
		if f.ResourceID == "build" {
			found = true
			if !strings.Contains(f.Content, "name = 'build'") {
				t.Errorf("codex agent content missing native name:\n%s", f.Content)
			}
		}
	}
	if !found {
		t.Errorf("expected a file translated from the build agent, got %+v", report.Files)
	}
}

func TestExplainOpenCodeIncludesConfig(t *testing.T) {
	store := installExplainPackage(t)
	var buf bytes.Buffer
	if err := cli.RunExplain(cli.ExplainOptions{Store: store, Name: "acme-reviewers", Target: "opencode", Output: &buf}); err != nil {
		t.Fatalf("explain: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "opencode.json") {
		t.Errorf("expected merged opencode.json in preview, got:\n%s", out)
	}
	if !strings.Contains(out, "\"build\"") {
		t.Errorf("expected the build agent entry in the config preview, got:\n%s", out)
	}
}

func TestExplainUnknownTarget(t *testing.T) {
	store := installExplainPackage(t)
	if err := cli.RunExplain(cli.ExplainOptions{Store: store, Name: "acme-reviewers", Target: "emacs", Output: io.Discard}); err == nil {
		t.Fatal("expected error for unknown target")
	}
}
