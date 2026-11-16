# SDKit Roadmap Progress

> **History.** This record was written for the 1.x (bash) kit. Commands are shown by their 2.0 names (`sdkit <command>`); the `tests/*.test.sh` suites, `bench/` and `jq` it names belong to 1.x (2.0 replaced the suites with a small Go test set, and the harness lives in `attic/bench/`, unmaintained). The design and the decisions still stand; see [guide.md](guide.md) for how 2.0 is built.

Last updated: 2026-10-31
Current: F12–F20 implemented locally; validation: see the last section
Source: `Implementation Roadmap.md` (§0 rules apply: no commits, no version bumps, no paid benchmarks, gates unchanged).

| Feature | Status | Evidence | Focused tests | Known blockers |
| --- | --- | --- | --- | --- |
| F07 usage & cost | DONE (documented limits) | `docs/design-usage.md` §6, `sdkit usage`, `sdkit hook usage-log` | `tests/usage.test.sh` 113 passed | hook payloads not exercised in a live Claude Code run; codex/opencode/cursor report no tokens; cache-write is one rate (no 5m/1h split) |
| F08 agent & verification efficiency | DONE (fixtures) | `sdkit verdict` (normalise, `[format]`/`[semantic]`, evidence-less PASS refused), `sdkit dod` (early DoD check, wired in `sdkit run-role`), telemetry `efficiency` block | `tests/efficiency.test.sh` 33 passed | runtime effect on live cost NOT MEASURED |
| F09 context optimisation | DONE (opt-in) | `sdkit digest` (bounded output, ENVELOPE/1), `sdkit run-role --envelope` / `RESULT_ENVELOPE=1`, `context.jsonl` stats | `tests/context.test.sh` | default stays off until a comparable live trace shows no quality loss; reduction figures are fixture bytes, not tokens |
| F10 test & execution efficiency | DONE | test selection (changed path → focused suites; cut from 2.0, `go test ./...` runs whole) | `tests/selection.test.sh` | map covers the kit's own suites; a consuming project writes its own `<project-dir>/test-map.conf` |
| F11 cost benchmark | PARTIAL (fixture baseline only) | `bench/costbase.sh`, `bench/baselines/f11-fixture-final-map.costbase.json`: format-only reruns 6 of 8 without normalisation, 0 with; 0 semantic cases wrongly accepted; fixture output 8914 -> 339 chars, role answer 4447 -> 509 chars; mean 2.2 of 18 suites per small patch | `tests/costbase.test.sh` 9 passed | cost per accepted task, SDKit on/off, prefix-cache/TTL experiments need live paid runs (T4, not authorised) |
| F12 provider-agnostic execution | DONE (mock) | `sdkit adapter`, `sdkit run-role --host ext:<name>`, `docs/design-adapters.md` | `tests/adapter.test.sh` 16 | Claude path unchanged (dry-run command line asserted); cancel/status unsupported for built-ins, reported as such; no real second provider exercised |
| F13 LiteLLM / FusionCLI | PARTIAL | the built-in LiteLLM adapter (`internal/adapter`) (OpenAI-compatible `/chat/completions`, text-only, egress opt-in, allow-list, key via stdin) | `tests/litellm.test.sh` 18 (fake curl) | never run against a live endpoint; **no FusionCLI adapter** (its CLI surface could not be verified here) |
| F14 cost-aware routing | DONE (opt-in) | `sdkit route`: `ROUTE_COST_ENFORCE=hard`, task/role/project limits, reservations + reconcile, unknown = stop | `tests/costroute.test.sh` 22 | money is an estimate from the user price table; "historical quality" is not a routing input (no measured quality signal exists) |
| F15 parallel orchestration | DONE (opt-in) | `sdkit dispatch` (deps, write overlap, MAX_PARALLEL, idempotent claim, stale-evidence reminder) on top of `sdkit plan waves`/`sdkit worktree` | `tests/dispatch.test.sh` 16 | wall-time and token cost of parallel runs NOT benchmarked |
| F16 knowledge reuse | DONE | `sdkit knowledge` (sha256-bound notes, UNTRUSTED, refused to independent roles) | `tests/knowledge.test.sh` 18 | saving not measured on live work |
| F17 observability | DONE | `sdkit telemetry trace` (ids, roles, numbers, reason codes; no content) | `tests/trace.test.sh` 9 | no external exporter |
| F18 recovery | DONE | `sdkit recover scan|apply` (closes stale handoffs/spawns/claims; never re-runs, never touches BLOCK) | `tests/recover.test.sh` 16 | automatic resume of work is deliberately not done: the existing bounded retry rules decide |
| F19 security | DONE | `sdkit redact`, `sdkit audit` (hash-chained trail, `policy` static checks), audited egress | `tests/security.test.sh` 24 | redaction is a filter, not a guarantee; audit tail can be truncated by a writer; no critical issue found in this pass; hook payloads not exercised live |
| F20 CI / evaluation | DONE | `sdkit pr check` (T0+T1, `--milestone` T3), `bench/history.sh` regression history, `templates/ci/pr-check.yml` (example, not installed) | `tests/ci.test.sh` 14 | history is fixtures only; real cost/quality regression needs live runs |

## Validation (2026-11-01)
- T1 for F08–F11 (selected by `go test ./...`): 13 commands, 0 failed, 1148 s.
- T3 full suite after F12–F20 (`go test ./... run --tier T3`, 28 commands, 1460 s): 27 passed, 1 failed first (`tests/budget.test.sh`: it expected `cache hit rate: 80%`, F07's report prints one decimal `80.0%`; the test expectation was stale, not the code). Fixed the expectation, re-ran that suite alone: 45 passed, 0 failed. The other 27 were not re-run after that one-line test change (they do not exercise it).
- Also clean: `bash -n` on every script/hook/test, `jq` on changed JSON, `sdkit dev adapters --check`, `sdkit audit policy`.
- Not done, by rule: commits, version bumps, tags, paid/live benchmarks, live Claude hook payloads, live LiteLLM endpoint, FusionCLI adapter.

## Stabilization (2026-11-01)
- P0: `init.sh`/`sdkit doctor` now install/check all runtime scripts and the `adapters.d/` copy (`tests/install.test.sh`: codex, all hosts, claude copy; a missing `sdkit dod` makes the verifier refuse). `.gitignore` excludes machine-specific files.
- P1: hard reservation > 0 and strict numbers; one price resolver; reservation lifetime (exit/signal/TTL/recover); project lock; child-session marker (`tests/reservation.test.sh`).
- P2: placeholder/final reconcile (sdkit usage), `sdkit dod accept`, test-map gates (`@always`), cache key, deleted test scripts, docs.
- Not verified live: Claude Code hook payloads, a real `claude -p` child session, provider model-id prices.
