package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/adapter/claude"
	"github.com/S1933/Shenron/internal/adapter/opencode"
	"github.com/S1933/Shenron/internal/cli"
	"github.com/S1933/Shenron/internal/pivot"
)

func TestGenerateOpenCode(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join("..", "adapter", "opencode", "testdata")
	data, err := os.ReadFile(filepath.Join(dir, "shenron.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pf, err := pivot.Parse(data, dir)
	if err != nil {
		t.Fatal(err)
	}

	adapters := map[string]adapter.Adapter{
		"opencode": opencode.NewAdapterWithBaseDir(tmp, dir),
	}

	out, err := cli.Generate(pf, dir, adapters)
	if err != nil {
		t.Fatal(err)
	}

	files := out["opencode"]
	if len(files) < 4 {
		t.Fatalf("expected at least 4 files, got %d", len(files))
	}

	foundConfig := false
	for path := range files {
		if filepath.Base(path) == "opencode.json" {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Error("missing opencode.json in generated files")
	}
}

func TestGenerateConfigReadError(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join("..", "adapter", "opencode", "testdata")
	configPath := filepath.Join(tmp, "opencode.json")
	if err := os.WriteFile(configPath, []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0o644) })

	data, err := os.ReadFile(filepath.Join(dir, "shenron.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pf, err := pivot.Parse(data, dir)
	if err != nil {
		t.Fatal(err)
	}

	adapters := map[string]adapter.Adapter{
		"opencode": opencode.NewAdapterWithBaseDir(tmp, dir),
	}

	_, err = cli.Generate(pf, dir, adapters)
	if err == nil {
		t.Fatal("expected error when existing config is unreadable")
	}
}

func TestResolveTargetsUnknown(t *testing.T) {
	_, err := cli.ResolveTargets("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestResolveTargetsCodex(t *testing.T) {
	targets, err := cli.ResolveTargets("codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := targets["codex"]; !ok {
		t.Fatalf("Codex target was not resolved: %#v", targets)
	}
}

func TestRunDiffSeparatesPayloadAndWarnings(t *testing.T) {
	dir := t.TempDir()
	pivotPath := filepath.Join(dir, "shenron.yaml")
	if err := os.WriteFile(pivotPath, []byte("version: \"1\"\nagents:\n  - id: build\n    description: Build\n    mode: primary\n    systemPrompt: hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-populate state with an orphaned path to trigger a stderr warning.
	statePath := filepath.Join(dir, ".shenron-state.json")
	if err := os.WriteFile(statePath, []byte(`{"version":"1","files":{"orphan.md":{"hash":"x","path":"orphan.md","adapter":"claude-code"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := cli.CaptureOutput(func() error {
		return cli.RunDiff(cli.DiffOptions{
			ConfigPath: pivotPath,
			Adapters: map[string]adapter.Adapter{
				"claude-code": claude.NewAdapterWithBaseDir(dir, dir),
			},
		})
	})
	if err != nil {
		t.Fatalf("RunDiff: %v", err)
	}

	if strings.Contains(stdout, "warning:") {
		t.Errorf("stdout should not contain warnings, got: %s", stdout)
	}
	if !strings.Contains(stderr, "warning:") {
		t.Errorf("stderr should contain orphan warning, got: %q", stderr)
	}
}
