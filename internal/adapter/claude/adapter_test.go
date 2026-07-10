package claude_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnuel/agentsync/internal/adapter/claude"
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

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGenerateAgentBuild(t *testing.T) {
	pf, pivotDir := testPivot(t)
	build := pf.Agents[0]

	files, err := claude.GenerateAgent(build, pivotDir)
	if err != nil {
		t.Fatal(err)
	}

	wantPath := filepath.Join(claudeAgentDir(t), "build.md")
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	got, ok := files[wantPath]
	if !ok {
		t.Fatalf("missing %q, got keys: %v", wantPath, fileKeys(files))
	}

	want := readGolden(t, "expected_agents_build.md")
	if got != want {
		t.Errorf("agent content mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerateAgentReview(t *testing.T) {
	pf, pivotDir := testPivot(t)
	review := pf.Agents[1]

	files, err := claude.GenerateAgent(review, pivotDir)
	if err != nil {
		t.Fatal(err)
	}

	content := files[filepath.Join(claudeAgentDir(t), "review.md")]
	if !strings.Contains(content, "permissionMode: plan") {
		t.Errorf("expected permissionMode plan, got:\n%s", content)
	}
	if !strings.Contains(content, "tools: Read, Task") {
		t.Errorf("expected tools CSV with Read and Task, got:\n%s", content)
	}
}

func TestGenerateCommand(t *testing.T) {
	pf, _ := testPivot(t)
	ship := pf.Commands[0]

	files, err := claude.GenerateCommand(ship)
	if err != nil {
		t.Fatal(err)
	}

	wantPath := filepath.Join(claudeCommandDir(t), "ship.md")
	got, ok := files[wantPath]
	if !ok {
		t.Fatalf("missing %q, got keys: %v", wantPath, fileKeys(files))
	}

	want := readGolden(t, "expected_commands_ship.md")
	if got != want {
		t.Errorf("command content mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPermissionMapping(t *testing.T) {
	tests := []struct {
		name           string
		perms          *pivot.Permissions
		wantTools      []string
		wantPermission string
	}{
		{
			name: "read allow",
			perms: &pivot.Permissions{
				Read: "allow",
			},
			wantTools:      []string{"Read"},
			wantPermission: "",
		},
		{
			name: "edit allow",
			perms: &pivot.Permissions{
				Edit: "allow",
			},
			wantTools:      nil,
			wantPermission: "acceptEdits",
		},
		{
			name: "edit ask",
			perms: &pivot.Permissions{
				Edit: "ask",
			},
			wantTools:      nil,
			wantPermission: "default",
		},
		{
			name: "edit deny",
			perms: &pivot.Permissions{
				Edit: "deny",
			},
			wantTools:      nil,
			wantPermission: "plan",
		},
		{
			name: "bash string allow",
			perms: &pivot.Permissions{
				Bash: "allow",
			},
			wantTools:      []string{"Bash"},
			wantPermission: "",
		},
		{
			name: "bash patterns",
			perms: &pivot.Permissions{
				Bash: map[string]string{"go *": "allow", "rm *": "deny"},
			},
			wantTools:      []string{"Bash"},
			wantPermission: "",
		},
		{
			name: "webfetch allow",
			perms: &pivot.Permissions{
				WebFetch: "allow",
			},
			wantTools:      []string{"WebFetch"},
			wantPermission: "",
		},
		{
			name: "websearch allow",
			perms: &pivot.Permissions{
				WebSearch: "allow",
			},
			wantTools:      []string{"WebSearch"},
			wantPermission: "",
		},
		{
			name: "tasks allow",
			perms: &pivot.Permissions{
				Tasks: map[string]string{"build": "allow"},
			},
			wantTools:      []string{"Task"},
			wantPermission: "",
		},
		{
			name: "ask and deny do not add tools",
			perms: &pivot.Permissions{
				Read:      "ask",
				WebFetch:  "deny",
				WebSearch: "ask",
				Bash:      "deny",
			},
			wantTools:      nil,
			wantPermission: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := pivot.AgentDefinition{
				ID:          "test",
				Description: "test agent",
				Mode:        "primary",
				Permissions: tt.perms,
			}
			files, err := claude.GenerateAgent(agent, "")
			if err != nil {
				t.Fatal(err)
			}
			content := files[filepath.Join(claudeAgentDir(t), "test.md")]

			if len(tt.wantTools) > 0 {
				want := "tools: " + strings.Join(tt.wantTools, ", ")
				if !strings.Contains(content, want) {
					t.Errorf("expected %q in:\n%s", want, content)
				}
			}
			if tt.wantTools == nil && strings.Contains(content, "tools:") {
				t.Errorf("expected no tools section, got:\n%s", content)
			}

			if tt.wantPermission == "" {
				if strings.Contains(content, "permissionMode:") {
					t.Errorf("expected no permissionMode, got:\n%s", content)
				}
			} else if !strings.Contains(content, "permissionMode: "+tt.wantPermission) {
				t.Errorf("expected permissionMode %q, got:\n%s", tt.wantPermission, content)
			}
		})
	}
}

func TestNoPermissions(t *testing.T) {
	agent := pivot.AgentDefinition{
		ID:           "bare",
		Description:  "No permissions",
		Mode:         "primary",
		SystemPrompt: "Hello.",
	}

	files, err := claude.GenerateAgent(agent, "")
	if err != nil {
		t.Fatal(err)
	}

	content := files[filepath.Join(claudeAgentDir(t), "bare.md")]
	if strings.Contains(content, "permissionMode:") {
		t.Errorf("expected no permissionMode when unconstrained, got:\n%s", content)
	}
	if strings.Contains(content, "tools:") {
		t.Errorf("expected no tools, got:\n%s", content)
	}
}

func TestClaudeExtensionOverrides(t *testing.T) {
	agent := pivot.AgentDefinition{
		ID:           "ask",
		Description:  "Ask agent",
		Mode:         "primary",
		SystemPrompt: "Hello.",
		Extensions: map[string]any{
			"claude": map[string]any{
				"tools":          []any{"Read", "Glob", "Grep", "Bash"},
				"permissionMode": "plan",
			},
		},
	}

	files, err := claude.GenerateAgent(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	content := files[filepath.Join(claudeAgentDir(t), "ask.md")]

	if !strings.Contains(content, "tools: Read, Glob, Grep, Bash") {
		t.Errorf("expected explicit tools CSV from extensions.claude.tools, got:\n%s", content)
	}
	if !strings.Contains(content, "permissionMode: plan") {
		t.Errorf("expected permissionMode plan from extensions.claude, got:\n%s", content)
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
				ID:          "test",
				Description: "test agent",
				Mode:        "primary",
				Model:       "opus",
				Extensions: map[string]any{
					"claude":   map[string]any{"model": "claude-model"},
					"opencode": map[string]any{"model": "opencode-model"},
				},
			},
			wantModel: "claude-model",
		},
		{
			name: "scalar fallback when no override",
			agent: pivot.AgentDefinition{
				ID:          "test",
				Description: "test agent",
				Mode:        "primary",
				Model:       "opus",
			},
			wantModel: "opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := claude.GenerateAgent(tt.agent, "")
			if err != nil {
				t.Fatal(err)
			}
			content := files[filepath.Join(claudeAgentDir(t), "test.md")]
			if !strings.Contains(content, "model: "+tt.wantModel) {
				t.Errorf("expected model %q, got:\n%s", tt.wantModel, content)
			}
		})
	}
}

func TestPromptFile(t *testing.T) {
	pivotDir := filepath.Join("testdata")
	agent := pivot.AgentDefinition{
		ID:          "review",
		Description: "Review from file",
		Mode:        "primary",
		PromptFile:  "prompts/review.md",
		Permissions: &pivot.Permissions{Read: "allow"},
	}

	files, err := claude.GenerateAgent(agent, pivotDir)
	if err != nil {
		t.Fatal(err)
	}

	content := files[filepath.Join(claudeAgentDir(t), "review.md")]
	wantBody, err := os.ReadFile(filepath.Join(pivotDir, "prompts/review.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(content, string(wantBody)) {
		t.Errorf("expected prompt file content at end, got:\n%s", content)
	}
}

func TestAdapterMergeFileReturnsNil(t *testing.T) {
	a := claude.NewAdapter()
	out, err := a.MergeFile("agents/build.md", []byte("x"), map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("expected nil, got %q", out)
	}
}

func TestValidateAgent(t *testing.T) {
	a := claude.NewAdapter()
	err := a.ValidateAgent(pivot.AgentDefinition{ID: "x", Mode: "invalid"})
	if err == nil {
		t.Error("expected validation error for invalid mode")
	}
}

func claudeAgentDir(t *testing.T) string {
	t.Helper()
	a := claude.NewAdapter()
	return a.TargetPaths()[0]
}

func claudeCommandDir(t *testing.T) string {
	t.Helper()
	a := claude.NewAdapter()
	return a.TargetPaths()[1]
}

func fileKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
