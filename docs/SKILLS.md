# Agent skill bindings

`shenron.yaml` is the source of truth for skill metadata. `shenron push <name>`
emits each binding to both the Claude Code agent frontmatter and the OpenCode
agent object.

| agent | skills | file |
|---|---|---|
| `ask` | `using-superpowers`, `brainstorming`, `codebase-design`, `domain-modeling`, `ubiquitous-language` | `~/.claude/agents/ask.md` |
| `build` | `test-driven-development`, `verification-before-completion`, `dispatching-parallel-agents`, `setup-pre-commit`, `go-cli-conventions`, `schema-validation` | `~/.claude/agents/build.md` |
| `debug` | `systematic-debugging`, `verification-before-completion` | `~/.claude/agents/debug.md` |
| `git` | `git-guardrails-claude-code`, `resolving-merge-conflicts`, `finishing-a-development-branch` | `~/.claude/agents/git.md` |
| `plan` | `writing-plans`, `design-an-interface`, `codebase-design`, `to-issues`, `decision-mapping`, `go-cli-conventions`, `schema-validation` | `~/.claude/agents/plan.md` |
| `salameche` | `verification-before-completion`, `requesting-code-review` | `~/.claude/agents/salameche.md` |
| `carapuce` | `verification-before-completion`, `requesting-code-review` | `~/.claude/agents/carapuce.md` |
| `bulbizarre` | `verification-before-completion`, `requesting-code-review` | `~/.claude/agents/bulbizarre.md` |
| `orchestrator` | `using-superpowers`, `verification-before-completion`, `requesting-code-review`, `dispatching-parallel-agents`, `finishing-a-development-branch` | `~/.claude/agents/orchestrator.md` |

The current dogfood pivot contains nine agents. It has no `docs` agent, so no
binding is invented for one. Agents without critical skills may omit `skills`.

## Missing skills to write

All skills identified by the wiring plan are now written under
`~/.agents/skills/`.

| skill | status |
|---|---|
| `go-cli-conventions` | written |
| `schema-validation` | written |
| `atomic-file-write` | written |
| `golden-file-testing` | written |
| `adapter-pattern` | written |
| `binary-distribution` | written |
| `embedded-fixtures` | written |
