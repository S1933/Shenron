package opencode_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jnuel/agentsync/internal/adapter/opencode"
	"github.com/jnuel/agentsync/internal/pivot"
)

func testPivot(t *testing.T) (*pivot.PivotFile, string) {
	t.Helper()
	dir := filepath.Join("testdata")
	data, err := os.ReadFile(filepath.Join(dir, "agentsync.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pf, err := pivot.Parse(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	return pf, dir
}

func TestDumpGolden(t *testing.T) {
	if os.Getenv("WRITE_GOLDEN") == "" {
		t.Skip("set WRITE_GOLDEN=1 to regenerate golden files")
	}
	pf, pivotDir := testPivot(t)
	a := opencode.NewAdapter()
	fragments := map[string]any{}
	for _, agent := range pf.Agents {
		fragment, _, _, err := opencode.GenerateAgentFragment(agent, pivotDir)
		if err != nil {
			t.Fatal(err)
		}
		fragments["agent."+agent.ID] = fragment
	}
	for _, cmd := range pf.Commands {
		fragment, _, _, err := opencode.GenerateCommandFragment(cmd)
		if err != nil {
			t.Fatal(err)
		}
		fragments["command."+cmd.ID] = fragment
	}
	merged, err := a.MergeFile("opencode.json", nil, fragments)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("testdata", "expected_opencode.json"), merged, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateAgent(t *testing.T) {
	pf, pivotDir := testPivot(t)
	build := pf.Agents[0]

	fragment, promptPath, promptContent, err := opencode.GenerateAgentFragment(build, pivotDir)
	if err != nil {
		t.Fatal(err)
	}

	if promptPath != filepath.Join("prompts", "build.md") {
		t.Errorf("promptPath = %q, want prompts/build.md", promptPath)
	}

	expectedPrompt, err := os.ReadFile(filepath.Join("testdata", "expected_prompts_build.md"))
	if err != nil {
		t.Fatal(err)
	}
	if promptContent != string(expectedPrompt) {
		t.Errorf("prompt content mismatch:\ngot:\n%q\nwant:\n%q", promptContent, string(expectedPrompt))
	}

	want := map[string]any{
		"description": "Build and deploy agent",
		"model":       "anthropic/claude-sonnet-4-5",
		"temperature": 0.7,
		"prompt":      "{file:./prompts/build.md}",
		"steps":       50,
		"permission": map[string]any{
			"glob":     "allow",
			"grep":     "allow",
			"list":     "allow",
			"lsp":      "deny",
			"edit":     "ask",
			"webfetch": "deny",
			"bash": map[string]any{
				"go *":  "allow",
				"npm *": "allow",
			},
		},
	}

	if !reflect.DeepEqual(fragment, want) {
		gotJSON, _ := json.MarshalIndent(fragment, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("fragment mismatch:\ngot:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
}

func TestGenerateCommand(t *testing.T) {
	pf, _ := testPivot(t)
	ship := pf.Commands[0]

	fragment, cmdPath, cmdContent, err := opencode.GenerateCommandFragment(ship)
	if err != nil {
		t.Fatal(err)
	}

	if cmdPath != filepath.Join("command", "ship.md") {
		t.Errorf("cmdPath = %q, want command/ship.md", cmdPath)
	}

	wantContent := "Review and ship the current changes.\n"
	if cmdContent != wantContent {
		t.Errorf("cmdContent = %q, want %q", cmdContent, wantContent)
	}

	want := map[string]any{
		"description": "Ship current changes",
		"template":    "{file:./command/ship.md}",
		"agent":       "build",
	}

	if !reflect.DeepEqual(fragment, want) {
		t.Errorf("fragment = %#v, want %#v", fragment, want)
	}
}

func TestMergeFilePreservesOutOfScope(t *testing.T) {
	a := opencode.NewAdapter()
	existing := []byte(`{
  "theme": "dark",
  "agent.old": {"description": "legacy"}
}`)
	fragments := map[string]any{
		"agent.build": map[string]any{"description": "Build and deploy agent"},
	}

	merged, err := a.MergeFile("opencode.json", existing, fragments)
	if err != nil {
		t.Fatal(err)
	}

	var root map[string]any
	if err := json.Unmarshal(merged, &root); err != nil {
		t.Fatal(err)
	}

	if root["theme"] != "dark" {
		t.Errorf("theme = %v, want dark", root["theme"])
	}
	if _, ok := root["agent.old"]; !ok {
		t.Error("agent.old should be preserved")
	}
	if _, ok := root["agent.build"]; !ok {
		t.Error("agent.build should be present")
	}
}

func TestMergeFileEmpty(t *testing.T) {
	a := opencode.NewAdapter()
	fragments := map[string]any{
		"agent.build": map[string]any{"description": "Build and deploy agent"},
	}

	merged, err := a.MergeFile("opencode.json", nil, fragments)
	if err != nil {
		t.Fatal(err)
	}

	var root map[string]any
	if err := json.Unmarshal(merged, &root); err != nil {
		t.Fatal(err)
	}

	if len(root) != 1 {
		t.Errorf("expected 1 key, got %d", len(root))
	}
}

func TestGoldenOpenCodeJSON(t *testing.T) {
	pf, pivotDir := testPivot(t)
	a := opencode.NewAdapterWithBaseDir("/tmp/opencode", pivotDir)

	fragments := map[string]any{}
	for _, agent := range pf.Agents {
		fragment, _, _, err := opencode.GenerateAgentFragment(agent, pivotDir)
		if err != nil {
			t.Fatal(err)
		}
		fragments["agent."+agent.ID] = fragment
	}
	for _, cmd := range pf.Commands {
		fragment, _, _, err := opencode.GenerateCommandFragment(cmd)
		if err != nil {
			t.Fatal(err)
		}
		fragments["command."+cmd.ID] = fragment
	}

	merged, err := a.MergeFile("opencode.json", nil, fragments)
	if err != nil {
		t.Fatal(err)
	}

	expected, err := os.ReadFile(filepath.Join("testdata", "expected_opencode.json"))
	if err != nil {
		t.Fatal(err)
	}

	normalize := func(b []byte) string {
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return string(out) + "\n"
	}

	if normalize(merged) != normalize(expected) {
		t.Errorf("golden JSON mismatch:\ngot:\n%s\nwant:\n%s", merged, expected)
	}
}

func TestGoldenPromptFile(t *testing.T) {
	pf, pivotDir := testPivot(t)
	build := pf.Agents[0]

	_, _, promptContent, err := opencode.GenerateAgentFragment(build, pivotDir)
	if err != nil {
		t.Fatal(err)
	}

	expected, err := os.ReadFile(filepath.Join("testdata", "expected_prompts_build.md"))
	if err != nil {
		t.Fatal(err)
	}

	if promptContent != string(expected) {
		t.Errorf("prompt mismatch:\ngot:\n%s\nwant:\n%s", promptContent, expected)
	}
}

func TestMergeFileNonConfigReturnsNil(t *testing.T) {
	a := opencode.NewAdapter()
	out, err := a.MergeFile("prompts/build.md", []byte("x"), map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("expected nil for non-config path, got %q", out)
	}
}

func TestAdapterGenerateAgentIntegration(t *testing.T) {
	pf, pivotDir := testPivot(t)
	a := opencode.NewAdapterWithBaseDir(t.TempDir(), pivotDir)

	files, err := a.GenerateAgent(pf.Agents[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	fragments := a.Fragments()
	if _, ok := fragments["agent.build"]; !ok {
		t.Error("missing agent.build fragment")
	}
}

func TestValidateAgent(t *testing.T) {
	a := opencode.NewAdapter()
	err := a.ValidateAgent(pivot.AgentDefinition{ID: "x", Mode: "invalid"})
	if err == nil {
		t.Error("expected validation error for invalid mode")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("unexpected error: %v", err)
	}
}
