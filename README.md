# AgentSync

Sync agent configurations across AI coding assistants from a single source of truth.

AgentSync lets you define your agents, slash-commands, and their prompts **once** in a
CLI-agnostic pivot file (`agentsync.yaml`) and generate the native configuration for each
assistant. Edit one file, run `push`, and every tool stays in sync.

## Supported targets

| Target | Identifier | Config location | Layout |
|---|---|---|---|
| Claude Code | `claude-code` | `~/.claude/` | one Markdown file per agent/command (`agents/*.md`, `commands/*.md`) |
| OpenCode | `opencode` | `~/.config/opencode/` | single `opencode.json` + `prompts/*.md` and `command/*.md` |

## Why

Each assistant stores agent definitions in its own format — Claude Code uses Markdown
files with YAML frontmatter, OpenCode uses a nested JSON config plus external prompt
files. Keeping the same set of agents consistent across both by hand is tedious and
error-prone. AgentSync makes the pivot file authoritative and does the translation.

## Install

Requires Go 1.24+.

```bash
git clone git@github.com:S1933/AgentSync.git
cd AgentSync
make build          # produces ./agents-sync
# or: go build -o agents-sync ./cmd/agents-sync
```

## Quick start

```bash
# 1. Bootstrap a pivot file from your existing native configs
./agents-sync init

# 2. Edit agentsync.yaml to taste, then check it
./agents-sync validate

# 3. Preview what would change
./agents-sync push --dry-run

# 4. Propagate to the native configs
./agents-sync push
```

The pivot file is discovered automatically by walking up from the current directory, or
pointed at explicitly with `-c/--config <path>`.

## Commands

| Command | Description |
|---|---|
| `init` | Generate a skeleton `agentsync.yaml` from existing native configs. Refuses to overwrite an existing pivot. |
| `validate` | Validate the pivot file (schema, agent ids, modes). |
| `diff` | Show the differences between the pivot and the native configs. |
| `push` | Write the pivot config out to the native CLIs. |

### Flags

- `-c, --config <path>` *(global)* — path to `agentsync.yaml` (otherwise auto-discovered).
- `push --dry-run` — show changes without writing (equivalent to `diff`).
- `push --target <name>` / `diff --target <name>` — limit to a single target (`claude-code` or `opencode`).
- `push --force` — overwrite native files that were edited by hand since the last push.

## Pivot file

`agentsync.yaml` is the single source of truth.

```yaml
version: "1"

agents:
  - id: ask                       # required, lowercase kebab-case
    description: Read-only Q&A and feature framing.
    mode: primary                 # primary | subagent
    model: opus                   # optional; passed through per target
    temperature: 0.2              # optional
    systemPrompt: |-              # inline prompt…
      You are **ask**, a read-only Q&A agent.
    # promptFile: prompts/ask.md  # …or reference an external file (relative to the pivot)
    permissions:                  # optional, CLI-agnostic capability grants
      read: allow                 # allow | ask | deny
      edit: ask
      bash:                       # a string (allow/ask/deny) or a pattern map
        "go *": allow
        "rm *": deny
      webfetch: deny
      websearch: ask
      tasks:                      # per-subagent delegation grants
        build: allow
    extensions:                   # per-target escape hatch (see below)
      claude:
        tools: [Read, Glob, Grep, Bash]
      opencode:
        steps: 20

commands:
  - id: ship
    description: Ship the current changes.
    template: |-
      Review and ship the current changes.
    agent: build                  # optional default agent (opencode)
    model: opus                   # optional

skills:
  - name: test-driven-development
```

### Permissions → native mapping

Permissions are declared once and translated per target:

- **Claude Code** derives the `tools:` list from the grants (`read → Read`, `bash → Bash`,
  `webfetch → WebFetch`, `websearch → WebSearch`, `tasks → Task`) and maps `edit` to a
  `permissionMode` (`allow → acceptEdits`, `deny → plan`, `ask`/unset → default/omitted).
- **OpenCode** emits a `permission` block (`read` expands to `glob`/`grep`/`list`/`lsp`,
  plus `edit`/`bash`/`webfetch`/`websearch`/`task`).

### Extensions

`extensions` carries target-specific settings the pivot schema doesn't model directly:

- `extensions.claude.tools` — explicit Claude tools list (overrides the derived one).
- `extensions.claude.permissionMode` — explicit Claude permission mode.
- `extensions.opencode.steps` — OpenCode step budget.
- `extensions.opencode.permission` — OpenCode read-sub permission overrides.

## How sync works

1. **Generate** — each adapter renders the pivot agents/commands into its native files
   (Claude: standalone `.md` files; OpenCode: JSON fragments + prompt/command files).
2. **Diff** — generated output is compared against what's on disk and against the last
   push recorded in `.agentsync-state.json`.
3. **Write** — `push` writes changed files atomically and updates the state file.

**OpenCode merge is non-destructive and order-preserving.** Pivot agents/commands are
upserted into the nested `agent`/`command` objects of your existing `opencode.json`;
native-only entries and unrelated top-level keys are preserved verbatim, and the file's
key order is kept so pushes produce minimal diffs. Pushes are idempotent — running
`push` twice in a row is a no-op.

**Upsert-only.** Removing an agent from the pivot does **not** delete it from
`opencode.json`; managed entries are only created/updated, never removed. Remove
stale native entries by hand if you want them gone.

**Manual-edit protection.** If a native file was changed since AgentSync last wrote it,
`push` refuses to clobber it and lists the affected paths. Use `--force` to override.

## Project layout

```
cmd/agents-sync/        CLI entry point
internal/
  cli/                  commands (init, validate, diff, push), target registry
  pivot/                pivot schema, parsing, discovery
  adapter/
    claude/             Claude Code adapter (Markdown + frontmatter)
    opencode/           OpenCode adapter (nested JSON merge, ordered.go)
  diff/                 diff engine + push state tracking
  fsutil/               path resolution, atomic writes
testdata/               fixtures and golden files
docs/                   design notes
```

## Development

```bash
make test     # go test ./...
make lint     # golangci-lint run
make build    # go build -o agents-sync ./cmd/agents-sync
```

The pivot file at the repo root (`agentsync.yaml`) and the push state
(`.agentsync-state.json`) are per-user and gitignored.
