# Shenron

<p align="center">
  <img src="docs/images/shenron.jpg" alt="Shenron — the configuration dragon" width="600">
</p>

Shenron keeps agent configurations aligned across AI coding assistants from one
CLI-agnostic source of truth.

Define agents, prompts, slash commands, permissions, and per-agent skill
bindings once in `shenron.yaml`. Shenron validates that pivot, previews the
native changes, then writes the corresponding Claude Code, Codex, and OpenCode
files.

## What it supports

| Capability | Claude Code | Codex | OpenCode |
|---|---|---|---|
| Agents | `~/.claude/agents/<id>.md` | `~/.codex/agents/<id>.toml` | `agent.<id>` in `~/.config/opencode/opencode.json` |
| Agent prompts | Markdown body | `developer_instructions` | `prompts/<id>.md` referenced from JSON |
| Slash commands | `~/.claude/commands/<id>.md` | `~/.codex/prompts/<id>.md` | `command.<id>` plus `command/<id>.md` |
| Permissions | `tools` and `permissionMode` | Sandbox, approvals, and web search | Native `permission` object |
| Per-agent skills | YAML frontmatter `skills` | Instruction hint | Native JSON `skills` array |
| Bootstrap with `shenron install` | After OpenCode | After Claude Code | Preferred import source |

## Install

Shenron requires Go 1.24 or newer.

```bash
git clone git@github.com:S1933/Shenron.git
cd Shenron
make build
```

This produces `./shenron` (or run `go build -o shenron ./cmd/shenron` directly).

## Quick start

Shenron manages installed configuration **packages**. Every command operates on
a package in the store (`~/.shenron/packages/` by default); there is no
`shenron init` or single-pivot mode.

```bash
# Install a local package directory.
./shenron install ./my-package

# See what is installed.
./shenron list

# Preview the package's native changes without writing.
./shenron diff my-package

# Push to every supported target.
./shenron push my-package
```

Work with one target only:

```bash
./shenron diff my-package --target opencode
./shenron push my-package --target claude-code
```

The first time a revision declares permission grants, `push` requires
`--allow-permissions`. After a successful push, `diff` reports `No changes` for
each synchronized target.

## Commands

| Command | Behavior |
|---|---|
| `shenron install <source>` | Install a local directory or a public HTTPS Git package. |
| `shenron list` | List installed packages, ordered by name. |
| `shenron update <name>` | Validate and replace an installed snapshot from a new source or ref. |
| `shenron diff <name>` | Show a package's native diff plus its permission grants and missing skills. |
| `shenron push <name>` | Generate and atomically write a package's native files, then update its state. |

Flags:

- `--store <path>` (root, persistent) — custom package cache directory (default `~/.shenron/packages`).
- `install --ref <tag-or-sha>` — pin the Git revision for HTTPS sources.
- `update --source <dir-or-url>` / `update --ref <tag-or-sha>` — replace the installed package's source and revision.
- `diff --target <name>` / `push --target <name>` — select `claude-code`, `codex`, or `opencode`.
- `push --force` — overwrite native files that changed after the last push.
- `push --allow-permissions` — approve the package revision's declared permission grants; the approval is bound to the installed revision and its permission digest.

## The pivot file

The pivot (`shenron.yaml`) is intentionally smaller than any native format.
Shared concepts stay portable; target-specific features live under `extensions`.

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

| Field | Rules |
|---|---|
| `id` | Required, unique, matched by `^[a-z][a-z0-9-]*$`. |
| `description` | Required; maximum 1024 characters. |
| `mode` | Required: `primary` or `subagent`. |
| `model` | Optional fallback used when no target-specific model is set. |
| `temperature` | Optional, `0.0`–`2.0`. |
| `systemPrompt` / `promptFile` | Mutually exclusive. `promptFile` is relative to the pivot directory and must exist. |
| `permissions` | Portable grants (`allow`/`ask`/`deny`) translated by each adapter. |
| `extensions` | Target-specific overrides. |
| `skills` | Optional ordered list of kebab-case skill names emitted as native agent metadata. |

Each adapter resolves the model from `extensions.<target>.model`, then the
shared `model`, then its own default. `permissions` map to native tools per
target; see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the per-target
translation.

### Skills

- `agents[].skills` binds skills to an agent and round-trips through OpenCode's
  JSON `skills` array and Claude Code's frontmatter `skills` list.
- Top-level `skills: [{name: ...}]` stores global references only; Shenron does
  not manage skill contents or install skills on another machine.

The repository's current dogfood bindings are in [`docs/SKILLS.md`](docs/SKILLS.md).

## Configuration packages

A package bundles a pivot with a manifest and ships as a self-contained
directory, so the same configuration can be installed on many machines or
released to other users.

```text
my-package/
├── shenron-package.yaml   # manifest: name, version, description, skills
└── shenron.yaml           # pivot: agents, commands, permissions
```

The manifest validates `schemaVersion: "1"`, a kebab-case `name`, a strict
semver `version`, a non-empty `description`, and `skills.required` /
`skills.optional`. Every `promptFile` must stay inside the package directory.

```bash
# Local directory
./shenron install ./my-package

# Public Git repository (HTTPS only, immutable tag or full commit SHA)
./shenron install https://github.com/acme/reviewers.git --ref 1.2.0

# Update to a new source or ref
./shenron update acme-reviewers --source https://github.com/acme/reviewers.git --ref 1.3.0
```

Each install copies the source into a content-addressed snapshot under the
store. Git sources require `--ref` (an immutable tag or full commit SHA);
branches, `HEAD`, SSH, and archive URLs are refused. The snapshot digest is
revalidated before every load, so a corrupted cache can never be pushed. Old
snapshots are retained across updates.

## Safety

- **Manual-edit protection** — `.shenron-state.json` records the hash of every
  file written by a successful push. If a managed native file later differs from
  both that state and the newly generated output, `push` refuses to overwrite it.
  Review the diff, reconcile, or use `push --force` deliberately.
- **Permission approval** — the first push of a revision that declares
  permission grants requires `--allow-permissions`. The approval is bound to the
  revision and the SHA-256 digest of the normalized grant list, so changed grants
  must be approved again. Missing required skills abort the push; missing
  optional skills warn and continue.
- **Foreign collisions** — `push` refuses to take over native resources it does
  not already own, returning `ErrPackageCollision`. `push --force` overwrites
  manually edited package-owned files.
- **OpenCode merge** — Shenron upserts pivot agents and commands into
  `opencode.json` and preserves native-only entries and unrelated top-level
  fields. The merge is upsert-only; removing an item from the pivot does not
  delete its nested entry.

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the full synchronization
pipeline and adapter internals.

## Current limitations

- Sync is pivot-to-native only; there is no automatic native-to-pivot merge.
- Native entries removed from the pivot are reported, not deleted.
- Skill bindings are metadata only; skill directories and `SKILL.md` contents
  are outside Shenron's scope.
- OpenCode JSON is structurally preserved, not guaranteed byte-identical.
- Git installs accept only public HTTPS repositories with an immutable tag or
  full commit SHA.

## Development

```bash
make test      # go test ./...
make lint      # golangci-lint run
make build     # go build -o shenron ./cmd/shenron
make clean     # remove the local binary
```

The root `shenron.yaml`, the generated `shenron` binary, and
`.shenron-state.json` are local dogfood artifacts and are gitignored.

## Further reading

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — module map, sync pipeline,
  adapter internals, and the extension guide for adding new targets.
- [`docs/prd/shenron.md`](docs/prd/shenron.md) — the detailed product contract.
- [`docs/SKILLS.md`](docs/SKILLS.md) — current dogfood skill bindings.
