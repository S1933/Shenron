package opencode

import (
	"fmt"
	"path/filepath"

	"github.com/jnuel/agentsync/internal/pivot"
)

const commandFileRef = "{file:./command/%s.md}"

// GenerateCommandFragment produces the OpenCode JSON fragment and command template file.
func GenerateCommandFragment(cmd pivot.CommandDefinition) (jsonFragment map[string]any, cmdPath string, cmdContent string, err error) {
	fragment := map[string]any{
		"description": cmd.Description,
		"template":    fmt.Sprintf(commandFileRef, cmd.ID),
	}

	if cmd.Agent != "" {
		fragment["agent"] = cmd.Agent
	}

	if cmd.Model != "" {
		fragment["model"] = cmd.Model
	}

	cmdPath = filepath.Join("command", cmd.ID+".md")
	return fragment, cmdPath, cmd.Template, nil
}
