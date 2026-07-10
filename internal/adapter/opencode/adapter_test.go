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

func TestMergeFileUpsertsNestedAndPreserves(t *testing.T) {
	a := opencode.NewAdapter()
	existing := []byte(`{
  "theme": "dark",
  "agent": {
    "orchestrator": {"description": "native only"},
    "build": {"description": "old build"}
  }
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
		t.Errorf("theme = %v, want dark (unrelated keys preserved)", root["theme"])
	}
	if _, ok := root["agent.build"]; ok {
		t.Error("must not write a flat dotted agent.build key")
	}

	agents, ok := root["agent"].(map[string]any)
	if !ok {
		t.Fatalf("agent = %T, want nested object", root["agent"])
	}
	if _, ok := agents["orchestrator"]; !ok {
		t.Error("native-only agent orchestrator should be preserved (upsert-only)")
	}
	build, ok := agents["build"].(map[string]any)
	if !ok {
		t.Fatalf("agent.build = %T, want object", agents["build"])
	}
	if build["description"] != "Build and deploy agent" {
		t.Errorf("build.description = %v, want updated value from pivot", build["description"])
	}
}

func TestMergeFilePreservesNativeAgentsNotInPivot(t *testing.T) {
	a := opencode.NewAdapter()
	existing := []byte(`{
  "agent": {
    "build": {"description": "Build"},
    "legacy": {"description": "not in pivot"}
  }
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
	agents := root["agent"].(map[string]any)
	if _, ok := agents["legacy"]; !ok {
		t.Error("agent.legacy should be preserved under upsert-only policy")
	}
	if _, ok := agents["build"]; !ok {
		t.Error("agent.build should remain")
	}
}

func TestMergeFilePreservesKeyOrder(t *testing.T) {
	a := opencode.NewAdapter()
	existing := []byte(`{
  "model": "glm",
  "small_model": "flash",
  "agent": {
    "orchestrator": {"description": "native"}
  },
  "permission": {"edit": "ask"}
}`)
	fragments := map[string]any{
		"agent.build": map[string]any{"description": "Build"},
	}

	merged, err := a.MergeFile("opencode.json", existing, fragments)
	if err != nil {
		t.Fatal(err)
	}

	topOrder := topLevelKeyOrder(t, merged)
	want := []string{"model", "small_model", "agent", "permission"}
	if !reflect.DeepEqual(topOrder, want) {
		t.Errorf("top-level key order = %v, want %v", topOrder, want)
	}

	agentOrder := nestedKeyOrder(t, merged, "agent")
	if !reflect.DeepEqual(agentOrder, []string{"orchestrator", "build"}) {
		t.Errorf("agent key order = %v, want [orchestrator build]", agentOrder)
	}
}

// topLevelKeyOrder returns root object keys in document order.
func topLevelKeyOrder(t *testing.T, data []byte) []string {
	t.Helper()
	return decodeKeyOrder(t, data)
}

func nestedKeyOrder(t *testing.T, data []byte, key string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(data)))
	// walk to the nested object
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	_ = dec
	return decodeKeyOrder(t, root[key])
}

func decodeKeyOrder(t *testing.T, data []byte) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if _, err := dec.Token(); err != nil { // opening '{'
		t.Fatal(err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
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

func TestEmptyPermissionOverrideFallsBackToRead(t *testing.T) {
	agent := pivot.AgentDefinition{
		ID: "test",
		Permissions: &pivot.Permissions{
			Read: "allow",
			Edit: "deny",
		},
		Extensions: map[string]any{
			"opencode": map[string]any{
				"permission": map[string]any{},
			},
		},
	}

	fragment, _, _, err := opencode.GenerateAgentFragment(agent, "")
	if err != nil {
		t.Fatal(err)
	}

	perm, ok := fragment["permission"].(map[string]any)
	if !ok {
		t.Fatalf("permission = %T, want map[string]any", fragment["permission"])
	}

	for _, key := range []string{"glob", "grep", "list", "lsp"} {
		if perm[key] != "allow" {
			t.Errorf("permission[%q] = %v, want allow", key, perm[key])
		}
	}
	if perm["edit"] != "deny" {
		t.Errorf("permission[edit] = %v, want deny", perm["edit"])
	}
}

func TestModelResolution(t *testing.T) {
	tests := []struct {
		name      string
		agent     pivot.AgentDefinition
		wantModel string
	}{
		{
			name: "override beats scalar fallback",
			agent: pivot.AgentDefinition{
				ID:    "test",
				Model: "opus",
				Extensions: map[string]any{
					"claude":   map[string]any{"model": "claude-model"},
					"opencode": map[string]any{"model": "opencode-model"},
				},
			},
			wantModel: "opencode-model",
		},
		{
			name: "scalar fallback when no override",
			agent: pivot.AgentDefinition{
				ID:    "test",
				Model: "opus",
			},
			wantModel: "opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fragment, _, _, err := opencode.GenerateAgentFragment(tt.agent, "")
			if err != nil {
				t.Fatal(err)
			}
			if fragment["model"] != tt.wantModel {
				t.Errorf("model = %v, want %v", fragment["model"], tt.wantModel)
			}
		})
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
