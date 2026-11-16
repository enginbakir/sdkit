# Contracts that must not change

The port keeps these byte- or behaviour-compatible. `P0-07` turns this list into `docs/go-port/contracts-inventory.md` with the exact strings, found by searching `skills/`, `core/`, `docs/`, `templates/` and `hooks/` for quoted or parsed output. A worker that must change one stops and reports it (`README.md`, "When to stop").

## Files and layout

- `<project-dir>` is `.sdkit/`. The resolver tries `$KIT_DIR` then `.sdkit` with `sdkit.env`. (Pre-1.0 directories are dropped, D2.)
- Settings: `sdkit.env` then `sdkit.local.env` (the second wins). **Parsed as data, never executed.** Accepted line forms: `KEY=value`, `KEY="value"`, `KEY='value'`, `export KEY=value`, `: "${KEY:=default}"`, `# comment`, blank. A value may use `$NAME` and `${NAME}` of a setting read earlier; nothing else is expanded. Any other line is ignored and reported on stderr as `file:line: ignored (unsupported syntax)` without its content; `SDKIT_ENV_QUIET=1` silences it. Keys that would change how a shell runs (`PATH`, `IFS`, `BASH_ENV`, `LD_*`, ...) are refused. Read `scripts/env.sh` for the exact list. Tolerate CRLF and a UTF-8 BOM.
- Default stack when `STACK` is empty: `go`.
- State: `<project-dir>/.sdd/state.json` and the sibling files the bash writes; `.frozen`; `attestations/` (committed). Field names and phase names are unchanged: `intake proposed red green verified pr done` (plus `aborted`).
- Ownership (README "Who Edits What"): `init` never overwrites `project.md`, `sdkit.env` (appends new settings only), `rules/`, `settings.json`, `verify.d/`, `memory/`, `roles.d/`. The kit owns only its marked `spec-driven-kit:begin ... end` blocks and its own keys in shared files.
- Generated host files are hidden per clone in `.git/info/exclude`; `AGENTS.md` and the kit block in `.claude/CLAUDE.md` are committed.

## `verify.d` policies (user code in the field)

Policies in `<project-dir>/verify.d/` run in name order after the stack's built-in steps; names starting with `_` are skipped and `*.example` is ignored. They receive these environment variables, with unchanged names and formats (see `scripts/verify-lib.sh`): `VERIFY_ROOT`, `VERIFY_MERGE_BASE`, `VERIFY_CHANGED`, `VERIFY_CHANGED_GO`, `VERIFY_CHANGED_SRC`. `VERIFY_POLICY_DIR` overrides the directory. A policy's non-zero exit fails verification. Other commands that user settings hold (`VERIFY_CMD`, `DIFF_BASE_CMD`, `--test-cmd`) are strings run by the shell of `03-cross-platform.md` item 2.

## Hook protocol

- In: the tool call as JSON on stdin: `tool_name`, `tool_input.file_path`, `tool_input.command`, `agent_type`, `hook_event_name`.
- Out (Claude Code): `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow|ask|deny","permissionDecisionReason":"..."}}`. Exit 0 with no output means allow.
- Other hosts: the per-host protocol of `hooks/adapt.sh` (Gemini: deny is exit 2 with the reason on stderr; Cursor: `{"permission":...}`, `ask` only where Cursor supports it, otherwise a deny whose reason says an explicit approval is needed). OpenCode: a deny or ask becomes a thrown error in the plugin.
- Env the hooks honour: `CLAUDE_PROJECT_DIR`, `CLAUDE_PLUGIN_ROOT` (plugin mode only), `CURSOR_PROJECT_DIR`, `GEMINI_PROJECT_DIR`, `SDKIT_HOST`.
- Other outputs: SessionStart `hookSpecificOutput` with `additionalContext`, `{"systemMessage":"..."}`, and the Stop hook's `{"decision":"block","reason":"..."}`.
- A hook never blocks because of its own error.

## Phase gate

- `sdkit sdd gate edit <path>` and `gate push`: exit 0 allowed; exit 1 with the reason **on stdout** when out of phase. `SDD_MODE`: `strict` (default) denies, `guided` asks or warns, `off` disables. `SDD_SKIP=1` bypasses once in git hooks.
- A refused `advance` leaves the state untouched, prints why **on stderr** (`sdd: ...`; `gate` refusals go to stdout), and records a `phase.reject` event when telemetry is on. Two advances at once: one wins.
- `risk` can only be raised. High risk needs a PASS from each of verifier, reviewer and security-auditor from a **registered** run (`trust=spawned`), on this diff, for this task. Hand-recorded evidence never satisfies that gate.
- Edit after `verified` makes the verdict stale. Push and PR are refused until verified again.
- `_spawn`, `_evidence`, `_spawn-close`, `_verdict` are for hooks and `run-role` only. `guard-pr` refuses them from the model's shell.

## Text that skills, roles and docs read or quote

- Verifier output, exact: `DOD / FINDINGS / VERIFY / VERDICT`.
- `sdkit sdd status` prints `task=<slug> risk=<r> phase=<p>`, followed by the optional fields ` profile=`, ` verdict=`, ` evidence=stale`, ` ticket= spec=`, ` blocked=` when they apply.
- Step lines: `[sdkit] orchestrator · Step <n>/<total>: <name> — started|done` (total 7 for low, 8 for medium and high). Analyst skills: `[sdkit] <skill> - Step <n>/<total>: <name> - started|done` (plain hyphens, total 6 or 8).
- The evidence table of `sdd evidence`; `sdd transitions`; `sdd policy [--json]`; `sdkit where` prints the kit location (in Go: the install location; the embedded data is not a directory, see `06-release.md`).
- Messages that a skill quotes to the user (find them in P0-07).

## Exit codes

- `sdd gate`: 0 allowed, 1 refused. `sdd` evidence paths: 4 with `no active task`.
- `run-role`: 0 ran, 2 refused by policy or config, 3 budget, 4 and 5 contract, 6 the evidence role's answer could not be recorded, **7 valid evidence for this diff already exists, nothing was run (success-like, no retry)**, **8 routing declined and no valid result exists (never a pass)**.
- `sdkit doctor`: 0 when no FAIL (warnings allowed), 1 otherwise.
- Usage errors: 2.

## Allowed to change in 2.0

Command paths (`$KIT/scripts/x.sh` becomes `sdkit x`), `sdkit install`, `sdkit upgrade --migrate` (dropped), wording of messages that no skill or doc quotes, the attestation fingerprint algorithm only with a note in `docs/upgrading.md` (D11). Everything else here is frozen.
