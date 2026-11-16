# Testing: rewritten, very small

Today: about 3.9k lines in 40 shell suites, plus `tests/old/`. They are **not ported**. The new suite is Go, runs with `go test ./...`, needs no Docker, no `jq`, no bash, finishes in **under 20 seconds**, and runs unchanged on macOS, Linux and Windows.

**Budget: about 1,200 lines of test code in total, plus at most 150 lines of helpers in `internal/testutil`** (set on purpose: roughly a third of today's size). Every task card carries its own line budget, and the cards add up to this total (unit tables ~600, five scenarios ~350, Windows smoke ~40, rest in the cards). Going over a card's budget is a "stop and ask".

## Principle

Test **decisions**, not plumbing. A test exists only where a mistake would silently weaken enforcement or corrupt a committed or user-owned file. Everything else is covered by `go vet`, the compiler, and the scenarios.

## Allowed shapes

| Shape | Use for | Rule |
|---|---|---|
| **Table test** | pure decision logic: gate matrix, transitions, risk floor, evidence validity, verdict parse, guard regexes and bypass attempts, glob, `envfile` | one table per package, 40 to 150 lines, rows are `{name, input, want}`. A new bug is a new row, not a new file |
| **Golden** | output that skills or docs quote, captured from the bash before it is deleted | `testdata/golden/<pkg>/<case>.txt` (command, stdin, stdout, exit code). Compared by `internal/testutil/golden`. At most 25 files in total. Captured with `tools/golden` (`05-workflow.md` §3) |
| **Scenario** | a real flow in a temp git repo with the real binary (`go test` builds it once, `os/exec`), fake `gh`/`glab`/Jira through `httptest` and stub executables | exactly five (below) |
| **Windows smoke** | proves the Windows binary starts and enforces | one PowerShell script, ~40 lines, run by the Windows CI job only |

## The five scenarios (the whole integration suite)

| Id | What it proves |
|---|---|
| **A** | Standalone, low risk, `init` to `done`: start, DoD, failing test (RED), implementation (GREEN), verify command, PR step, merged check. A code edit before RED is refused. |
| **B** | Medium and high risk: independent evidence required, stale verdict after an edit, push and PR refused, high needs three registered roles, hand-recorded evidence does not satisfy it, risk can only be raised, the CI gate runs the base's rules (`06-release.md`). |
| **C** | Hook JSON in and out for the four hosts (claude, cursor, gemini, opencode): allow, ask, deny, unknown tool, malformed input, panic path exits 0 silently. |
| **D** | Managed mode with a fake Jira: contract validation, pinned spec revision, `block --spec-change`, refusal of every `advance` and edit while blocked, `revalidate`, `evidence-sync` idempotent. |
| **E** | `init`, `sync`, `upgrade` never overwrite project-owned files; settings are only appended; `doctor` reports the right FAILs; per-host files for all five hosts are generated. |

## What is deliberately not tested

Usage and cost arithmetic beyond one table, report layout, telemetry schema, template text, documentation counts, adapter file contents (`sdkit dev adapters --check` compares generated files with the committed ones, byte for byte), flag parsing, help text, one test per message. Benchmarks (`bench/`) are out of scope (D5).

## Quality gates (run by the orchestrator on every merge, and in CI)

- `go build ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, `GOOS=linux go vet ./...`
- `go test ./...` (and `-race` in CI)
- `sdkit dev adapters --check` once it exists
- `gofmt -l .` prints nothing

CI (P0-06): matrix `ubuntu-latest`, `macos-latest`, `windows-latest` for build, vet and test; the Windows job also runs the smoke script once P2 lands.

## Helpers in `internal/testutil` (test code only)

`TempRepo(t)` (a `git init` repo with a first commit and a project dir), `RunKit(t, repo, stdin, args...)` (runs the built binary and returns stdout, stderr, exit code), `Golden(t, name, got)`, `FakeCLI(t, name, script)` (an executable stub named `gh`, `glab`, `claude`... that records its arguments; on Windows a `.cmd`), `FakeJira(t)` (an `httptest` server). Keep them small; a helper over 60 lines needs a reason.
