# braid

A CLI that orchestrates AI code agents through composable operators.
Feed it a task and it'll run review loops, race parallel implementations,
compare approaches, or step through a task list — all in a single
command line.

See [IMPLEMENTATION.md](IMPLEMENTATION.md) for the architecture summary.

## Install

```bash
go install github.com/ihavespoons/braid@latest
```

Or clone and build:

```bash
git clone https://github.com/ihavespoons/braid
cd braid
go build -o braid .
```

## Prerequisites

- `git` — required; braid runs composition operators inside git worktrees
- Either:
  - `claude` (Claude Code CLI) on PATH — for `--sandbox agent` (default)
  - `docker` with a running daemon — for `--sandbox docker`
- A Claude auth token: either `CLAUDE_CODE_OAUTH_TOKEN` in the environment
  or a `~/.claude.json` from `claude login`

Run `braid doctor` to verify.

## Quick start

```bash
# Scaffold config
braid init

# Simple work
braid "implement a rate limiter"

# Work → review → gate loop (up to 3 iterations)
braid "implement a rate limiter" review

# Work, then iterate 3 times
braid "implement a rate limiter" x3

# Race 3 parallel implementations, judge picks the best
braid "implement a rate limiter" v3 pick "most correct + tests pass"

# Fork into two approaches
braid "use a token bucket" vs "use a sliding window" pick "simpler"

# Step through PLAN.md tasks until done
braid "work through the plan" ralph 10 "are all tasks complete"
```

## Operators

| Operator              | Meaning                                                                       |
|-----------------------|-------------------------------------------------------------------------------|
| `review`              | `work → review → gate` loop (default 3 iterations)                           |
| `xN` / `repeat N`     | Run the inner pipeline N times sequentially, threading output                 |
| `ralph [N] "gate"`    | Iterate tasks until the gate emits DONE (max N, default 100)                 |
| `vN` / `race N`       | Run N parallel implementations in isolated worktrees                          |
| `vs`                  | Fork into two branches (A vs B)                                               |
| `pick "criteria"`     | Resolver: judge agent selects the winning branch + git merges it              |
| `merge "criteria"`    | Resolver: synthesis agent combines best parts, git merges result              |
| `compare`             | Resolver: generate markdown comparison doc; worktrees preserved               |

Operators compose: `braid "task" review v3 pick "best"` runs 3 parallel
review loops and picks the winner.

### Gate verdict keywords

After each review iteration, the gate's output is scanned line-by-line:

- **DONE**: `DONE`, `PASS`, `COMPLETE`, `APPROVE`, `ACCEPT`
- **ITERATE**: `ITERATE`, `REVISE`, `RETRY`

Ambiguous output defaults to `ITERATE` (favoring more iterations over
premature completion).

Ralph uses a separate verdict: `NEXT` / `CONTINUE` vs
`DONE` / `COMPLETE` / `FINISHED` (defaults to `DONE` to avoid runaway
progression).

## Configuration

`braid init` creates `.braid/` with three files:

### `.braid/config.json`

```json
{
  "sandbox": "agent",
  "animation": "strip",
  "agent": "claude",
  "permissions": "acceptEdits",
  "env": ["CLAUDE_CODE_OAUTH_TOKEN"],
  "steps": {
    "work":    {},
    "review":  {},
    "gate":    {},
    "iterate": {},
    "ralph":   {"permissions": "bypassPermissions"}
  },
  "retry": {
    "enabled": true,
    "pollIntervalMinutes": 5,
    "maxWaitMinutes": 360
  }
}
```

Each `steps.*` entry may override `agent`, `model`, `sandbox`, and
`permissions` for that step. `iterate` falls back to `work` and `ralph`
falls back to `gate`.

`permissions` controls claude's `--permission-mode`. Valid values:
`default`, `acceptEdits`, `auto`, `bypassPermissions`, `dontAsk`, `plan`.
When omitted, native runs default to `acceptEdits` (auto-accept edits,
prompt for everything else) and docker runs default to
`bypassPermissions` (the container is already isolated, and headless
`-p` calls hang on interactive prompts).

### `.braid/docker.json`

```json
{
  "network": {
    "mode": "restricted",
    "allowedHosts": ["api.anthropic.com"]
  },
  "dockerfile": ".braid/Dockerfile"
}
```

Only consulted when `--sandbox docker` is active. Restricted mode uses
iptables in the container to deny all egress except DNS and HTTPS to the
allowlist.

`dockerfile` is optional. When set, the given path (relative to the
project root, or absolute) replaces the embedded Dockerfile when building
`braid-sandbox:latest`. The build context still includes the embedded
`entrypoint.sh`, so a custom Dockerfile must `COPY entrypoint.sh
/usr/local/bin/entrypoint.sh`, create a `braid` user, and set the
entrypoint — the simplest approach is to copy the default and add layers
on top. Run `braid rebuild` after changing the Dockerfile or this path.

### Flags

Flags override config. Per-step flag > per-step config > global flag >
global config > default.

| Flag                   | Description                                               |
|------------------------|-----------------------------------------------------------|
| `--agent claude`       | Global agent selection                                    |
| `--model MODEL`        | Global model                                              |
| `--sandbox agent|docker` | Execution mode                                          |
| `--work-agent AGENT`   | Per-step agent (also `--review-agent`, `--gate-agent`, etc.) |
| `--work-model MODEL`   | Per-step model                                            |
| `--work PROMPT`        | Override the work prompt text                             |
| `--review PROMPT`      | Override the review prompt                                |
| `--gate PROMPT`        | Override the gate prompt                                  |
| `--iterate PROMPT`     | Override the iterate prompt                               |
| `--max-iterations N`   | Cap on review loop iterations                             |
| `--no-wait`            | Fail immediately on rate limits instead of retrying       |
| `--hide-request`       | Don't render the prompt panel in the TUI                  |
| `-y`, `--yes`          | Auto-accept interactive prompts                           |

## Subcommands

| Command          | Description                                                     |
|------------------|-----------------------------------------------------------------|
| `braid init`     | Scaffold `.braid/config.json`, `.braid/docker.json`, `.braid/.gitignore` |
| `braid doctor`   | Verify git, claude, docker, auth, and config                    |
| `braid rebuild`  | Rebuild the sandbox Docker image from the embedded Dockerfile   |
| `braid shell`    | Open an interactive shell inside the sandbox (`--unrestricted` disables firewall) |

## Custom prompts (`BRAID.md`)

If a file named `BRAID.md` exists in the project root, braid uses it as
the template for every agent invocation. The template is Go
[text/template](https://pkg.go.dev/text/template) syntax:

```
Step: **{{.Step}}** | Iteration {{.Iteration}}/{{.MaxIterations}}

### Task
{{.Prompt}}

{{if .LastMessage}}### Previous Output
{{.LastMessage}}
{{end}}

Session log: {{.LogFile}}
```

Available variables: `.Step`, `.Iteration`, `.MaxIterations`, `.Prompt`,
`.LastMessage`, `.LogFile`, `.RalphIteration`, `.MaxRalph`, `.RepeatPass`,
`.MaxRepeatPasses`.

If no `BRAID.md` exists, an embedded default is used.

## Session logs

Every run appends to `.braid/logs/<timestamp>.md` with one entry per
step per iteration:

```markdown
## [work 1] 2026-04-13 14:22:13

<agent output>

---
```

Logs persist across runs until manually cleaned.

## TUI

On a TTY, braid renders a [bubbletea](https://github.com/charmbracelet/bubbletea)
UI with a session banner, phase/iteration header, step indicator with
spinner, scrolling output pane (20-line window), and status lines.
Compositions get a parallel multi-run view. Press `q` or `Ctrl+C` to
quit — composition worktrees and in-flight subprocesses are cleaned up
on exit.

Non-TTY environments (pipes, CI) fall back to colored stderr output.
`BRAID_NO_TUI=1` forces the fallback on a TTY too.

## Status

- **Claude Code agent**: fully supported
- **Codex / OpenCode**: not yet implemented (runner interface ready)
- **Native sandbox**: production-ready
- **Docker sandbox**: functional; image builds on first use or via `braid rebuild`
- **Network isolation**: iptables-based egress restriction to allowlisted
  hosts (requires `CAP_NET_ADMIN`, which is added by default)

## Development

```bash
# Build
go build .

# Full test suite (unit + integration)
go test ./...

# Vet
go vet ./...
```

134 tests across ast, config, retry, template, runner, executor,
gitutil, sandbox, tui, and integration packages.

## License

Check the source; licensing is TBD.
