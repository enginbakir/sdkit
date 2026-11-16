# Architecture

## Scope

Runtime code to port: about **9.3k lines of bash** (the Go result is estimated at 1.5 to 2x that; an estimate, not a measurement).

| Area | Bash lines | Becomes |
|---|---|---|
| `scripts/` (40 files, not `test-select.sh`) | ~6.7k | `internal/*` packages + subcommands |
| `bin/` (`init doctor sync upgrade shared lib sdkit`) | ~1.3k | `sdkit init|doctor|sync|upgrade` |
| `hooks/` (22 files) | ~0.7k | `sdkit hook <name>` |
| `stacks/*/scripts`, `detect.sh` | ~0.7k | `internal/stack` (tables) |
| `adapters/git/*`, `adapters/opencode/plugins/*.js` | small | two-line shims |

Maintainer-only code: `bin/build-adapters.sh` + `bin/emit/*` become `sdkit dev adapters [--check]`; `packaging/scan-publishable.sh` becomes `sdkit dev scan`; `packaging/homebrew/gen-formula.sh` is replaced by the release pipeline; `scripts/test-select.sh`, `tests/*`, `bench/` per `00-decisions.md` D5 and D12.

Data that stays as files and is embedded in the binary: `core/`, `skills/`, `agents/`, `adapters/*/` generated files, `stacks/*/{permissions.txt,rules,known-pitfalls.md,golangci.yml,verify.d.example}`, `templates/`.

## Layout

```
go.mod                      module github.com/wecii/sdkit
kit.go                      package kit: //go:embed core skills agents adapters stacks templates
cmd/sdkit/main.go           dispatch only. Every command lives in cmd/sdkit/<command>.go and registers itself in init()
internal/
  cli/                      flag helpers, usage text, exit-code constants
  envfile/                  sdkit.env parser (data, never executed) and the settings model
  project/                  repo root, project-dir resolver (.sdkit), kit root (`where`)
  store/                    atomic JSON write, lock, JSONL append
  glob/                     the kit's glob, comma lists, protected paths
  gitx/ proc/               git helpers; process helpers (proc_unix.go, proc_windows.go)
  state/ workflow/ sdd/     task state, workflow policy, phase machine, evidence, gate, floor, managed mode
  verdict/ plan/            verifier output parser; dod check, plan parity, write sets, waves
  base/ worktree/ capped/ digest/
  hook/                     framework, host adapters (claude cursor gemini opencode), one file per guard
  guard/                    git-hook and CI guards
  verify/ stack/            verify engine; stack detection, tools, formatter, steps (one file per stack)
  setup/ emit/              init, doctor, sync, upgrade, shared blocks, merge_json, templates; host role emitters
  spec/ tasks/ review/ jira/ pr/
  runrole/ adapter/ route/ contract/ dispatch/ recover/ memory/ knowledge/ lookup/
  usage/ pricing/ telemetry/ report/ audit/ redact/
  testutil/                 temp git repo, fake CLIs, golden compare (test code only)
tools/golden/               capture tool for goldens (maintainer, removed in P5)
testdata/golden/<pkg>/      captured expected outputs
```

Command registration through `init()` in one file per command means parallel tasks never edit the same file (`cmd/sdkit/main.go` is written once, in P0-01).

## Code rules

- One package per script family. A package takes its inputs as parameters. Only `cmd/` and `internal/hook` read stdin, `os.Args` and the environment.
- Settings reach packages as a struct built by `envfile`, never by `os.Getenv` scattered through code. The environment overrides only where the bash did (CI base ref, `SDD_SKIP`, `ALLOW_PROTECTED`, `ALLOW_PROTECTED_PUSH`, `GUARD_SKIP_VERIFY`, `SPEC_KIT_HOME`).
- Output of JSON that is hashed or committed (`audit` chain, `attestations/`, `state.json`) uses structs or an ordered writer, never `map` iteration. Keep jq's key order where a file is committed.
- Hooks: `recover()` at the entry; any error, unreadable JSON or panic ends as exit 0 with no output. A hook blocks only by a deliberate decision.
- Hooks are off by default: an empty setting means exit 0 silently.
- Keep the kit generic: no project, branch, cluster or service names in code, docs or fixtures.
- Never hardcode `.claude/` for project facts; use the resolver. `.claude/` is literal only for Claude Code's own files.
- Never overwrite project-side files in `init` (`project.md`, `sdkit.env` (append only), `rules/`, `settings.json`), not even with `--force`.
- No `os.Exit` outside `cmd/`. Return errors; `cmd/` maps them to the exit codes of `02-contracts.md`.
- Comments say why, not what. Keep a file's header comment (the usage of the command it implements), as the bash files did.

## Command map

| Today | Becomes |
|---|---|
| `scripts/sdd.sh`, `state.sh`, `workflow.sh` | `sdkit sdd ...`, `sdkit state ...`, `sdkit workflow ...` |
| `scripts/guard.sh` | `sdkit guard pre-commit|pre-push` |
| `hooks/<name>.sh`, `hooks/adapt.sh` | `sdkit hook <name>`; `sdkit hook --host cursor|gemini <name>` |
| `verify-core.sh`, `verify-lib.sh`, `stacks/*/scripts/verify.sh` | `sdkit verify [--fast] [--since-head] [--no-cache]` |
| `parse-verdict.sh`, `dod-check.sh`, `resolve-base.sh`, `worktree.sh`, `capped.sh`, `digest.sh` | `sdkit verdict`, `sdkit dod`, `sdkit base`, `sdkit worktree`, `sdkit capped -- <cmd>`, `sdkit digest` |
| `check-plan-parity.sh`, `check-writes.sh`, `plan-waves.sh` | `sdkit plan parity|writes|waves` |
| `open-pr.sh`, `pr-check.sh` | `sdkit pr open|check` |
| `jira.sh`, `adapters.d/jira.sh`, `tasks.sh`, `review.sh` | `sdkit jira`, `sdkit tasks`, `sdkit review` |
| `spec.sh`, `spec-change.sh`, `spec-apply.sh` | `sdkit spec ...` (with `change` and `apply`) |
| `run-role.sh`, `adapter.sh`, `adapters.d/litellm.sh` | `sdkit run-role` (adapters are internal) |
| `route.sh`, `dispatch.sh`, `contract.sh`, `recover.sh` | `sdkit route`, `sdkit dispatch`, `sdkit contract`, `sdkit recover` |
| `memory.sh`, `knowledge.sh`, `lookup-cache.sh` | `sdkit memory`, `sdkit knowledge`, `sdkit lookup` |
| `usage.sh`, `pricing.sh`, `telemetry.sh`, `report.sh`, `audit.sh`, `redact.sh` | same names as subcommands |
| `bin/init.sh`, `doctor.sh`, `sync.sh`, `upgrade.sh`, `bin/sdkit prespec` | `sdkit init|doctor|sync|upgrade|prespec` |
| `bin/build-adapters.sh`, `bin/emit/*` | `sdkit dev adapters [--check]` |
| `packaging/scan-publishable.sh` | `sdkit dev scan [--history]` |
| `bin/install.sh`, `bin/migrate.sh` | dropped (D2, D3) |
| `env.sh`, `lib.sh`, `shared.sh`, `hooks/lib.sh`, `verify-lib.sh` | libraries inside packages |

Every command that a skill, role or doc tells an agent to run becomes `sdkit <cmd>`. There is no `$KIT/scripts/x.sh` path any more, so the same text works in PowerShell, cmd and bash.

## Hooks

`hooks/hooks.json` ends up with one `sdkit hook <event>` entry per event (D4). The dispatcher selects the guards for the tool name and event the way the `matcher` fields do today, runs them, and merges decisions. The matcher table of the current `hooks/hooks.json` is the specification of which guard runs for which tool:

```
SessionStart: session-context, check-tools, value-report
PreToolUse  Agent|Task: guard-budget, check-contract
            Bash: guard-prod, guard-git, guard-pr, guard-noisy
            Read|Edit|Write: guard-secrets
            Edit|Write: guard-phase, guard-protected, frozen-tests
            Bash|Edit|Write|NotebookEdit: guard-readonly
PostToolUse Edit|Write: post-edit-fmt
Stop: stop-verify, usage-log
SubagentStop: record-verdict, usage-log, result-size, check-contract
SessionEnd: value-report
```

Cursor and Gemini call the same entry with `--host`; the translation of host JSON and of the deny protocol is what `hooks/adapt.sh` does today. The OpenCode plugin (`adapters/opencode/plugins/spec-driven-kit-guard.js`) spawns `sdkit hook` instead of `bash`.

## Git hooks

`.githooks/pre-commit` and `pre-push` stay two-line `sh` shims: `exec sdkit guard pre-push "$@"`. Git for Windows runs hooks with its own `sh`, so this needs nothing extra there. If `sdkit` is not on `PATH` the shim prints one line and exits 0 (same as the current "kit not installed, guard skipped").
