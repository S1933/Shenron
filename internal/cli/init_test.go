package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnuel/agentsync/internal/cli"
)

func TestInitRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agentsync.yaml"), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cli.RunInit(cli.InitOptions{WorkDir: dir})
	if err == nil {
		t.Fatal("expected error when agentsync.yaml exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInitFromOpenCode(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join("testdata", "init", "opencode")
	opencodeDir := filepath.Join(dir, "opencode")
	if err := copyDir(fixture, opencodeDir); err != nil {
		t.Fatal(err)
	}

	if err := cli.RunInit(cli.InitOptions{WorkDir: dir, OpenCodeDir: opencodeDir, ClaudeDir: filepath.Join(dir, "missing-claude")}); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "agentsync.yaml")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"version:", "agents:", "id: build", "id: ship", "systemPrompt:"} {
		if !strings.Contains(content, want) {
			t.Errorf("output missing %q:\n%s", want, content)
		}
	}

	if !strings.Contains(content, "opencode:\n        model: anthropic/claude-sonnet-4-5") {
		t.Errorf("expected model under extensions.opencode, got:\n%s", content)
	}
}

func TestInitFromClaudeCode(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join("testdata", "init", "claude")
	claudeDir := filepath.Join(dir, "claude")
	if err := copyDir(fixture, claudeDir); err != nil {
		t.Fatal(err)
	}

	if err := cli.RunInit(cli.InitOptions{WorkDir: dir, ClaudeDir: claudeDir, OpenCodeDir: filepath.Join(dir, "missing-opencode")}); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "agentsync.yaml")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"version:", "agents:", "id: build", "commands:", "id: ship", "permissions:"} {
		if !strings.Contains(content, want) {
			t.Errorf("output missing %q:\n%s", want, content)
		}
	}

	if !strings.Contains(content, "claude:\n        model: anthropic/claude-sonnet-4-5") {
		t.Errorf("expected model under extensions.claude, got:\n%s", content)
	}
}

func TestInitFallsBackToClaudeOnBrokenOpenCode(t *testing.T) {
	dir := t.TempDir()
	claudeFixture := filepath.Join("testdata", "init", "claude")
	claudeDir := filepath.Join(dir, "claude")
	if err := copyDir(claudeFixture, claudeDir); err != nil {
		t.Fatal(err)
	}

	opencodeDir := filepath.Join(dir, "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opencodeDir, "opencode.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cli.RunInit(cli.InitOptions{WorkDir: dir, OpenCodeDir: opencodeDir, ClaudeDir: claudeDir}); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "agentsync.yaml")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "id: build") {
		t.Errorf("expected Claude bootstrap output, got:\n%s", content)
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
