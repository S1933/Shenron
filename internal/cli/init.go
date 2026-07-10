package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var fileRefPattern = regexp.MustCompile(`^\{file:(.+)\}$`)

// InitOptions configures bootstrap source paths for testing.
type InitOptions struct {
	WorkDir     string
	OpenCodeDir string
	ClaudeDir   string
}

// NewInitCmd creates the init subcommand.
func NewInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a skeleton agentsync.yaml from existing native configs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunInit(InitOptions{})
		},
	}
}

// RunInit bootstraps agentsync.yaml from the first available native config source.
func RunInit(opts InitOptions) error {
	workDir := opts.WorkDir
	if workDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		workDir = cwd
	}

	outputPath := filepath.Join(workDir, "agentsync.yaml")
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("agentsync.yaml already exists in %s (edit the existing file instead)", workDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check existing pivot file: %w", err)
	}

	pf, source, err := bootstrapPivot(opts)
	if err != nil {
		return err
	}
	if pf == nil {
		return fmt.Errorf("no native config found (checked OpenCode and Claude Code)")
	}

	data, err := marshalPivot(pf)
	if err != nil {
		return fmt.Errorf("marshal pivot file: %w", err)
	}

	if err := fsutil.WriteFileAtomic(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write agentsync.yaml: %w", err)
	}

	fmt.Printf("Created %s from %s (%d agents, %d commands)\n", outputPath, source, len(pf.Agents), len(pf.Commands))
	return nil
}

func bootstrapPivot(opts InitOptions) (*pivot.PivotFile, string, error) {
	opencodeDir := opts.OpenCodeDir
	if opencodeDir == "" {
		opencodeDir = fsutil.OpenCodePath()
	}
	if pf, err := bootstrapFromOpenCode(opencodeDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: skipping OpenCode bootstrap: %v\n", err)
	} else if pf != nil {
		return pf, "opencode", nil
	}

	claudeDir := opts.ClaudeDir
	if claudeDir == "" {
		claudeDir = fsutil.ClaudePath()
	}
	if pf, err := bootstrapFromClaude(claudeDir); err != nil {
		return nil, "", err
	} else if pf != nil {
		return pf, "claude-code", nil
	}

	return nil, "", nil
}

func bootstrapFromOpenCode(baseDir string) (*pivot.PivotFile, error) {
	configPath := filepath.Join(baseDir, "opencode.json")
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read opencode config: %w", err)
	}

	root := map[string]any{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse opencode.json: %w", err)
	}

	pf := &pivot.PivotFile{Version: "1"}

	if agents, ok := root["agent"].(map[string]any); ok {
		for _, id := range sortedKeys(agents) {
			fragment, ok := agents[id].(map[string]any)
			if !ok {
				continue
			}
			agent, err := openCodeAgentToPivot(id, fragment, baseDir)
			if err != nil {
				return nil, err
			}
			pf.Agents = append(pf.Agents, agent)
		}
	}

	if commands, ok := root["command"].(map[string]any); ok {
		for _, id := range sortedKeys(commands) {
			fragment, ok := commands[id].(map[string]any)
			if !ok {
				continue
			}
			cmd, err := openCodeCommandToPivot(id, fragment, baseDir)
			if err != nil {
				return nil, err
			}
			pf.Commands = append(pf.Commands, cmd)
		}
	}

	if len(pf.Agents) == 0 && len(pf.Commands) == 0 {
		return nil, nil
	}
	return pf, nil
}

// ensureOpenCodeExtension returns the agent's extensions.opencode submap, creating
// it (and the parent extensions map) if absent, so callers can merge keys into it
// without clobbering values set by earlier callers.
func ensureOpenCodeExtension(agent *pivot.AgentDefinition) map[string]any {
	if agent.Extensions == nil {
		agent.Extensions = map[string]any{}
	}
	opencode, ok := agent.Extensions["opencode"].(map[string]any)
	if !ok {
		opencode = map[string]any{}
		agent.Extensions["opencode"] = opencode
	}
	return opencode
}

func openCodeAgentToPivot(id string, fragment map[string]any, baseDir string) (pivot.AgentDefinition, error) {
	agent := pivot.AgentDefinition{
		ID:          id,
		Description: stringValue(fragment["description"]),
		Mode:        "primary",
	}

	if mode := stringValue(fragment["mode"]); mode != "" {
		agent.Mode = mode
	}
	if temp, ok := fragment["temperature"].(float64); ok {
		agent.Temperature = &temp
	}

	if prompt := stringValue(fragment["prompt"]); prompt != "" {
		content, err := readOpenCodeFileRef(prompt, baseDir)
		if err != nil {
			return agent, err
		}
		if strings.TrimSpace(content) != "" {
			agent.SystemPrompt = content
		}
	}

	if perms, ok := fragment["permission"].(map[string]any); ok {
		agent.Permissions = openCodePermissionsToPivot(perms)
	}

	if model := stringValue(fragment["model"]); model != "" {
		ensureOpenCodeExtension(&agent)["model"] = model
	}

	if steps, ok := fragment["steps"]; ok {
		ensureOpenCodeExtension(&agent)["steps"] = steps
	}

	if perms, ok := fragment["permission"].(map[string]any); ok {
		if override := openCodePermissionOverride(perms); len(override) > 0 {
			ensureOpenCodeExtension(&agent)["permission"] = override
		}
	}

	return agent, nil
}

func openCodeCommandToPivot(id string, fragment map[string]any, baseDir string) (pivot.CommandDefinition, error) {
	cmd := pivot.CommandDefinition{
		ID:          id,
		Description: stringValue(fragment["description"]),
		Agent:       stringValue(fragment["agent"]),
		Model:       stringValue(fragment["model"]),
	}

	if template := stringValue(fragment["template"]); template != "" {
		content, err := readOpenCodeFileRef(template, baseDir)
		if err != nil {
			return cmd, err
		}
		cmd.Template = content
	}

	return cmd, nil
}

func readOpenCodeFileRef(ref, baseDir string) (string, error) {
	m := fileRefPattern.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return ref, nil
	}
	path := strings.TrimPrefix(m[1], "./")
	data, err := os.ReadFile(filepath.Join(baseDir, path))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read referenced file %q: %w", path, err)
	}
	return string(data), nil
}

func openCodePermissionsToPivot(perms map[string]any) *pivot.Permissions {
	out := &pivot.Permissions{}
	readSubs := []string{"glob", "grep", "list", "lsp"}
	readVals := map[string]string{}

	for _, key := range readSubs {
		if val := stringValue(perms[key]); val != "" {
			readVals[key] = val
		}
	}
	if len(readVals) > 0 {
		if allSame(readVals) {
			out.Read = firstValue(readVals)
		}
	}

	if val := stringValue(perms["edit"]); val != "" {
		out.Edit = val
	}
	if val := stringValue(perms["webfetch"]); val != "" {
		out.WebFetch = val
	}
	if val := stringValue(perms["websearch"]); val != "" {
		out.WebSearch = val
	}
	if bash, ok := perms["bash"]; ok {
		out.Bash = bash
	}
	if tasks, ok := perms["task"].(map[string]any); ok {
		out.Tasks = stringMap(tasks)
	}

	if out.Read == "" && out.Edit == "" && out.Bash == nil && out.WebFetch == "" && out.WebSearch == "" && len(out.Tasks) == 0 {
		return nil
	}
	return out
}

func openCodePermissionOverride(perms map[string]any) map[string]string {
	readSubs := []string{"glob", "grep", "list", "lsp"}
	out := map[string]string{}
	for _, key := range readSubs {
		if val := stringValue(perms[key]); val != "" {
			out[key] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func bootstrapFromClaude(baseDir string) (*pivot.PivotFile, error) {
	agentsDir := filepath.Join(baseDir, "agents")
	commandsDir := filepath.Join(baseDir, "commands")

	agentFiles, _ := filepath.Glob(filepath.Join(agentsDir, "*.md"))
	commandFiles, _ := filepath.Glob(filepath.Join(commandsDir, "*.md"))
	if len(agentFiles) == 0 && len(commandFiles) == 0 {
		return nil, nil
	}

	pf := &pivot.PivotFile{Version: "1"}
	sort.Strings(agentFiles)
	sort.Strings(commandFiles)

	for _, path := range agentFiles {
		agent, err := claudeAgentFileToPivot(path)
		if err != nil {
			return nil, err
		}
		pf.Agents = append(pf.Agents, agent)
	}

	for _, path := range commandFiles {
		cmd, err := claudeCommandFileToPivot(path)
		if err != nil {
			return nil, err
		}
		pf.Commands = append(pf.Commands, cmd)
	}

	return pf, nil
}

func claudeAgentFileToPivot(path string) (pivot.AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pivot.AgentDefinition{}, err
	}

	fm, body, err := splitFrontmatter(string(data))
	if err != nil {
		return pivot.AgentDefinition{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}

	id := stringValue(fm["name"])
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	agent := pivot.AgentDefinition{
		ID:           id,
		Description:  stringValue(fm["description"]),
		Mode:         "primary",
		SystemPrompt: strings.TrimSpace(body),
		Permissions:  claudeToolsToPermissions(fm),
	}

	if model := stringValue(fm["model"]); model != "" {
		agent.Extensions = map[string]any{
			"claude": map[string]any{"model": model},
		}
	}

	return agent, nil
}

func claudeCommandFileToPivot(path string) (pivot.CommandDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pivot.CommandDefinition{}, err
	}

	content := string(data)
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	cmd := pivot.CommandDefinition{ID: id}

	lines := strings.Split(content, "\n")
	start := 0
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "<!-- agent:") {
		agentRef := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "<!-- agent:")), "-->")
		cmd.Agent = strings.TrimSpace(agentRef)
		start = 1
	}
	cmd.Template = strings.TrimLeft(strings.Join(lines[start:], "\n"), "\n")
	return cmd, nil
}

func claudeToolsToPermissions(fm map[string]any) *pivot.Permissions {
	tools := stringSlice(fm["tools"])
	if len(tools) == 0 && stringValue(fm["permissionMode"]) == "" {
		return nil
	}

	perms := &pivot.Permissions{}
	for _, tool := range tools {
		switch tool {
		case "Read":
			perms.Read = "allow"
		case "Bash":
			perms.Bash = "allow"
		case "WebFetch":
			perms.WebFetch = "allow"
		case "WebSearch":
			perms.WebSearch = "allow"
		}
	}

	switch stringValue(fm["permissionMode"]) {
	case "acceptEdits":
		perms.Edit = "allow"
	case "plan":
		perms.Edit = "deny"
	case "default":
		if perms.Edit == "" {
			perms.Edit = "ask"
		}
	}

	if perms.Read == "" && perms.Edit == "" && perms.Bash == nil && perms.WebFetch == "" && perms.WebSearch == "" {
		return nil
	}
	return perms
}

func splitFrontmatter(content string) (map[string]any, string, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(content, "---") {
		return map[string]any{}, content, nil
	}

	rest := content[len("---"):]
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, "", fmt.Errorf("unclosed frontmatter")
	}

	fmRaw := rest[:end]
	body := strings.TrimLeft(rest[end+len("\n---"):], "\r\n")

	fm := map[string]any{}
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return nil, "", fmt.Errorf("parse frontmatter: %w", err)
	}
	return fm, body, nil
}

func marshalPivot(pf *pivot.PivotFile) ([]byte, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(pf); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprint(v)
	}
}

func stringSlice(v any) []string {
	switch s := v.(type) {
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str := stringValue(item); str != "" {
				out = append(out, str)
			}
		}
		return out
	case []string:
		return s
	default:
		return nil
	}
}

func stringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = stringValue(v)
	}
	return out
}

func allSame(m map[string]string) bool {
	var first string
	for _, v := range m {
		if first == "" {
			first = v
		} else if v != first {
			return false
		}
	}
	return true
}

func firstValue(m map[string]string) string {
	for _, v := range m {
		return v
	}
	return ""
}
