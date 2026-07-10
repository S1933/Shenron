package opencode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jnuel/agentsync/internal/pivot"
)

const promptFileRef = "{file:./prompts/%s.md}"

// GenerateAgentFragment produces the OpenCode JSON fragment and prompt file for an agent.
func GenerateAgentFragment(agent pivot.AgentDefinition, pivotDir string) (jsonFragment map[string]any, promptPath string, promptContent string, err error) {
	fragment := map[string]any{
		"description": agent.Description,
		"prompt":      fmt.Sprintf(promptFileRef, agent.ID),
	}

	if agent.Mode == "subagent" {
		fragment["mode"] = "subagent"
	}

	if model := resolveOpenCodeModel(agent); model != "" {
		fragment["model"] = model
	}

	if agent.Temperature != nil {
		fragment["temperature"] = *agent.Temperature
	}

	if steps := extractSteps(agent.Extensions); steps != nil {
		fragment["steps"] = steps
	}

	if perms := buildPermissionBlock(agent); len(perms) > 0 {
		fragment["permission"] = perms
	}

	promptContent, err = resolvePromptContent(agent, pivotDir)
	if err != nil {
		return nil, "", "", err
	}

	promptPath = filepath.Join("prompts", agent.ID+".md")
	return fragment, promptPath, promptContent, nil
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

func extractSteps(extensions map[string]any) any {
	if extensions == nil {
		return nil
	}
	opencode, ok := extensions["opencode"]
	if !ok {
		return nil
	}
	opencodeMap, ok := opencode.(map[string]any)
	if !ok {
		return nil
	}
	steps, ok := opencodeMap["steps"]
	if !ok {
		return nil
	}
	return steps
}

func openCodeString(extensions map[string]any, key string) (string, bool) {
	if extensions == nil {
		return "", false
	}
	opencode, ok := extensions["opencode"]
	if !ok {
		return "", false
	}
	opencodeMap, ok := opencode.(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := opencodeMap[key].(string)
	return s, ok
}

// resolveOpenCodeModel returns extensions.opencode.model when set, otherwise falls
// back to the pivot's scalar model field.
func resolveOpenCodeModel(agent pivot.AgentDefinition) string {
	if model, ok := openCodeString(agent.Extensions, "model"); ok {
		return model
	}
	return agent.Model
}

func extractOpenCodePermissionOverride(extensions map[string]any) map[string]string {
	if extensions == nil {
		return nil
	}
	opencode, ok := extensions["opencode"]
	if !ok {
		return nil
	}
	opencodeMap, ok := opencode.(map[string]any)
	if !ok {
		return nil
	}
	permission, ok := opencodeMap["permission"]
	if !ok {
		return nil
	}
	permMap, ok := permission.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(permMap))
	for key, val := range permMap {
		if s, ok := val.(string); ok {
			result[key] = s
		}
	}
	return result
}

func buildPermissionBlock(agent pivot.AgentDefinition) map[string]any {
	if agent.Permissions == nil && agent.Extensions == nil {
		return nil
	}

	perms := agent.Permissions
	result := map[string]any{}

	readSubs := []string{"glob", "grep", "list", "lsp"}
	override := extractOpenCodePermissionOverride(agent.Extensions)
	if len(override) > 0 {
		for _, key := range readSubs {
			if val, ok := override[key]; ok {
				result[key] = val
			}
		}
	} else if perms != nil && perms.Read != "" {
		for _, key := range readSubs {
			result[key] = perms.Read
		}
	}

	if perms != nil {
		if perms.Edit != "" {
			result["edit"] = perms.Edit
		}
		if perms.Bash != nil {
			result["bash"] = normalizeBashPermission(perms.Bash)
		}
		if perms.WebFetch != "" {
			result["webfetch"] = perms.WebFetch
		}
		if perms.WebSearch != "" {
			result["websearch"] = perms.WebSearch
		}
		if len(perms.Tasks) > 0 {
			result["task"] = perms.Tasks
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeBashPermission(bash any) any {
	switch v := bash.(type) {
	case string:
		return v
	case map[string]any:
		return v
	case map[string]string:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out
	default:
		return bash
	}
}
