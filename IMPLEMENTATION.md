# Implementation Summary

Braid is a Go CLI that orchestrates AI code agents through composable
operators: review loops, parallel races, A/B comparisons, and multi-task
progression. This document captures what was built, how it was factored,
and the key design decisions.

## Outcome

- **134 tests** across 9 packages, all passing; `go vet ./...` clean
- **Zero external runtime deps** beyond cobra + bubbletea + lipgloss + go-isatty
- **Single static Go binary** — no vendored Docker SDK (shells out to `docker` CLI)
- Full operator set: `work | review | xN/repeat | ralph | vN/race | vs` with `pick / merge / compare` resolvers
- Both execution modes working: native (subprocess) and Docker (sandboxed)
- Subcommands: `init`, `doctor`, `rebuild`, `shell`

## Architecture

```
braid/
├── main.go               # entry point, routes argv to cobra or runDefault
├── cmd/                  # cobra subcommands (init, run, doctor, rebuild, shell)
├── internal/
│   ├── ast/              # AST types + parser (positional CLI → tree)
│   ├── config/           # .braid/config.json, .braid/docker.json, agent types
│   ├── executor/         # recursive AST walker; work/repeat/review/ralph/composition handlers
│   ├── runner/           # AgentRunner interface; native subprocess impl; pool
│   ├── sandbox/          # Docker runner, embedded Dockerfile, entrypoint script
│   ├── template/         # BRAID.md rendering via Go text/template
│   ├── retry/            # rate-limit detection + fixed-interval retry loop
│   ├── gitutil/          # git worktree management, repo sanity checks
│   ├── log/              # colored stderr logging + session log files
│   └── tui/              # typed event channel, bubbletea AppModel + RaceModel
└── test/integration/     # end-to-end tests that compile+run the real binary
```

## Phase-by-phase

### Phase 1 — Scaffold + Parser + Config
Recursive-descent parser for the positional CLI language: reserved keyword
detection (`review`, `ralph`, `race`, `repeat`, `vs`, `pick`, `merge`,
`compare`), `xN`/`vN` regex operators, implicit review slotting,
`vs` branch splitting with trailing resolver, second-level compositions.
AST is a sealed Go interface with 5 concrete node types; composition
branches are deep-cloned so mutations stay local. Config loader merges
user overrides with built-in defaults, with a warning path for legacy
`sandbox: "none"` settings.

### Phase 2 — Runner, Template, Logging, Retry
`AgentRunner` interface uses `context.Context` for cancellation.
`NativeRunner` spawns agents as subprocesses, pipes the prompt to stdin,
streams stdout via a `LineBuffer` callback, and does a SIGTERM→5s→SIGKILL
shutdown. `Pool` caches one runner per sandbox mode with lazy
initialization. Template engine uses Go's `text/template` with a
parsed-template cache (keyed by source). Session logs written to
`.braid/logs/<timestamp>.md` in markdown with one entry per step per
iteration. Retry loop uses Go generics (`Do[T]`) for typed returns
without `any` boxing, and respects `ctx.Done()` for cancellation.

### Phase 3 — Executor Core (Work + Repeat + Review)
`ExecutionContext` threads per-run state (config, flags, step config,
last message, repeat/ralph counters) through the recursive walker.
`ResolveStepConfig` implements the precedence chain: per-step flag >
per-step config > global flag > global config > default, with
`iterate → work` and `ralph → gate` fallbacks. `ParseGateVerdict`
scans output line-by-line for `DONE/PASS/COMPLETE/APPROVE/ACCEPT` or
`ITERATE/REVISE/RETRY`, defaulting to ITERATE on ambiguity. The review
loop skips the work step when inner nodes already produced output
(`skipFirstWork`). Signal handling wires SIGINT/SIGTERM into
`context.Context` via `signal.NotifyContext`, with a goroutine that
stops the pool on cancel so in-flight subprocesses die immediately.

### Phase 4 — TUI
Typed event channel (`tui.Event` sealed interface with 10 concrete event
types) carries executor state to the renderer. `Emitter` wrapper does
non-blocking sends with nil-channel safety — handlers write `ec.emit(ev)`
without guards, and tests pass `nil` to skip TUI. Bubbletea `AppModel`
renders a banner, phase/iteration header, step indicator with spinner,
streaming output pane (20-line scrollback), and a status footer. When
`--show-request` is active, the rendered prompt appears in a folded
panel above the output (bounded to 8 lines). `RaceModel` handles
multi-run parallel display, demultiplexing events by `RunIndex`. Non-TTY
destinations fall back to plain colored stderr via `runWithLogging`;
`BRAID_NO_TUI=1` forces the fallback even on TTY (used by integration
tests).

### Phase 5 — Ralph + Composition
`gitutil` package: worktree create/remove, `FindProjectRoot`,
`SessionID`, clean-tree detection, diff/status/add/commit/merge helpers.
Ralph handler: outer task loop running the inner node then the ralph
gate, `NEXT` continues, `DONE` stops; if the inner review loop exhausts
its iterations ralph stops early (not converging). Composition handler:
verifies git state, creates per-branch worktrees at
`.braid/race/<session>/run-N`, launches per-branch goroutines with
tagged event forwarders and per-branch runner pools rooted at the
worktree. On SIGINT mid-flight, cleanup runs before returning (no
orphaned worktrees). Resolvers: `pick` (judge agent parses `PICK N`
regex, git merges winner), `merge` (synthesis worktree runs review loop
reading `MERGE_CONTEXT.md`, commits, merges back), `compare` (produces
markdown doc at `.braid/compare-<session>.md`, preserves worktrees for
manual inspection).

### Phase 6 — Docker Sandbox
Embedded Dockerfile (`node:20-slim` + Claude Code CLI + iptables +
gosu) and entrypoint script embedded via `//go:embed`. Entrypoint
applies iptables egress rules when `BRAID_NETWORK_RESTRICTED=1`
(loopback + ESTABLISHED + DNS + HTTPS to resolved allowlist IPs,
default DROP), remaps the `braid` user to host UID/GID so bind-mounted
files get correct ownership, then drops privileges via `gosu`. Fails
open without `CAP_NET_ADMIN` rather than crashing — `braid doctor`
catches the misconfig instead. `DockerRunner` implements
`AgentRunner` by wrapping `docker run` with the same stdin/stdout
streaming as the native runner. Three new subcommands: `rebuild` (force
image rebuild), `shell` (interactive TTY or one-shot command in
sandbox, with `--unrestricted` to disable firewall), `doctor` (checks
git, claude CLI + version, docker daemon, sandbox image, Claude auth,
config parseability).

### Phase 7 — Polish + Testing
End-to-end integration test suite compiles the real binary and runs 9
scenarios against scratch git repos with a mock `claude` on PATH.
Ralph handler gets 4 dedicated tests. `PromptEvent` wired into the
TUI. Dead code removed (unused cleanup Registry, hand-rolled path
basename). Composition hardened with `ctx.Err()` check after
`wg.Wait()` — SIGINT leaves a clean state. Doctor prints `claude
--version`. Root help reorganized with OPERATORS and EXAMPLES
sections. `NativeRunner` pre-checks `exec.LookPath(cmd)` and fails
fast with an actionable error instead of letting `exec: file not
found` bubble up through retry loops.

## Key design choices

- **Typed event channels** instead of a singleton event bus: each
  executor goroutine gets its own `chan tui.Event`, which eliminates
  global state and lets compositions fan out per-branch channels that
  get multiplexed by the race renderer.
- **`context.Context` everywhere** for cancellation: SIGINT cascades
  through the signal handler → root context → all branch goroutines →
  all in-flight subprocesses. No manual cleanup registry needed; `defer`
  plus context checks handle the tree.
- **Go generics for retry**: `retry.Do[T]()` returns typed values
  directly without `any` boxing at the call site.
- **Shell out to `docker`**: the official Docker SDK pulls in hundreds
  of transitive deps. The CLI is stable, widely available, and keeps
  the binary lean. It also lets users swap in `podman` via alias.
- **Go `text/template` for BRAID.md**: safer than `eval`-style template
  literal compilation, cached by source, and idiomatic for Go users.

## What isn't built

- **Codex and OpenCode agents.** Only Claude Code is wired up; the
  runner interface is ready to accept more but the command-building
  switch returns "not yet supported" for the others. Adding them is
  additive (~30 lines per agent in `runner/native.go` and
  `sandbox/sandbox.go`).
- **Interactive confirm prompts** for resolver merges — braid assumes
  `--yes` behavior (merges automatically). A TTY confirm would slot in
  at `applyWinnerAndCleanup` in `executor/resolver.go`.
- **Animation style options.** Braid ships with a single braille
  spinner. The config field is still read — it just isn't used to
  select an animation yet.
- **Per-project Dockerfile customization.** The embedded Dockerfile is
  fixed. Users wanting custom base images would need to patch the
  binary or shell out via `braid shell`.
