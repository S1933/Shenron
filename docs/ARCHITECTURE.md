# AgentSync Architecture

## Purpose and scope

AgentSync is a Go command-line application that treats `agentsync.yaml` as the
authoritative, tool-neutral description of coding-assistant agents, commands,
permissions, prompts, and skill references. It validates that pivot model and
renders it into the native configuration formats used by Claude Code and
OpenCode.

The system is intentionally one-way:

```text
agentsync.yaml -> validated pivot model -> target adapters -> native files
                                            |
                                            v
                                  diff and state tracking
```

`init` is the exception: it bootstraps a new pivot file from an existing
OpenCode or Claude Code configuration. Normal synchronization never reads a
native configuration back into the pivot.

## Architectural drivers

- Keep one CLI-neutral source of truth.
- Isolate target-specific formats and permission mappings behind adapters.
- Preview every observable change before writing it.
- Protect native files that users edited after the last successful push.
- Preserve native-only OpenCode configuration while updating managed entries.
- Ship as a single Go binary with no runtime service or database.

## System context

```mermaid
flowchart LR
    User[User] --> CLI[agents-sync CLI]
    Pivot[agentsync.yaml] --> CLI
    Prompt[Referenced prompt files] --> CLI
    CLI --> Claude[Claude Code config\n~/.claude]
    CLI --> OpenCode[OpenCode config\n~/.config/opencode]
    CLI --> State[.agentsync-state.json]
```

The application accesses only local files. There are no network calls, daemon
processes, plugin loaders, or remote persistence mechanisms in the current
implementation.

## Module map

| Module | Responsibility | Main interface |
|---|---|---|
| `cmd/agents-sync` | Process entry point and root Cobra command | Executable invocation |
| `internal/cli` | Command construction and synchronization orchestration | `RunInit`, `RunValidate`, `RunDiff`, `RunPush`, `Generate` |
| `internal/pivot` | Pivot schema, YAML parsing, validation, and discovery | `Discover`, `Parse`, pivot types |
| `internal/adapter` | Seam between the tool-neutral model and target formats | `Adapter` |
| `internal/adapter/claude` | Claude Markdown/frontmatter generation | `adapter.Adapter` implementation |
| `internal/adapter/opencode` | OpenCode JSON fragments, ordered merge, and prompt files | `adapter.Adapter` plus fragment accumulation |
| `internal/diff` | Disk comparison, unified output, manual-edit detection, push state | `ComputeDiffs`, `LoadState`, `SaveState` |
| `internal/fsutil` | Configuration paths and atomic replacement | `WriteFileAtomic`, path helpers |

Dependencies point inward toward the pivot model. Target adapters depend on
`internal/pivot`; the pivot module does not know which targets exist.

```mermaid
flowchart TD
    Main[cmd/agents-sync] --> CLI[internal/cli]
    CLI --> Pivot[internal/pivot]
    CLI --> AdapterInterface[internal/adapter]
    CLI --> Diff[internal/diff]
    CLI --> FS[internal/fsutil]
    AdapterInterface --> Pivot
    Claude[adapter/claude] --> AdapterInterface
    Claude --> Pivot
    Claude --> FS
    OpenCode[adapter/opencode] --> AdapterInterface
    OpenCode --> Pivot
    OpenCode --> FS
    CLI --> Claude
    CLI --> OpenCode
    Diff --> FS
```

## Core domain model

`internal/pivot` is the canonical model used by every synchronization path:

- `PivotFile` contains a schema version, agents, commands, and skill references.
- `AgentDefinition` contains identity, role, model selection, temperature,
  prompt source, normalized permissions, target extensions, and skills.
- `CommandDefinition` contains identity, description, template, and optional
  agent/model selection.
- `Permissions` expresses common capabilities with `allow`, `ask`, and `deny`.

Parsing and validation happen together at the pivot seam. Invalid identifiers,
duplicates, missing required values, invalid cross-references, conflicting
prompt sources, missing prompt files, invalid temperatures, and unsupported
permission values are rejected before any target generation or write occurs.

The parser currently accepts YAML fields that are unknown to the Go structs,
because it uses the default `yaml.Unmarshal` behavior rather than strict known-
field decoding.

## Synchronization pipeline

`diff`, `push`, and `push --dry-run` share the preparation path in
`internal/cli`:

```mermaid
sequenceDiagram
    participant U as User
    participant C as CLI orchestrator
    participant P as Pivot module
    participant A as Target adapters
    participant D as Diff/state module
    participant F as Filesystem

    U->>C: diff or push
    C->>P: Discover(config flag or current directory)
    P-->>C: pivot path
    C->>F: Read agentsync.yaml
    C->>P: Parse and validate
    P-->>C: PivotFile
    C->>A: Generate agents and commands
    A->>F: Read existing shared config when merging
    A-->>C: target -> path -> desired content
    C->>D: Load .agentsync-state.json
    C->>D: Compare desired content, disk, and prior hashes
    alt diff / dry run
        D-->>U: changes, warnings, unified diff
    else push with safe changes
        C->>F: Atomically replace changed files
        C->>D: Record hashes and adapter ownership
        D->>F: Atomically replace state file
    else manual edit without --force
        C-->>U: refuse push and list paths
    end
```

### Discovery

`pivot.Discover` gives an explicit `--config` path priority, otherwise walks
upward from the current directory looking for `agentsync.yaml`, then falls back
to `$HOME/.agentsync/agentsync.yaml`.

### Generation

`cli.Generate` iterates through every selected adapter, then through every pivot
agent and command. Generated files are grouped by adapter so push output and
state ownership remain target-aware.

Two optional internal seams supplement the public `Adapter` interface:

- `pivotDirSetter` supplies the pivot directory for resolving `promptFile`.
- `fragmentAccumulator` lets OpenCode collect fragments that share one JSON
  file before it performs a single merge.

These are discovered with Go type assertions inside the CLI orchestrator.

### Diff and manual-edit protection

For each desired file, `diff.ComputeDiffs` compares:

1. generated content,
2. current disk content, and
3. the SHA-256 hash stored after the last successful push.

If disk differs from both generated content and the last stored hash, the file
is classified as manually modified. `push` refuses to overwrite it unless
`--force` is supplied. Files previously tracked but no longer generated are
classified as orphaned and reported; they are not deleted.

The state file lives beside the pivot as `.agentsync-state.json`. It records the
content hash, path, and owning adapter for each written file. Adapter ownership
keeps a targeted push from reporting another target's files as orphaned.

### Writes

All application-owned writes use `fsutil.WriteFileAtomic`: create parent
directories, write a temporary file in the destination directory, apply the
requested permissions, close it, and rename it over the destination. This
prevents readers from observing partially written configuration.

The state update follows generated-file writes. The operation is safe against
partial file content, but it is not a transaction across all targets: a process
failure after some renames can leave native files updated while the state file
still describes the preceding push.

## Target adapters

The central extension seam is `internal/adapter.Adapter`:

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

This is a relatively deep module interface: callers provide normalized pivot
definitions and receive complete desired files, while format details remain in
the adapter implementations. `internal/cli/registry.go` is the composition root
that constructs and names the concrete adapters.

### Claude Code

The Claude adapter generates independent Markdown files:

- `~/.claude/agents/<id>.md` for agents,
- `~/.claude/commands/<id>.md` for commands.

It maps pivot fields into YAML frontmatter, derives Claude tools and permission
mode from normalized permissions, resolves prompts, and honors Claude-specific
extension overrides. Because each definition owns a standalone file,
`MergeFile` returns no merged content.

### OpenCode

The OpenCode adapter generates:

- prompt bodies under `~/.config/opencode/prompts/`,
- command bodies under `~/.config/opencode/command/`, and
- agent/command fragments merged into `opencode.json`.

It accumulates JSON fragments during generation, then performs one ordered
merge. Its `orderedObject` implementation preserves existing key order and raw
values while upserting managed `agent` and `command` entries. Unrelated
top-level keys and native-only nested entries survive the merge.

OpenCode synchronization is deliberately upsert-only. Removing an item from the
pivot does not remove the corresponding nested entry from `opencode.json`.

## Command behavior

| Command | Read path | Write path | Important behavior |
|---|---|---|---|
| `init` | First usable OpenCode config, then Claude config | New `./agentsync.yaml` | Refuses to overwrite an existing pivot |
| `validate` | Discovered pivot and referenced prompt files | None | Runs pivot validation only |
| `diff` | Pivot, native files, state | None | Reports per-target changes and orphans |
| `push --dry-run` | Same as `diff` | None | Delegates to the diff path |
| `push` | Pivot, native files, state | Native files and state | Refuses manual overwrites unless forced |

## Testing architecture

Tests live beside their modules and use temporary directories plus checked-in
fixtures:

- pivot discovery and validation tests exercise schema invariants;
- adapter tests compare complete generated artifacts with golden fixtures;
- diff and filesystem tests cover state classification and atomic replacement;
- CLI tests exercise commands through exported `Run*` functions and injectable
  adapters/paths;
- `internal/integration_test.go` runs end-to-end pivot-to-target scenarios,
  including idempotency, manual edits, target scoping, merging, and skills.

Test-specific constructors such as `NewAdapterWithBaseDir` place the filesystem
seam at adapter construction, so tests do not touch real user configuration.

## Extension guide

To add a new target:

1. Create `internal/adapter/<target>` and implement `adapter.Adapter`.
2. Keep all native format, permission, prompt, and path knowledge inside that
   adapter.
3. Add an adapter constructor to `internal/cli/registry.go`.
4. Add focused generation tests and golden fixtures.
5. Add an end-to-end CLI test using temporary target paths.
6. Update user documentation and the supported-target list.

If the target stores many definitions in one file, implement a target-local
fragment accumulator and deterministic merge. If it writes one file per
definition, return complete files directly and let `MergeFile` return `nil`.

## Constraints and notable design trade-offs

- Adapter registration is compile-time; dynamic plugins are out of scope.
- The CLI orchestrator depends on the concrete Claude and OpenCode packages to
  build the registry, while synchronization logic depends on their interface.
- The `Adapter` interface requires `MergeFile` even for standalone-file targets;
  optional behavior is represented by returning `nil`.
- Shared-file merge accumulation uses internal type assertions, so a future
  shared-file adapter must implement both the public adapter and the expected
  internal accumulation seam.
- State is hash-based, not a stored content snapshot; the tool detects divergent
  edits but does not perform a three-way merge.
- Orphan handling favors safety: stale managed files and nested entries are
  reported or preserved rather than automatically removed.
- `StateFile.Managed` metadata exists in the model but is not currently wired
  into synchronization; OpenCode's current behavior is preservation/upsert,
  not tracked pruning of formerly managed nested keys.
- The unified diff renderer is intentionally lightweight and line-position
  based; it is adequate for previews but is not a full minimal-diff algorithm.

## Repository layout

```text
cmd/agents-sync/              executable entry point
internal/
  cli/                        commands, registry, orchestration
  pivot/                      canonical schema, parsing, validation, discovery
  adapter/
    adapter.go                target interface
    claude/                   Claude Markdown adapter
    opencode/                 OpenCode JSON/Markdown adapter
  diff/                       comparison and state tracking
  fsutil/                     paths and atomic writes
  integration_test.go         end-to-end behavior
testdata/                     integration fixtures
docs/                         product and implementation notes
```
