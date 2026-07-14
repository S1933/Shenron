# Shenron

<p align="center">
  <img src="docs/images/shenron.jpg" alt="Shenron — the configuration dragon" width="600">
</p>

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
| Per-agent skills | YAML frontmatter `skills` | Instruction hint | Dropped (see `docs/SKILLS.md`) |
| Bootstrap with `shenron install` | After OpenCode | After Claude Code | Preferred import source |

Shenron targets Claude Code, Codex, and OpenCode.

> **Why no `skills` in OpenCode output?** OpenCode v1.x does not recognize
> `skills` as an agent field, so the CLI forwards unknown top-level options to
> the LLM provider as payload fields. Strict providers (Pydantic
> `additionalProperties: false`, e.g. GLM-5.2) reject them with 400
> `Extra inputs are not permitted`. Pivot `agents[].skills` is therefore not
> emitted to OpenCode. Claude Code (native frontmatter) and Codex (instruction
> hint) keep receiving the bindings, and OpenCode agents are expected to
> reference skills from their prompt instead.

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

Shenron manages installed configuration packages. Every command operates on
a package in the store (`~/.shenron/packages/` by default); there is no
single-pivot mode.

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

To work with one target only:

```bash
./shenron diff my-package --target opencode
./shenron push my-package --target claude-code
```

The first time a revision declares permission grants, `push` requires
`--allow-permissions`. After a successful push, running `diff` again
reports `No changes` for each synchronized target.

## Commands and flags

| Command | Behavior |
|---|---|
| `shenron install <source>` | Install a local directory or a remote Git package (public HTTPS, or SSH via `git@host:path` / `ssh://`). |
| `shenron list` | List installed packages, ordered by name. |
| `shenron update <name>` | Validate and replace an installed snapshot from a new source or ref. |
| `shenron diff <name>` | Show a package's native diff plus its permission grants and missing skills. |
| `shenron push <name>` | Generate and atomically write a package's native files, then update its state. |
| `shenron doctor` | Check tool paths, snapshot-cache integrity, sync state, and pending permission approvals. |
| `shenron explain <name> --target <tool>` | Preview the native files a package translates into for one target, without writing. |

Common flags:

- `--store <path>` (root, persistent) selects a custom package cache
  directory (default `~/.shenron/packages`).
- `install --ref <tag-or-sha>` pins the Git revision for HTTPS and SSH sources.
  SSH sources (`git@host:path` or `ssh://…`) authenticate through your
  ssh-agent and verify host keys against `~/.ssh/known_hosts`; credentials are
  never read from the URL.
- `update --source <dir-or-url>` and `update --ref <tag-or-sha>` replace the
  installed package's source and revision.
- `diff --target <name>` and `push --target <name>` select `claude-code`,
  `codex`, or `opencode`.
- `push --force` overwrites native files that changed after the last push.
- `push --allow-permissions` approves the package revision's declared
  permission grants; the approval is bound to the installed revision and its
  permission digest.
- `--output json` (on `diff`, `push`, `explain`, and `doctor`) emits a
  machine-readable report on stdout — for `diff`/`push`, files with their
  adapter, status, and resource id plus any orphaned paths; for `explain`, the
  translated files; for `doctor`, the health checks. Human diagnostics stay on
  stderr.

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
| `skills` | Optional ordered list of kebab-case skill names, emitted to Claude Code frontmatter and Codex instructions only (see below). Local skill existence is not required. |

### Per-agent skills and global skill references

These two fields have different purposes:

- `agents[].skills` binds skills to an agent. It is emitted to Claude Code's
  agent frontmatter and added as a Codex instruction hint. **OpenCode output
  drops the field** because OpenCode v1.x forwards unknown agent keys to the
  LLM provider, which strict providers reject (see the table note above and
  [`docs/SKILLS.md`](docs/SKILLS.md)).
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

## Getting started

Shenron ships its configuration as a directory containing a manifest and a
pivot. Hand-write one or import the first usable native config you already
have, then install it with `shenron install`. There is no `shenron init` —
every command operates on an installed package.

## Configuration packages

A package bundles a pivot together with a manifest and ships as a
self-contained directory. Use packages when the same configuration should be
reliably installed on many machines or released to other users.

A package directory looks like this:

```text
my-package/
├── shenron-package.yaml   # manifest: name, version, description, skills
└── shenron.yaml           # pivot: agents, commands, permissions
```

`shenron-package.yaml` validates `schemaVersion: "1"`, a kebab-case `name`
matching `^[a-z][a-z0-9-]*$`, a strict semver `version`, a non-empty
`description`, and the `skills.required` / `skills.optional` arrays. Every
`promptFile` referenced by the pivot must stay inside the package directory.

### Install a package

```bash
# Local directory
./shenron install ./my-package

# Public Git repository over HTTPS (immutable tag or full commit SHA)
./shenron install https://github.com/acme/reviewers.git --ref 1.2.0

# Private/public repository over SSH (uses your ssh-agent and known_hosts)
./shenron install git@github.com:acme/reviewers.git --ref 1.2.0
./shenron install ssh://git@github.com/acme/reviewers.git --ref 1.2.0
```

The first install copies the source into a content-addressed snapshot under
`~/.shenron/packages/<name>/<digest>/`. Each subsequent `install` from Git
requires `--ref`; branches and `HEAD` are refused. The snapshot's digest is
revalidated before every load, so a corrupted cache can never be pushed.

### List and update

```bash
./shenron list                                # name, version, source, revision
./shenron update acme-reviewers \
    --source https://github.com/acme/reviewers.git --ref 1.3.0
```

`update` stages and validates the new snapshot before swapping the active
record. Old snapshots are retained.

### Diff and push

```bash
./shenron diff acme-reviewers                 # preview without writing
./shenron push acme-reviewers --allow-permissions
```

`diff` reports the same created / modified / manually-modified / orphaned
status the package flow has always reported, and additionally surfaces the
package's declared permission grants plus any required or optional skills
missing on disk.

`push` requires explicit approval the first time a revision declares
permission grants. The approval is bound to both the package revision and the
SHA-256 digest of the normalized grant list (`state/<name>/permissions.json`),
so a new revision with changed grants must be approved again. Missing required
skills abort the push; missing optional skills emit a warning and continue.

Packages also refuse to take over native resources they do not already own.
If a generated path (or a managed nested entry inside `opencode.json`) exists
on disk without being tracked in the package's own state file, `push` returns
`ErrPackageCollision` and aborts. `push --force` overwrites manually edited
package-owned files.

State for a package lives at `~/.shenron/state/<name>/.shenron-state.json`,
kept outside the immutable snapshot so it survives revisions.

## Synchronization and safety

The sync pipeline is:

1. Discover, parse, and validate the pivot.
2. Generate each target's files in memory.
3. Merge OpenCode agent and command fragments into the existing JSON.
4. Compare generated content with disk and `.shenron-state.json`.
5. Stage changed files, then commit the batch through a journalled
   transaction and record their hashes.

Each file is written atomically, and the batch is guarded by a journal
(`.shenron-journal.json`): the staged renames are recorded before any go live,
so a crash mid-commit is repaired on the next run by replaying the pending
renames (roll-forward), never leaving a push half-applied.

### OpenCode merge policy

Shenron upserts pivot agents and commands into the nested `agent` and
`command` objects. Native-only entries and unrelated top-level fields are
preserved, and existing key order is retained where possible. The JSON document
is parsed and serialized again, so byte-for-byte formatting is not guaranteed.

For `shenron push`, the merge is upsert-only on nested entries: removing an
agent or command from the pivot does not delete its nested OpenCode entry, so
remove stale JSON entries by hand. (The package apply flow goes further — it
tracks the entries it owns in `.shenron-state.json` and prunes owned entries
that leave the pivot, while preserving native-only entries you added by hand.)
Standalone managed files that are no longer generated can be reported as
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
- Package installs accept local directories, public HTTPS Git repositories, or
  SSH Git repositories (`git@host:path` and `ssh://`). Remote sources require an
  immutable tag or full commit SHA; branches, `HEAD`, embedded credentials, and
  archive URLs are refused. SSH auth is delegated to the caller's ssh-agent and
  host keys are verified against `known_hosts`.

## Architecture for contributors

```text
cmd/shenron/       Cobra entry point
internal/
  cli/                 install, list, update, diff, push, doctor, explain commands, registry, orchestration
  pivot/               YAML schema, discovery, parsing, validation
  package/             shenron-package.yaml manifest, immutable snapshots, Git and local install
  adapter/
    claude/            Markdown/frontmatter and command-file generation
    codex/             TOML custom-agent and Markdown custom-prompt generation
    opencode/          JSON fragments, ordered merge, prompt/command files
  diff/                status calculation, unified diffs, state hashes
  fsutil/              target paths and atomic file replacement
testdata/              end-to-end fixtures
docs/                  PRD, plans, and skill-binding matrix
```

The core consumes the `adapter.Adapter` interface. `Generate` is a single,
side-effect-free pass over the whole pivot: it returns every file plus any
nested-config fragments, so one adapter instance is reusable and safe under
concurrency.

```go
type Adapter interface {
    Name() string
    ValidateAgent(pivot.AgentDefinition) error
    Generate(*pivot.PivotFile) (GenerationResult, error)
    TargetPaths() []string
}
```

Optional behaviors are expressed as capability interfaces (`capabilities.go`)
that the sync runtime probes for with type assertions:

- `MergingAdapter` (`MergeFile`, `ConfigPath`) folds accumulated fragments into
  a shared config file. OpenCode implements it to merge all agent and command
  fragments into one `opencode.json`; Claude Code and Codex generate
  independent files and do not.
- `ManagedPruner` removes leaves shenron previously managed but the current
  pivot no longer generates, preserving entries it never owned.
- `PivotDirectoryAware` receives the pivot directory to resolve relative
  `promptFile` references during generation.

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
- cobra-driven surface tests for the seven top-level commands
  (`commands_test.go`);
- install, list, update, diff, push, permissions, skills, and
  foreign-collision tests against the package store;
- atomic-write and state-file tests;
- end-to-end round-trip tests for all three targets, including per-agent skills.

The root `shenron.yaml`, generated `shenron` binary, and
`.shenron-state.json` are local dogfood artifacts and are gitignored.

For the detailed product contract, see
[`docs/prd/shenron.md`](docs/prd/shenron.md).
