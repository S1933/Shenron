package pivot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var idRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

var validPermissionValues = map[string]bool{
	"allow": true,
	"deny":  true,
	"ask":   true,
}

var validOpenCodePermissionKeys = map[string]bool{
	"glob": true,
	"grep": true,
	"list": true,
	"lsp":  true,
}

// Parse parses YAML pivot data and validates it against the schema.
func Parse(data []byte, pivotDir string) (*PivotFile, error) {
	var pf PivotFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}

	var errs []string
	errs = append(errs, validatePivot(&pf, pivotDir)...)

	if len(errs) > 0 {
		return nil, fmt.Errorf("validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return &pf, nil
}

func validatePivot(pf *PivotFile, pivotDir string) []string {
	var errs []string

	if pf.Version == "" {
		errs = append(errs, "version is required")
	}

	agentIDs := make(map[string]bool)
	for i, agent := range pf.Agents {
		prefix := fmt.Sprintf("agents[%d]", i)
		if agent.ID != "" {
			agentIDs[agent.ID] = true
		}
		errs = append(errs, validateAgent(agent, prefix, pivotDir)...)
	}

	for i, cmd := range pf.Commands {
		prefix := fmt.Sprintf("commands[%d]", i)
		errs = append(errs, validateCommand(cmd, prefix, agentIDs)...)
	}

	return errs
}

func validateAgent(agent AgentDefinition, prefix, pivotDir string) []string {
	var errs []string

	if agent.ID == "" {
		errs = append(errs, prefix+".id is required")
	} else if !idRegex.MatchString(agent.ID) {
		errs = append(errs, prefix+".id must match ^[a-z][a-z0-9-]*$")
	}

	if agent.Description == "" {
		errs = append(errs, prefix+".description is required")
	} else if len(agent.Description) > 1024 {
		errs = append(errs, prefix+".description must be 1-1024 characters")
	}

	if agent.Mode == "" {
		errs = append(errs, prefix+".mode is required")
	} else if agent.Mode != "primary" && agent.Mode != "subagent" {
		errs = append(errs, prefix+".mode must be primary or subagent")
	}

	hasSystemPrompt := strings.TrimSpace(agent.SystemPrompt) != ""
	hasPromptFile := strings.TrimSpace(agent.PromptFile) != ""
	if hasSystemPrompt && hasPromptFile {
		errs = append(errs, prefix+": systemPrompt and promptFile are mutually exclusive")
	}

	if hasPromptFile {
		resolved := filepath.Join(pivotDir, agent.PromptFile)
		if _, err := os.Stat(resolved); err != nil {
			errs = append(errs, fmt.Sprintf("%s.promptFile: file not found: %s", prefix, agent.PromptFile))
		}
	}

	if agent.Temperature != nil {
		t := *agent.Temperature
		if t < 0.0 || t > 2.0 {
			errs = append(errs, prefix+".temperature must be between 0.0 and 2.0")
		}
	}

	if agent.Permissions != nil {
		errs = append(errs, validatePermissions(agent.Permissions, prefix+".permissions")...)
	}

	if agent.Extensions != nil {
		errs = append(errs, validateExtensions(agent.Extensions, prefix+".extensions")...)
	}

	return errs
}

func validatePermissions(perms *Permissions, prefix string) []string {
	var errs []string

	errs = append(errs, validatePermissionEnum(perms.Read, prefix+".read")...)
	errs = append(errs, validatePermissionEnum(perms.Edit, prefix+".edit")...)
	errs = append(errs, validatePermissionEnum(perms.WebFetch, prefix+".webfetch")...)
	errs = append(errs, validatePermissionEnum(perms.WebSearch, prefix+".websearch")...)
	errs = append(errs, validateBashPermission(perms.Bash, prefix+".bash")...)

	for key, val := range perms.Tasks {
		errs = append(errs, validatePermissionEnum(val, fmt.Sprintf("%s.tasks.%s", prefix, key))...)
	}

	return errs
}

func validateBashPermission(bash any, prefix string) []string {
	if bash == nil {
		return nil
	}

	switch v := bash.(type) {
	case string:
		return validatePermissionEnum(v, prefix)
	case map[string]any:
		var errs []string
		for key, val := range v {
			s, ok := val.(string)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s.%s must be a string", prefix, key))
				continue
			}
			errs = append(errs, validatePermissionEnum(s, fmt.Sprintf("%s.%s", prefix, key))...)
		}
		return errs
	default:
		return []string{prefix + " must be a string or map[string]string"}
	}
}

func validatePermissionEnum(value, field string) []string {
	if value == "" {
		return nil
	}
	if !validPermissionValues[value] {
		return []string{fmt.Sprintf("%s must be allow, deny, or ask (got %q)", field, value)}
	}
	return nil
}

func validateExtensions(extensions map[string]any, prefix string) []string {
	opencode, ok := extensions["opencode"]
	if !ok {
		return nil
	}

	opencodeMap, ok := opencode.(map[string]any)
	if !ok {
		return []string{prefix + ".opencode must be an object"}
	}

	permission, ok := opencodeMap["permission"]
	if !ok {
		return nil
	}

	permMap, ok := permission.(map[string]any)
	if !ok {
		return []string{prefix + ".opencode.permission must be an object"}
	}

	var errs []string
	for key, val := range permMap {
		if !validOpenCodePermissionKeys[key] {
			errs = append(errs, fmt.Sprintf("%s.opencode.permission.%s: invalid key (must be glob, grep, list, or lsp)", prefix, key))
			continue
		}
		s, ok := val.(string)
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.opencode.permission.%s must be a string", prefix, key))
			continue
		}
		errs = append(errs, validatePermissionEnum(s, fmt.Sprintf("%s.opencode.permission.%s", prefix, key))...)
	}

	return errs
}

func validateCommand(cmd CommandDefinition, prefix string, agentIDs map[string]bool) []string {
	var errs []string

	if cmd.ID == "" {
		errs = append(errs, prefix+".id is required")
	} else if !idRegex.MatchString(cmd.ID) {
		errs = append(errs, prefix+".id must match ^[a-z][a-z0-9-]*$")
	}

	if cmd.Description == "" {
		errs = append(errs, prefix+".description is required")
	}

	if strings.TrimSpace(cmd.Template) == "" {
		errs = append(errs, prefix+".template is required")
	}

	if cmd.Agent != "" && !agentIDs[cmd.Agent] {
		errs = append(errs, fmt.Sprintf("%s.agent references unknown agent %q", prefix, cmd.Agent))
	}

	return errs
}
