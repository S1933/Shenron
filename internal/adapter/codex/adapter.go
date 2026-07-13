package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
	"github.com/pelletier/go-toml/v2"
)

const fileMode = 0o644

// Adapter renders Shenron definitions into Codex custom-agent and custom-prompt files.
type Adapter struct {
	baseDir  string
	pivotDir string
}

type agentFile struct {
	Name                  string   `toml:"name"`
	Description           string   `toml:"description"`
	Model                 string   `toml:"model,omitempty"`
	ModelReasoningEffort  string   `toml:"model_reasoning_effort,omitempty"`
	SandboxMode           string   `toml:"sandbox_mode,omitempty"`
	ApprovalPolicy        string   `toml:"approval_policy,omitempty"`
	WebSearch             string   `toml:"web_search,omitempty"`
	NicknameCandidates    []string `toml:"nickname_candidates,omitempty"`
	DeveloperInstructions string   `toml:"developer_instructions"`
}

func NewAdapter() *Adapter { return NewAdapterWithBaseDir(fsutil.CodexPath(), "") }

func NewAdapterWithBaseDir(baseDir, pivotDir string) *Adapter {
	return &Adapter{baseDir: baseDir, pivotDir: pivotDir}
}

func (a *Adapter) Name() string { return "codex" }

func (a *Adapter) SetPivotDir(dir string) {
	a.pivotDir = dir
}

func (a *Adapter) ValidateAgent(agent pivot.AgentDefinition) error {
	if agent.Mode != "primary" && agent.Mode != "subagent" {
		return fmt.Errorf("agent %q: mode must be primary or subagent", agent.ID)
	}
	return nil
}

// Generate renders custom-agent TOML files and custom-prompt Markdown files.
// The pivot-id -> native-name map is built locally so a command's delegation
// line can reference the referenced agent's resolved Codex name.
func (a *Adapter) Generate(pf *pivot.PivotFile) (adapter.GenerationResult, error) {
	var files []adapter.GeneratedFile
	nativeNames := make(map[string]string, len(pf.Agents))

	for _, ag := range pf.Agents {
		if err := a.ValidateAgent(ag); err != nil {
			return adapter.GenerationResult{}, err
		}
		nativeName := codexName(ag.ID, ag.Extensions)
		nativeNames[ag.ID] = nativeName
		instructions, err := resolveInstructions(ag, a.pivotDir)
		if err != nil {
			return adapter.GenerationResult{}, fmt.Errorf("generate agent %q: %w", ag.ID, err)
		}

		native := agentFile{
			Name: nativeName, Description: ag.Description, Model: resolveModel(ag),
			ModelReasoningEffort: codexString(ag.Extensions, "modelReasoningEffort"),
			SandboxMode:          resolveSandbox(ag), ApprovalPolicy: resolveApproval(ag),
			WebSearch: resolveWebSearch(ag), NicknameCandidates: codexStrings(ag.Extensions, "nicknameCandidates"),
			DeveloperInstructions: instructions,
		}
		data, err := toml.Marshal(native)
		if err != nil {
			return adapter.GenerationResult{}, fmt.Errorf("marshal Codex agent %q: %w", ag.ID, err)
		}
		files = append(files, adapter.GeneratedFile{
			Path:       filepath.Join(a.baseDir, "agents", nativeName+".toml"),
			Content:    data,
			Mode:       fileMode,
			Adapter:    a.Name(),
			ResourceID: ag.ID,
		})
	}

	for _, cmd := range pf.Commands {
		nativeAgent := cmd.Agent
		if name, ok := nativeNames[cmd.Agent]; ok {
			nativeAgent = name
		}
		var content strings.Builder
		content.WriteString("---\ndescription: ")
		content.WriteString(strconv.Quote(cmd.Description))
		content.WriteString("\n---\n\n")
		if nativeAgent != "" {
			content.WriteString("Delegate this task to the `")
			content.WriteString(nativeAgent)
			content.WriteString("` custom agent.\n\n")
		}
		content.WriteString(cmd.Template)
		files = append(files, adapter.GeneratedFile{
			Path:       filepath.Join(a.baseDir, "prompts", codexName(cmd.ID, cmd.Extensions)+".md"),
			Content:    []byte(content.String()),
			Mode:       fileMode,
			Adapter:    a.Name(),
			ResourceID: cmd.ID,
		})
	}

	return adapter.GenerationResult{Files: files}, nil
}

func (a *Adapter) TargetPaths() []string {
	return []string{filepath.Join(a.baseDir, "agents"), filepath.Join(a.baseDir, "prompts")}
}

func resolveInstructions(agent pivot.AgentDefinition, pivotDir string) (string, error) {
	instructions := agent.SystemPrompt
	if strings.TrimSpace(instructions) == "" && agent.PromptFile != "" {
		data, err := os.ReadFile(filepath.Join(pivotDir, agent.PromptFile))
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		instructions = string(data)
	}
	if strings.TrimSpace(instructions) == "" {
		instructions = agent.Description
	}
	if len(agent.Skills) > 0 {
		prefixed := make([]string, 0, len(agent.Skills))
		for _, skill := range agent.Skills {
			prefixed = append(prefixed, "$"+skill)
		}
		instructions = strings.TrimRight(instructions, "\n") + "\n\nWhen applicable, use these skills: " + strings.Join(prefixed, ", ") + "."
	}
	return instructions, nil
}

func codexExtension(extensions map[string]any) map[string]any {
	if extensions == nil {
		return nil
	}
	value, _ := extensions["codex"].(map[string]any)
	return value
}
func codexString(extensions map[string]any, key string) string {
	value := codexExtension(extensions)
	if value == nil {
		return ""
	}
	s, _ := value[key].(string)
	return s
}
func codexStrings(extensions map[string]any, key string) []string {
	value := codexExtension(extensions)
	if value == nil {
		return nil
	}
	switch list := value[key].(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
func codexName(id string, extensions map[string]any) string {
	if name := codexString(extensions, "name"); name != "" {
		return name
	}
	return id
}
func resolveModel(agent pivot.AgentDefinition) string {
	if model := codexString(agent.Extensions, "model"); model != "" {
		return model
	}
	return agent.Model
}
func resolveSandbox(agent pivot.AgentDefinition) string {
	if sandbox := codexString(agent.Extensions, "sandboxMode"); sandbox != "" {
		return sandbox
	}
	if agent.Permissions == nil {
		return ""
	}
	switch agent.Permissions.Edit {
	case "allow":
		return "workspace-write"
	case "ask", "deny":
		return "read-only"
	default:
		return ""
	}
}
func resolveApproval(agent pivot.AgentDefinition) string {
	if approval := codexString(agent.Extensions, "approvalPolicy"); approval != "" {
		return approval
	}
	if agent.Permissions == nil {
		return ""
	}
	switch agent.Permissions.Edit {
	case "ask":
		return "on-request"
	case "deny":
		return "never"
	}
	if bash, ok := agent.Permissions.Bash.(string); ok && bash == "ask" {
		return "on-request"
	}
	return ""
}
func resolveWebSearch(agent pivot.AgentDefinition) string {
	if search := codexString(agent.Extensions, "webSearch"); search != "" {
		return search
	}
	if agent.Permissions == nil {
		return ""
	}
	switch agent.Permissions.WebSearch {
	case "allow":
		return "live"
	case "ask", "deny":
		return "disabled"
	default:
		return ""
	}
}
