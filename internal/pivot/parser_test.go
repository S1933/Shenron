package pivot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testdataDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata")
}

func TestParseValidPivotFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testdataDir(t), "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	pf, err := Parse(data, testdataDir(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pf.Version != "1" {
		t.Errorf("version = %q, want %q", pf.Version, "1")
	}
	if len(pf.Agents) != 2 {
		t.Fatalf("agents count = %d, want 2", len(pf.Agents))
	}
	if pf.Agents[0].ID != "build" {
		t.Errorf("agents[0].id = %q, want build", pf.Agents[0].ID)
	}
	if pf.Agents[0].Mode != "primary" {
		t.Errorf("agents[0].mode = %q, want primary", pf.Agents[0].Mode)
	}
	if pf.Agents[0].Temperature == nil || *pf.Agents[0].Temperature != 0.7 {
		t.Errorf("agents[0].temperature = %v, want 0.7", pf.Agents[0].Temperature)
	}
	if pf.Agents[1].PromptFile != "prompts/review.md" {
		t.Errorf("agents[1].promptFile = %q, want prompts/review.md", pf.Agents[1].PromptFile)
	}
	if len(pf.Commands) != 2 {
		t.Fatalf("commands count = %d, want 2", len(pf.Commands))
	}
	if pf.Commands[0].Agent != "build" {
		t.Errorf("commands[0].agent = %q, want build", pf.Commands[0].Agent)
	}
	if len(pf.Skills) != 1 || pf.Skills[0].Name != "test-driven-development" {
		t.Errorf("skills = %+v, want test-driven-development", pf.Skills)
	}
}

func TestParseMissingVersion(t *testing.T) {
	_, err := Parse([]byte(`agents: []`), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "version is required") {
		t.Fatalf("expected version error, got: %v", err)
	}
}

func TestParseAgentMissingID(t *testing.T) {
	yaml := `version: "1"
agents:
  - description: "test"
    mode: primary`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].id is required") {
		t.Fatalf("expected id error, got: %v", err)
	}
}

func TestParseAgentInvalidID(t *testing.T) {
	cases := []struct {
		name string
		id   string
	}{
		{"uppercase", "Build"},
		{"spaces", "build agent"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yaml := `version: "1"
agents:
  - id: ` + tc.id + `
    description: "test"
    mode: primary`
			_, err := Parse([]byte(yaml), testdataDir(t))
			if err == nil || !strings.Contains(err.Error(), "must match ^[a-z][a-z0-9-]*$") {
				t.Fatalf("expected id regex error, got: %v", err)
			}
		})
	}
}

func TestParseAgentMissingDescription(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    mode: primary`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].description is required") {
		t.Fatalf("expected description error, got: %v", err)
	}
}

func TestParseAgentMissingMode(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].mode is required") {
		t.Fatalf("expected mode error, got: %v", err)
	}
}

func TestParseAgentInvalidMode(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: invalid`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].mode must be primary or subagent") {
		t.Fatalf("expected mode value error, got: %v", err)
	}
}

func TestParseAgentBothSystemPromptAndPromptFile(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    systemPrompt: "hello"
    promptFile: prompts/review.md`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "systemPrompt and promptFile are mutually exclusive") {
		t.Fatalf("expected XOR error, got: %v", err)
	}
}

func TestParseAgentPromptFileNotFound(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    promptFile: prompts/missing.md`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "promptFile: file not found") {
		t.Fatalf("expected promptFile error, got: %v", err)
	}
}

func TestParseAgentTemperatureOutOfRange(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    temperature: 3.0`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "temperature must be between 0.0 and 2.0") {
		t.Fatalf("expected temperature error, got: %v", err)
	}
}

func TestParseCommandMissingRequiredFields(t *testing.T) {
	yaml := `version: "1"
agents: []
commands:
  - id: ship`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil {
		t.Fatal("expected error for missing command fields")
	}
	errMsg := err.Error()
	for _, want := range []string{"commands[0].description is required", "commands[0].template is required"} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("expected error containing %q, got: %v", want, err)
		}
	}
}

func TestParseCommandAgentReferencesNonExistent(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
commands:
  - id: ship
    description: "ship it"
    template: "go"
    agent: missing`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "references unknown agent") {
		t.Fatalf("expected agent reference error, got: %v", err)
	}
}

func TestParsePermissionInvalidEnum(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    permissions:
      read: maybe`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "must be allow, deny, or ask") {
		t.Fatalf("expected permission enum error, got: %v", err)
	}
}
