# P2 Setup path

Goal: `sdkit init`, `doctor`, `sync`, `upgrade` and `prespec` work on all OSes, for all five hosts, and `sdkit dev adapters` regenerates the committed role files byte for byte. After P2 a Windows user can install the binary, set a repo up and work a gated task in standalone mode.

Depends on: P1 merged. `P0-09` (Windows host spike) must be settled before P2-03: init writes hook wiring for each host, and that wiring depends on how each host runs commands on Windows.

## Waves

| Wave | Tasks (parallel) | Starts when |
|---|---|---|
| 1 | P2-01, P2-02, P2-07 | P1 merged |
| 2 | P2-03, P2-04, P2-05 | P2-01 and P2-02 merged (P2-04 and P2-05 need only P2-01) |
| 3 | P2-06 | wave 2 merged |

## Exit criteria

- Scenario E passes. `sdkit dev adapters --check` reports no drift against the committed `agents/` and `adapters/*/` files.
- In a scratch `git init` repo: `sdkit init --host claude,codex,opencode,cursor,gemini --stack go`, then `sdkit doctor` shows only the expected FAILs (project.md TODOs, base branch without a commit), then a gated task runs in standalone mode.
- `tests/windows-smoke.ps1` exists and the Windows CI job runs it (the first real run needs the user to push).
- Test code of the phase within budget (~150 lines including scenario E and the smoke script).

---

## P2-01 setup library {#p2-01}

- **Wave:** 1 · **Depends:** P1 · **Size:** M
- **Source:** `bin/lib.sh` (`say`, `install_managed`, `install_template`, host detection incl. the Codex 0.3 layout note, resolver helpers), `bin/shared.sh` (`install_shared`, `merge_json`, the marked `spec-driven-kit:begin ... end` blocks, `.git/info/exclude` handling), `templates/blocks/*`.
- **Go:** `internal/setup/{lib,shared,jsonmerge,exclude}.go`.
- **writes:** `internal/setup/lib.go`, `shared.go`, `jsonmerge.go`, `exclude.go`.
- **Notes:** the kit rewrites only its marked block or its own keys in shared files (`AGENTS.md`, `.claude/CLAUDE.md`, `.codex/config.toml`, `.cursor/hooks.json`, `.gemini/settings.json`, `settings.json`). `merge_json` is idempotent and never drops a user key. Project-owned files are never overwritten, not even with `--force` (`02-contracts.md`). Files the kit generates outside `.sdkit/` are hidden per clone through `.git/info/exclude` (manage a marked section there).
- **Delete:** `bin/lib.sh`, `bin/shared.sh`.
- **Goldens (≤ 2):** a merged `settings.json` from a user file with extra keys; a re-run of `install_shared` on its own output (no diff).
- **Tests (≤ 15 lines):** `merge_json` table: idempotent, keeps user keys, adds missing, arrays dedupe; block replace keeps text outside the markers.
- **Done when:** goldens match.

## P2-02 emitters and `sdkit dev adapters` {#p2-02}

- **Wave:** 1 · **Depends:** P1 (needs only P0 in practice) · **Size:** M
- **Source:** `bin/build-adapters.sh`, `bin/emit/{claude,codex,cursor,gemini,opencode}.sh`, `core/roles/*.md` (front matter and body), `core/adapters.json`, `core/hosts.env`, `core/tiers.env`.
- **Go:** `internal/emit/` (role parser, one emitter per host: Claude `agents/*.md`, Codex `adapters/codex/agents/*.toml` + `config.toml.snippet`, Cursor, Gemini, OpenCode), `cmd/sdkit/dev.go` (`sdkit dev adapters [--check]`).
- **writes:** `internal/emit/`, `cmd/sdkit/dev.go`.
- **Notes:** **the committed generated files are the oracle**: the Go output must be byte-identical (`--check` compares the tree and prints the first difference). Roles declare a cost `tier` (`light|standard|strong`), never a model name; `core/tiers.env` maps tiers to models per host. Read-only roles (`verifier`, `reviewer`, `security-auditor`, `spec-reviewer`) get the host's read-only mechanism. The Codex emitter refuses a role body that contains `'''`. The `orchestrator` is not an agent file.
- **Delete:** `bin/build-adapters.sh`, `bin/emit/`.
- **Tests:** none; `sdkit dev adapters --check` is the gate (run it on every merge from here on).
- **Done when:** `--check` is clean on the committed tree.

## P2-03 init {#p2-03}

- **Wave:** 2 · **Depends:** P2-01, P2-02, P1-08 to P1-10 · **Size:** L
- **Source:** `bin/init.sh` (all flags: `--install-openspec`, `--ci bitbucket|github|gitlab`, `--host`, `--dir`, `--mode plugin|copy`, `--stack`, `--env analyst|developer`, `--no-memory`, `--memory-vcs`, `--force`), `templates/*`, `templates/ci/*`, `stacks/*/{permissions.txt,rules,known-pitfalls.md,verify.d.example}`.
- **Go:** `internal/setup/init.go`, `internal/setup/ci.go`, `cmd/sdkit/init.go`.
- **writes:** `internal/setup/init.go`, `internal/setup/ci.go`, `cmd/sdkit/init.go`.
- **Notes:**
  - Writes `<project-dir>` (`project.md`, `sdkit.env`, `sdkit.local.env` with every key and default, `rules/`, `verify.d/`, `memory/`, `.gitignore`), host files per host, `.githooks/` shims, `AGENTS.md` block, the `.claude/CLAUDE.md` block, stack permissions and rules, and the CI file for `--ci`.
  - Stack detection from the repo (table in `README.md`); one stack per repo; `generic` is the fallback; in an empty repo `--stack` is required (as today).
  - `--env analyst` (specs repo, no stack, pre-spec dir git-ignored) and `--env developer` (source repo, `specs/` submodule hint).
  - **Mode `copy`**: copies agents and skills into `.claude/` and wires hooks to `sdkit hook`; no scripts are copied any more (there are none). **Mode `plugin`**: project files only.
  - Hook wiring per host uses the findings of `P0-09` (shell, quoting, `sdkit` on `PATH`). Where a host needs a wrapper on Windows, emit it from the same code path with `runtime.GOOS`.
  - Never overwrite project-owned files; `sdkit.env` only gets new settings appended.
  - `--install-openspec` runs `npm install -g @fission-ai/openspec` through `proc`; without it, `doctor` reports a missing `openspec`.
  - Git hooks: `git config core.hooksPath .githooks` is done by the user (README), not by `init` unless the bash did it; check `init.sh`.
- **Delete:** `bin/init.sh`.
- **Goldens (≤ 3):** the file list produced for `--host claude`, for `--host codex`, and for `--env analyst` (names only, not contents).
- **Tests (≤ 15 lines):** stack detection table (fixture file lists → stack id).
- **Done when:** scenario E (P2-06) passes for init.

## P2-04 doctor {#p2-04}

- **Wave:** 2 · **Depends:** P2-01 · **Size:** M
- **Source:** `bin/doctor.sh` (`FAIL`/`warn` checks, `--update-pricing`, the outdated Expo skill check), `hooks/check-tools.sh` logic shared with P1-13.
- **Go:** `internal/setup/doctor.go`, `cmd/sdkit/doctor.go`.
- **writes:** those two files.
- **Notes:** exit 0 when there is no `FAIL`, 1 otherwise. Missing-settings listing, rubric in `project.md` present (**mandatory**), `sdkit.env` keys, `verify.d` policies enabled, git hooks path, host files present and in sync with `sdkit.local` state, plugin vs binary version, `openspec` present, `gh`/`glab` when `PR_HOST` needs them, no `jq`/`bash` checks any more. `--update-pricing` delegates to `pricing` when P4 exists, otherwise prints that it is not available.
- **Delete:** `bin/doctor.sh`.
- **Tests (≤ 10 lines):** one table of fixture states → FAIL/warn set.
- **Done when:** the expected FAILs for a fresh scratch repo match the bash output (golden of the severity list).

## P2-05 sync and upgrade {#p2-05}

- **Wave:** 2 · **Depends:** P2-01 · **Size:** S
- **Source:** `bin/sync.sh`, `bin/upgrade.sh`, `docs/upgrading.md`.
- **Go:** `internal/setup/sync.go`, `internal/setup/upgrade.go`, `cmd/sdkit/{sync,upgrade}.go`.
- **writes:** those files.
- **Notes:** `sync [--check]` refreshes the kit's files and reports drift without clobbering local edits. `upgrade` adds what is new and never touches project-owned files. **New for 2.0**: `upgrade` also removes what 1.x installed and 2.0 no longer has (copied `.sdkit/scripts`, `.sdkit/hooks`, old kit-owned shims) and rewrites the git-hook shims; it refuses a pre-1.0 layout and says to run the last 1.x `upgrade --migrate` first (D2).
- **Delete:** `bin/sync.sh`, `bin/upgrade.sh`.
- **Tests (≤ 10 lines):** `upgrade` on a 1.x-shaped fixture removes only kit-owned files.
- **Done when:** scenario E covers it.

## P2-06 scenario E and Windows smoke {#p2-06}

- **Wave:** 3 · **Depends:** P2-03, P2-04, P2-05 · **Size:** S
- **writes:** `internal/scenario/scenario_setup_test.go`, `tests/windows-smoke.ps1`, `.github/workflows/go.yml` (add the smoke step), and **delete** `.github/workflows/spike-windows.yml`.
- **Do:** scenario E as defined in `04-testing.md`. `windows-smoke.ps1` (about 40 lines, CRLF): make a scratch repo, run `sdkit init --stack go`, `sdkit sdd start`, check that an edit before RED is refused (gate exit 1), pipe a hook JSON into `sdkit hook` and check the deny JSON. Fail the job on any difference.
- **Tests (≤ 100 lines):** scenario E ≈ 60, smoke ≈ 40.
- **Done when:** scenario E passes on macOS; the smoke script is syntactically valid and its Windows run is queued for the user's first push.

## P2-07 prespec {#p2-07}

- **Wave:** 1 · **Depends:** P1 · **Size:** S
- **Source:** `prespec_cmd` in `bin/sdkit`, `templates/prespec/README.md`, `skills/generate-specs` (how it is called).
- **Go:** `internal/setup/prespec.go`, `cmd/sdkit/prespec.go` (`sdkit prespec check <name>`).
- **writes:** `internal/setup/prespec.go`, `cmd/sdkit/prespec.go`.
- **Notes:** validation only, nothing is created or changed. `<PRESPEC_DIR>/<name>.md` (or a `<name>/` directory holding markdown) exists, is not empty, is git-ignored and is **not tracked**. Name is a plain name: no path, no `..`, no leading dot. Exit 0 valid, 1 invalid (says why), 2 usage.
- **Delete:** nothing (`bin/sdkit` is deleted in P5-02).
- **Tests (≤ 5 lines):** tracked file and non-ignored file are refused.
- **Done when:** the messages match the bash for the four failure cases (golden).
