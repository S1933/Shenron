# Shenron

Shenron keeps agent configurations aligned across AI coding assistants from
one CLI-agnostic source of truth.

Define agents, prompts, slash commands, permissions, and per-agent skill
bindings once in `shenron.yaml`. Shenron validates that pivot, previews the
native changes, then writes the corresponding Claude Code, Codex, and OpenCode files.

## What it supports

| Capability | Claude Code | Codex | OpenCode |
|---|---|---|---|
| Agents | `~/.claude/agents/<id>.md` | `~/.codex/agents/<id>.toml` | `agent.<id>` in `~/.config/opencode/opencode.json` |
| Agent prompts | Markdown body | `developer_instructions` | `prompts/<id>.md` referenced from JSON |
| Slash commands | `~/.claude/commands/<id>.md` | `~/.codex/prompts/<id>.md` | `command.<id>` plus `command/<id>.md` |
| Permissions | `tools` and `permissionMode` | Sandbox, approvals, and web search | Native `permission` object |
| Per-agent skills | YAML frontmatter `skills` | Instruction hint | Native JSON `skills` array |
| Bootstrap with `init` | After OpenCode | After Claude Code | Preferred import source |

Shenron targets Claude Code, Codex, and OpenCode.

## Install

Shenron requires Go 1.24 or newer.

```bash
git clone git@github.com:S1933/Shenron.git
cd Shenron
make build
```

This produces `./shenron`. You can also build it directly:

```bash
go build -o shenron ./cmd/shenron
```

## Quick start

```bash
# Import the first available native configuration into ./shenron.yaml.
# OpenCode is tried first, then Claude Code, then Codex.
./shenron init

# Edit the pivot and validate it.
./shenron validate

# Preview all native changes without writing.
./shenron diff

# Push to every supported target.
./shenron push
```

To work with one target only:

```bash
./shenron diff --target opencode
./shenron push --target claude-code
```

After a successful push, running `diff` again reports `No changes` for each
synchronized target.

## Commands and flags

| Command | Behavior |
|---|---|
| `init` | Creates `./shenron.yaml` from the first usable native config. Refuses to overwrite an existing pivot. |
| `validate` | Parses the pivot and checks schema rules, identifiers, permissions, references, and prompt files. |
| `diff` | Shows created, modified, manually modified, and orphaned native files without writing. |
| `push` | Generates and atomically writes native files, then updates `.shenron-state.json`. |

Common flags:

- `-c, --config <path>` selects an explicit pivot file.
- `diff --target <name>` and `push --target <name>` select `claude-code`,
  `codex`, or `opencode`.
- `push --dry-run` is equivalent to `diff`.
- `push --force` overwrites native files that changed after the last push.

Without `--config`, Shenron searches for `shenron.yaml` from the current
directory upward to the filesystem root. If none is found, it tries
`~/.shenron/shenron.yaml`.

## Pivot file

The pivot is intentionally smaller than either native format. Shared concepts
stay portable; target-specific features live under `extensions`.

```yaml
version: "1"

agents:
  - id: build
    description: Implements approved changes.
    mode: primary                 # primary | subagent
    model: sonnet                 # optional shared fallback
    temperature: 0.2             # optional, 0.0 through 2.0
    systemPrompt: |-
      You are the build agent.
    # promptFile: prompts/build.md  # alternative to systemPrompt

    skills:                       # emitted to both native agent formats
      - test-driven-development
      - verification-before-completion

    permissions:
      read: allow                 # allow | ask | deny
      edit: ask
      bash:                       # enum or command-pattern map
        "go *": allow
        "rm *": deny
      webfetch: deny
      websearch: ask
      tasks:
        review: allow

    extensions:
      claude:
        model: opus               # overrides the shared model for Claude
        tools: [Read, Glob, Grep, Bash]
        permissionMode: default
      opencode:
        model: anthropic/claude-sonnet-4-5
        steps: 40
        reasoningEffort: high
        permission:
          glob: allow
          grep: allow
          list: allow
          lsp: deny
      codex:
        model: gpt-5.4
        modelReasoningEffort: high
        sandboxMode: workspace-write
        approvalPolicy: on-request
        webSearch: live

commands:
  - id: ship
    description: Ship the current changes.
    template: |-
      Review and ship the current changes.
    agent: build
    model: sonnet

# Optional global skill references retained by the pivot schema.
# Shenron does not install, copy, or emit their content.
skills:
  - name: test-driven-development
```

### Agent fields

| Field | Rules and behavior |
|---|---|
| `id` | Required, unique, and matched by `^[a-z][a-z0-9-]*$`. |
| `description` | Required; maximum 1024 characters. |
| `mode` | Required: `primary` or `subagent`. |
| `model` | Optional fallback used when the target-specific model is absent. |
| `temperature` | Optional number from `0.0` to `2.0`. |
| `systemPrompt` / `promptFile` | Mutually exclusive. `promptFile` is relative to the pivot directory and must exist. |
| `permissions` | Portable grants translated by each adapter. |
| `extensions` | Target-specific overrides and fields. |
| `skills` | Optional ordered list of kebab-case skill names, emitted as native agent metadata. Local skill existence is not required. |

### Per-agent skills and global skill references

These two fields have different purposes:

- `agents[].skills` binds skills to an agent. It round-trips through OpenCode's
  JSON `skills` array and Claude Code's frontmatter `skills` list.
- Top-level `skills: [{name: ...}]` stores global references only. Shenron
  does not manage skill contents or install skills on another machine.

The repository's current dogfood bindings are documented in
[`docs/SKILLS.md`](docs/SKILLS.md).

### Model resolution

For agents, each adapter first looks for its target-specific override:

1. `extensions.claude.model`, `extensions.codex.model`, or `extensions.opencode.model`
2. the shared `model` field
3. the target's own default when neither is set

This lets one pivot select different providers or model aliases without
forcing target-specific names into the shared field.

### Permissions

Claude Code derives:

- `read: allow` → `Read`
- any allowed bash rule → `Bash`
- `webfetch: allow` → `WebFetch`
- `websearch: allow` → `WebSearch`
- any allowed task → `Task`
- `edit: allow | ask | deny` → `acceptEdits | default | plan`

`extensions.claude.tools` and `extensions.claude.permissionMode` override those
derived values.

OpenCode emits `edit`, `bash`, `webfetch`, `websearch`, and `task` directly in
its `permission` object. A shared `read` value expands to `glob`, `grep`,
`list`, and `lsp`; `extensions.opencode.permission` can override those four
sub-permissions individually.

Codex derives a coarse sandbox from `edit`, and maps `websearch` to Codex's
native search mode. Command-pattern bash rules, `read`, `webfetch`, and task
permissions have no equivalent per-agent Codex enforcement. Use
`extensions.codex` (`sandboxMode`, `approvalPolicy`, and `webSearch`) for an
explicit native override. Codex receives each agent skill list as an
instruction hint; Shenron does not resolve local skill paths or install skills.

## Bootstrap and round-trip behavior

`shenron init` writes a new pivot in the current directory:

1. It tries `~/.config/opencode/opencode.json`.
2. If OpenCode is missing or unusable, it tries `~/.claude/agents` and
   `~/.claude/commands`.
3. If Claude Code is unavailable, it tries `~/.codex/agents` and
   `~/.codex/prompts`.
4. It imports supported agents, commands, prompts, permissions, model
   overrides, and per-agent skills.

Bootstrap is intentionally selective. Native fields without a pivot equivalent
are ignored, except for supported values preserved under `extensions`.

## Synchronization and safety

The sync pipeline is:

1. Discover, parse, and validate the pivot.
2. Generate each target's files in memory.
3. Merge OpenCode agent and command fragments into the existing JSON.
4. Compare generated content with disk and `.shenron-state.json`.
5. Write changed files atomically and record their hashes.

### OpenCode merge policy

Shenron upserts pivot agents and commands into the nested `agent` and
`command` objects. Native-only entries and unrelated top-level fields are
preserved, and existing key order is retained where possible. The JSON document
is parsed and serialized again, so byte-for-byte formatting is not guaranteed.

The merge is deliberately upsert-only. Removing an agent or command from the
pivot does not delete its nested OpenCode entry; remove stale JSON entries by
hand. Standalone managed files that are no longer generated can be reported as
orphaned, but Shenron still leaves deletion to you.

### Manual-edit protection

`.shenron-state.json`, stored beside the pivot, records the hash of every file
written by a successful push. If a managed native file later differs from both
that state and the newly generated output, Shenron marks it as manually
modified and refuses to overwrite it. Review the diff, reconcile the change, or
use `push --force` deliberately.

## Current limitations

- Sync is pivot-to-native; there is no automatic native-to-pivot merge after
  initialization.
- Native entries removed from the pivot are warned about, not deleted.
- Skill bindings are metadata only; skill directories and `SKILL.md` contents
  are outside Shenron's management scope.
- Skill-name validation checks kebab-case syntax, not local filesystem
  availability.
- OpenCode JSON is structurally preserved, not guaranteed byte-identical.

## Architecture for contributors

```text
cmd/shenron/       Cobra entry point
internal/
  cli/                 init, validate, diff, push, registry, orchestration
  pivot/               YAML schema, discovery, parsing, validation
  adapter/
    claude/            Markdown/frontmatter and command-file generation
    codex/             TOML custom-agent and Markdown custom-prompt generation
    opencode/          JSON fragments, ordered merge, prompt/command files
  diff/                status calculation, unified diffs, state hashes
  fsutil/              target paths and atomic file replacement
testdata/              end-to-end fixtures
docs/                  PRD, plans, and skill-binding matrix
```

The core consumes the `adapter.Adapter` interface:

```go
type Adapter interface {
    Name() string
    ValidateAgent(pivot.AgentDefinition) error
    GenerateAgent(pivot.AgentDefinition) (map[string]string, error)
    GenerateCommand(pivot.CommandDefinition) (map[string]string, error)
    TargetPaths() []string
    MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error)
}
```

OpenCode additionally exposes an internal fragment accumulator so orchestration
can merge all generated agent and command fragments into one `opencode.json`.
Claude Code generates independent files and returns no merged file.

To add a target:

1. Implement the adapter interface in `internal/adapter/<target>`.
2. Keep target-specific translation inside that package.
3. Register the adapter in `internal/cli/registry.go`.
4. Add mapping tests, golden fixtures, and end-to-end coverage.

## Testing and development

```bash
make test      # go test ./...
make lint      # golangci-lint run
make build     # go build -o shenron ./cmd/shenron
make clean     # remove the local binary
```

The test suite contains:

- pivot parsing, validation, and discovery tests;
- adapter mapping and golden-file tests;
- ordered OpenCode merge and preservation tests;
- CLI bootstrap, diff, push, force, and orphan-scope tests;
- atomic-write and state-file tests;
- end-to-end round-trip tests for all three targets, including per-agent skills.

The root `shenron.yaml`, generated `shenron` binary, and
`.shenron-state.json` are local dogfood artifacts and are gitignored.

For the detailed product contract, see
[`docs/prd/shenron.md`](docs/prd/shenron.md).
