package codex_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/adapter/codex"
	"github.com/S1933/Shenron/internal/pivot"
	"github.com/pelletier/go-toml/v2"
)

// fileContent returns the body of the generated file at path, or "" if absent.
func fileContent(files []adapter.GeneratedFile, path string) string {
	for _, f := range files {
		if f.Path == path {
			return string(f.Content)
		}
	}
	return ""
}

func TestGenerateAgentUsesCodexNativeFields(t *testing.T) {
	baseDir := t.TempDir()
	a := codex.NewAdapterWithBaseDir(baseDir, "")
	agent := pivot.AgentDefinition{
		ID:           "build",
		Description:  "Build approved changes.",
		Mode:         "primary",
		Model:        "gpt-5.4",
		SystemPrompt: "Implement the approved plan.\v",
		Permissions:  &pivot.Permissions{Edit: "ask", WebSearch: "allow"},
		Skills:       []string{"test-driven-development"},
		Extensions: map[string]any{
			"codex": map[string]any{
				"modelReasoningEffort": "high",
				"nicknameCandidates":   []any{"Builder"},
			},
		},
	}

	result, err := a.Generate(&pivot.PivotFile{Agents: []pivot.AgentDefinition{agent}})
	if err != nil {
		t.Fatal(err)
	}
	content := fileContent(result.Files, filepath.Join(baseDir, "agents", "build.toml"))
	for _, want := range []string{
		"name = 'build'",
		"description = 'Build approved changes.'",
		"model = 'gpt-5.4'",
		"model_reasoning_effort = 'high'",
		"sandbox_mode = 'read-only'",
		"approval_policy = 'on-request'",
		"web_search = 'live'",
		"nickname_candidates = ['Builder']",
		"Implement the approved plan.",
		"When applicable, use these skills: $test-driven-development.",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated agent missing %q:\n%s", want, content)
		}
	}
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("generated agent must be valid TOML: %v", err)
	}
}

func TestGenerateCommandDelegatesToCodexAgentName(t *testing.T) {
	baseDir := t.TempDir()
	a := codex.NewAdapterWithBaseDir(baseDir, "")
	result, err := a.Generate(&pivot.PivotFile{
		Agents: []pivot.AgentDefinition{{
			ID: "code-review", Description: "Review code.", Mode: "subagent",
			Extensions: map[string]any{"codex": map[string]any{"name": "code_reviewer"}},
		}},
		Commands: []pivot.CommandDefinition{{
			ID: "review", Description: "Review the current change.", Agent: "code-review", Template: "Find defects.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	content := fileContent(result.Files, filepath.Join(baseDir, "prompts", "review.md"))
	for _, want := range []string{
		`description: "Review the current change."`,
		"Delegate this task to the `code_reviewer` custom agent.",
		"Find defects.",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated command missing %q:\n%s", want, content)
		}
	}
}
