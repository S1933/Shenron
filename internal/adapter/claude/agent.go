package claude

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
	"gopkg.in/yaml.v3"
)

type agentFrontmatter struct {
	Name           string `yaml:"name"`
	Description    string `yaml:"description"`
	Tools          string `yaml:"tools,omitempty"`
	Model          string `yaml:"model,omitempty"`
	PermissionMode string `yaml:"permissionMode,omitempty"`
}

// GenerateAgent produces a Claude Code agent Markdown file with YAML frontmatter.
func GenerateAgent(agent pivot.AgentDefinition, pivotDir string) (map[string]string, error) {
	return generateAgentFile(agent, pivotDir, fsutil.ClaudePath())
}

func generateAgentFile(agent pivot.AgentDefinition, pivotDir, baseDir string) (map[string]string, error) {
	body, err := resolvePromptContent(agent, pivotDir)
	if err != nil {
		return nil, err
	}

	fm := agentFrontmatter{
		Name:           agent.ID,
		Description:    agent.Description,
		Model:          resolveModel(agent),
		Tools:          strings.Join(resolveTools(agent), ", "),
		PermissionMode: resolvePermissionMode(agent),
	}

	fmYAML, err := marshalFrontmatter(&fm)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmYAML)
	buf.WriteByte('\n')
	buf.WriteString("---\n\n")
	buf.WriteString(body)

	agentPath := filepath.Join(baseDir, "agents", agent.ID+".md")
	return map[string]string{agentPath: buf.String()}, nil
}

func marshalFrontmatter(fm *agentFrontmatter) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, fmt.Errorf("marshal frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close frontmatter encoder: %w", err)
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

func resolvePromptContent(agent pivot.AgentDefinition, pivotDir string) (string, error) {
	if strings.TrimSpace(agent.SystemPrompt) != "" {
		return agent.SystemPrompt, nil
	}
	if agent.PromptFile != "" {
		data, err := os.ReadFile(filepath.Join(pivotDir, agent.PromptFile))
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

// resolveTools returns the explicit Claude tools list from extensions.claude.tools
// when present, otherwise derives one from the pivot permissions.
func resolveTools(agent pivot.AgentDefinition) []string {
	if tools, ok := claudeStringSlice(agent.Extensions, "tools"); ok {
		return tools
	}
	return mapTools(agent.Permissions)
}

// resolveModel returns extensions.claude.model when set, otherwise falls back to
// the pivot's scalar model field.
func resolveModel(agent pivot.AgentDefinition) string {
	if model, ok := claudeString(agent.Extensions, "model"); ok {
		return model
	}
	return agent.Model
}

// resolvePermissionMode returns extensions.claude.permissionMode when set, otherwise
// derives it from the pivot permissions. Returns "" when nothing constrains it, so the
// frontmatter omits the field rather than injecting a spurious "default".
func resolvePermissionMode(agent pivot.AgentDefinition) string {
	if mode, ok := claudeString(agent.Extensions, "permissionMode"); ok {
		return mode
	}
	return mapPermissionMode(agent.Permissions)
}

func claudeExtension(extensions map[string]any) map[string]any {
	if extensions == nil {
		return nil
	}
	claude, ok := extensions["claude"].(map[string]any)
	if !ok {
		return nil
	}
	return claude
}

func claudeString(extensions map[string]any, key string) (string, bool) {
	claude := claudeExtension(extensions)
	if claude == nil {
		return "", false
	}
	s, ok := claude[key].(string)
	return s, ok
}

func claudeStringSlice(extensions map[string]any, key string) ([]string, bool) {
	claude := claudeExtension(extensions)
	if claude == nil {
		return nil, false
	}
	switch v := claude[key].(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func mapTools(perms *pivot.Permissions) []string {
	if perms == nil {
		return nil
	}

	var tools []string
	if perms.Read == "allow" {
		tools = append(tools, "Read")
	}
	if hasBashAllow(perms.Bash) {
		tools = append(tools, "Bash")
	}
	if perms.WebFetch == "allow" {
		tools = append(tools, "WebFetch")
	}
	if perms.WebSearch == "allow" {
		tools = append(tools, "WebSearch")
	}
	if hasTaskAllow(perms.Tasks) {
		tools = append(tools, "Task")
	}
	return tools
}

func mapPermissionMode(perms *pivot.Permissions) string {
	if perms == nil || perms.Edit == "" {
		return ""
	}
	switch perms.Edit {
	case "allow":
		return "acceptEdits"
	case "ask":
		return "default"
	case "deny":
		return "plan"
	default:
		return "default"
	}
}

func hasBashAllow(bash any) bool {
	switch v := bash.(type) {
	case string:
		return v == "allow"
	case map[string]string:
		for _, val := range v {
			if val == "allow" {
				return true
			}
		}
	case map[string]any:
		for _, val := range v {
			if s, ok := val.(string); ok && s == "allow" {
				return true
			}
		}
	}
	return false
}

func hasTaskAllow(tasks map[string]string) bool {
	for _, val := range tasks {
		if val == "allow" {
			return true
		}
	}
	return false
}
