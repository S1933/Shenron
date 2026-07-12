package integration_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/adapter/claude"
	"github.com/S1933/Shenron/internal/adapter/codex"
	"github.com/S1933/Shenron/internal/adapter/opencode"
	"github.com/S1933/Shenron/internal/cli"
	"github.com/S1933/Shenron/internal/pivot"
	"gopkg.in/yaml.v3"
)

const integrationFixtureDir = "../testdata/integration"

func TestEndToEnd_PushCodex(t *testing.T) {
	env := newIntegrationEnv(t)
	if err := cli.RunPush(env.pushOpts("codex")); err != nil {
		t.Fatalf("push: %v", err)
	}

	agent, err := os.ReadFile(filepath.Join(env.codexDir, "agents", "build.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name = 'build'", "You are a build agent responsible for CI/CD tasks.", "$test-driven-development"} {
		if !strings.Contains(string(agent), want) {
			t.Errorf("Codex agent missing %q:\n%s", want, agent)
		}
	}
	command, err := os.ReadFile(filepath.Join(env.codexDir, "prompts", "ship.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(command), "Delegate this task to the `build` custom agent.") {
		t.Errorf("Codex command did not delegate:\n%s", command)
	}

	out, _, err := cli.CaptureOutput(func() error { return cli.RunDiff(env.diffOpts("codex")) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No changes") {
		t.Fatalf("expected no changes after Codex push, got:\n%s", out)
	}
}

func TestEndToEnd_PushOpenCode(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("opencode")); err != nil {
		t.Fatalf("push: %v", err)
	}

	assertOpenCodePush(t, env)
	assertStateFile(t, env.pivotDir)

	out, _, err := cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(out, "No changes") {
		t.Fatalf("expected no changes after push, got:\n%s", out)
	}
}

func TestEndToEnd_PivotEditDiffPush(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("opencode")); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	const updatedPrompt = "You are an updated build agent for CI/CD tasks.\n"
	if err := modifyBuildAgent(env.pivotPath, env.pivotDir, updatedPrompt, "Updated build and deploy agent"); err != nil {
		t.Fatalf("modify pivot: %v", err)
	}

	out, _, err := cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("diff after pivot edit: %v", err)
	}
	for _, want := range []string{"modify", "opencode.json", filepath.Join("prompts", "build.md")} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected diff to show %q, got:\n%s", want, out)
		}
	}

	if err := cli.RunPush(env.pushOpts("opencode")); err != nil {
		t.Fatalf("push after pivot edit: %v", err)
	}

	promptPath := filepath.Join(env.opencodeDir, "prompts", "build.md")
	promptData, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read updated prompt: %v", err)
	}
	if string(promptData) != updatedPrompt {
		t.Fatalf("prompt not updated, got:\n%s", promptData)
	}

	out, _, err = cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("diff after push: %v", err)
	}
	if !strings.Contains(out, "No changes") {
		t.Fatalf("expected no changes after push, got:\n%s", out)
	}
}

func TestEndToEnd_ManualEditDetection(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("opencode")); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	targetPath := filepath.Join(env.opencodeDir, "prompts", "build.md")
	if err := os.WriteFile(targetPath, []byte("manual edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("diff after manual edit: %v", err)
	}
	if !strings.Contains(out, "manually modified") {
		t.Fatalf("expected manual modification warning, got:\n%s", out)
	}

	err = cli.RunPush(env.pushOpts("opencode"))
	if err == nil {
		t.Fatal("expected push to refuse without --force")
	}
	if !errors.Is(err, cli.ErrManualEdits) {
		t.Fatalf("expected ErrManualEdits, got: %v", err)
	}

	opts := env.pushOpts("opencode")
	opts.Force = true
	if err := cli.RunPush(opts); err != nil {
		t.Fatalf("force push: %v", err)
	}

	out, _, err = cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("diff after force push: %v", err)
	}
	if !strings.Contains(out, "No changes") {
		t.Fatalf("expected no changes after force push, got:\n%s", out)
	}
}

func TestEndToEnd_PushClaudeCode(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("claude-code")); err != nil {
		t.Fatalf("push: %v", err)
	}

	buildPath := filepath.Join(env.claudeDir, "agents", "build.md")
	buildContent, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatalf("read build agent: %v", err)
	}
	content := string(buildContent)
	for _, want := range []string{
		"name: build",
		"description: Build and deploy agent",
		"permissionMode: default",
		"tools: Read, Bash",
		"You are a build agent responsible for CI/CD tasks.",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("build agent missing %q:\n%s", want, content)
		}
	}

	reviewPath := filepath.Join(env.claudeDir, "agents", "review.md")
	reviewContent, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatalf("read review agent: %v", err)
	}
	if !strings.Contains(string(reviewContent), "permissionMode: plan") {
		t.Errorf("review agent should map edit deny to plan:\n%s", reviewContent)
	}

	shipPath := filepath.Join(env.claudeDir, "commands", "ship.md")
	shipContent, err := os.ReadFile(shipPath)
	if err != nil {
		t.Fatalf("read ship command: %v", err)
	}
	if !strings.Contains(string(shipContent), "<!-- agent: build -->") {
		t.Errorf("ship command missing agent reference:\n%s", shipContent)
	}

	lintPath := filepath.Join(env.claudeDir, "commands", "lint.md")
	if _, err := os.Stat(lintPath); err != nil {
		t.Fatalf("expected standalone lint command: %v", err)
	}
}

func TestEndToEnd_SkillsRoundTrip(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("")); err != nil {
		t.Fatalf("push: %v", err)
	}

	configData, err := os.ReadFile(filepath.Join(env.opencodeDir, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(configData, &root); err != nil {
		t.Fatal(err)
	}
	agents := root["agent"].(map[string]any)
	build := agents["build"].(map[string]any)
	if got := stringSliceValue(build["skills"]); len(got) != 1 || got[0] != "test-driven-development" {
		t.Errorf("OpenCode build skills = %#v", got)
	}
	legacy := agents["legacy-helper"].(map[string]any)
	if got := stringSliceValue(legacy["skills"]); len(got) != 1 || got[0] != "native-existing-skill" {
		t.Errorf("native OpenCode skills not preserved: %#v", got)
	}

	claudeData, err := os.ReadFile(filepath.Join(env.claudeDir, "agents", "build.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claudeData), "skills:\n  - test-driven-development") {
		t.Errorf("Claude build skills missing:\n%s", claudeData)
	}
}

func TestEndToEnd_TargetedPushNoClaudeOrphans(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("")); err != nil {
		t.Fatalf("full push: %v", err)
	}

	out, _, err := cli.CaptureOutput(func() error {
		return cli.RunPush(env.pushOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("targeted push: %v", err)
	}
	if strings.Contains(out, "warning: orphaned") && strings.Contains(out, env.claudeDir) {
		t.Fatalf("targeted push should not warn about Claude orphans, got:\n%s", out)
	}
}

func TestEndToEnd_RemoveAgentFromPivot(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("opencode")); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	data, err := os.ReadFile(env.pivotPath)
	if err != nil {
		t.Fatal(err)
	}
	pf, err := pivot.Parse(data, env.pivotDir)
	if err != nil {
		t.Fatal(err)
	}
	filtered := make([]pivot.AgentDefinition, 0, len(pf.Agents))
	for _, agent := range pf.Agents {
		if agent.ID != "scan" {
			filtered = append(filtered, agent)
		}
	}
	pf.Agents = filtered
	updated, err := yaml.Marshal(pf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.pivotPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cli.RunPush(env.pushOpts("opencode")); err != nil {
		t.Fatalf("push after removing scan agent: %v", err)
	}

	configData, err := os.ReadFile(filepath.Join(env.opencodeDir, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := json.Unmarshal(configData, &root); err != nil {
		t.Fatal(err)
	}
	agents, ok := root["agent"].(map[string]any)
	if !ok {
		t.Fatalf("agent = %#v, want nested object", root["agent"])
	}
	if _, ok := agents["scan"]; !ok {
		t.Error("agent scan should be preserved after pivot deletion (upsert-only policy)")
	}
}

func TestEndToEnd_PushBothTargets(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("")); err != nil {
		t.Fatalf("push both targets: %v", err)
	}

	if _, err := os.Stat(filepath.Join(env.opencodeDir, "opencode.json")); err != nil {
		t.Fatalf("opencode config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.claudeDir, "agents", "build.md")); err != nil {
		t.Fatalf("claude build agent missing: %v", err)
	}

	out, _, err := cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts(""))
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(out, "[opencode] No changes") {
		t.Fatalf("expected opencode no changes, got:\n%s", out)
	}
	if !strings.Contains(out, "[claude-code] No changes") {
		t.Fatalf("expected claude-code no changes, got:\n%s", out)
	}
}

func TestEndToEnd_PermissionMapping(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("")); err != nil {
		t.Fatalf("push: %v", err)
	}

	configData, err := os.ReadFile(filepath.Join(env.opencodeDir, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := json.Unmarshal(configData, &root); err != nil {
		t.Fatal(err)
	}

	agents, ok := root["agent"].(map[string]any)
	if !ok {
		t.Fatalf("agent = %#v, want nested object", root["agent"])
	}
	build, ok := agents["build"].(map[string]any)
	if !ok {
		t.Fatalf("missing nested agent.build: %#v", agents)
	}
	perms, ok := build["permission"].(map[string]any)
	if !ok {
		t.Fatalf("missing permission block: %#v", build)
	}
	if perms["edit"] != "ask" {
		t.Errorf("edit = %v, want ask", perms["edit"])
	}
	bash, ok := perms["bash"].(map[string]any)
	if !ok {
		t.Fatalf("bash permission = %#v", perms["bash"])
	}
	if bash["go *"] != "allow" || bash["npm *"] != "allow" {
		t.Errorf("unexpected bash patterns: %#v", bash)
	}
	for _, key := range []string{"glob", "grep", "list"} {
		if perms[key] != "allow" {
			t.Errorf("%s = %v, want allow", key, perms[key])
		}
	}
	if perms["lsp"] != "deny" {
		t.Errorf("lsp = %v, want deny", perms["lsp"])
	}

	buildAgent, err := os.ReadFile(filepath.Join(env.claudeDir, "agents", "build.md"))
	if err != nil {
		t.Fatal(err)
	}
	claudeContent := string(buildAgent)
	if !strings.Contains(claudeContent, "permissionMode: default") {
		t.Errorf("expected permissionMode default for edit ask:\n%s", claudeContent)
	}
	if !strings.Contains(claudeContent, "tools: ") || !strings.Contains(claudeContent, "Bash") {
		t.Errorf("expected Bash tool for bash patterns:\n%s", claudeContent)
	}
	if !strings.Contains(claudeContent, "Read") {
		t.Errorf("expected Read tool for read allow:\n%s", claudeContent)
	}
}

type integrationEnv struct {
	pivotDir    string
	pivotPath   string
	opencodeDir string
	claudeDir   string
	codexDir    string
}

func newIntegrationEnv(t *testing.T) integrationEnv {
	t.Helper()

	tmp := t.TempDir()
	pivotPath := filepath.Join(tmp, "shenron.yaml")
	if err := copyFile(filepath.Join(integrationFixtureDir, "shenron.yaml"), pivotPath); err != nil {
		t.Fatal(err)
	}
	if err := copyDir(filepath.Join(integrationFixtureDir, "prompts"), filepath.Join(tmp, "prompts")); err != nil {
		t.Fatal(err)
	}

	opencodeDir := filepath.Join(tmp, "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join(integrationFixtureDir, "existing_opencode.json"), filepath.Join(opencodeDir, "opencode.json")); err != nil {
		t.Fatal(err)
	}

	claudeDir := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codexDir := filepath.Join(tmp, "codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	return integrationEnv{
		pivotDir:    tmp,
		pivotPath:   pivotPath,
		opencodeDir: opencodeDir,
		claudeDir:   claudeDir,
		codexDir:    codexDir,
	}
}

func (env integrationEnv) adapters() map[string]adapter.Adapter {
	return map[string]adapter.Adapter{
		"opencode":    opencode.NewAdapterWithBaseDir(env.opencodeDir, env.pivotDir),
		"claude-code": claude.NewAdapterWithBaseDir(env.claudeDir, env.pivotDir),
		"codex":       codex.NewAdapterWithBaseDir(env.codexDir, env.pivotDir),
	}
}

func (env integrationEnv) pushOpts(target string) cli.PushOptions {
	adapters := env.adapters()
	if target != "" {
		adapters = map[string]adapter.Adapter{target: env.adapters()[target]}
	}
	return cli.PushOptions{
		ConfigPath: env.pivotPath,
		Target:     target,
		Adapters:   adapters,
	}
}

func (env integrationEnv) diffOpts(target string) cli.DiffOptions {
	adapters := env.adapters()
	if target != "" {
		adapters = map[string]adapter.Adapter{target: env.adapters()[target]}
	}
	return cli.DiffOptions{
		ConfigPath: env.pivotPath,
		Target:     target,
		Adapters:   adapters,
	}
}

func assertOpenCodePush(t *testing.T, env integrationEnv) {
	t.Helper()

	configPath := filepath.Join(env.opencodeDir, "opencode.json")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	root := map[string]any{}
	if err := json.Unmarshal(configData, &root); err != nil {
		t.Fatal(err)
	}

	agents, ok := root["agent"].(map[string]any)
	if !ok {
		t.Fatalf("agent = %#v, want nested object", root["agent"])
	}
	for _, id := range []string{"build", "review", "scan"} {
		if _, ok := agents[id]; !ok {
			t.Errorf("opencode.json missing agent %q", id)
		}
	}
	commands, ok := root["command"].(map[string]any)
	if !ok {
		t.Fatalf("command = %#v, want nested object", root["command"])
	}
	for _, id := range []string{"ship", "lint"} {
		if _, ok := commands[id]; !ok {
			t.Errorf("opencode.json missing command %q", id)
		}
	}
	if _, ok := root["agent.build"]; ok {
		t.Error("must not write flat dotted agent.build key")
	}
	if root["theme"] != "dark" {
		t.Errorf("theme = %v, want dark (preserved from existing config)", root["theme"])
	}
	provider, ok := root["provider"].(map[string]any)
	if !ok {
		t.Fatalf("provider = %#v, want map preserved from existing config", root["provider"])
	}
	if provider["default"] != "anthropic" {
		t.Errorf("provider.default = %v, want anthropic", provider["default"])
	}
	if _, ok := agents["legacy-helper"]; !ok {
		t.Error("native-only agent legacy-helper should be preserved under upsert-only policy")
	}

	for _, prompt := range []string{"build.md", "review.md", "scan.md"} {
		path := filepath.Join(env.opencodeDir, "prompts", prompt)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing prompt file %s: %v", path, err)
		}
	}
}

func assertStateFile(t *testing.T, pivotDir string) {
	t.Helper()
	statePath := filepath.Join(pivotDir, ".shenron-state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func stringSliceValue(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func modifyBuildAgent(pivotPath, pivotDir, newPrompt, newDescription string) error {
	data, err := os.ReadFile(pivotPath)
	if err != nil {
		return err
	}
	pf, err := pivot.Parse(data, pivotDir)
	if err != nil {
		return err
	}
	found := false
	for i, agent := range pf.Agents {
		if agent.ID == "build" {
			pf.Agents[i].SystemPrompt = newPrompt
			pf.Agents[i].Description = newDescription
			found = true
			break
		}
	}
	if !found {
		return errors.New("agent not found: build")
	}
	updated, err := yaml.Marshal(pf)
	if err != nil {
		return err
	}
	return os.WriteFile(pivotPath, updated, 0o644)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
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
		return copyFile(path, target)
	})
}
