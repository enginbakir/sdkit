# P4 Cost and measurement

Goal: the optional cost, routing and measurement features: price table, usage ledger, telemetry, the Value Report, routing decisions, bounded views, caches, and the hooks that feed them. Everything here is **off by default** and must keep its honesty rule: a missing record prints `N/A`, never a guess; every figure is labelled actual, estimated or unavailable; nothing leaves the machine and no prompt is logged.

**Before P4 starts**, resolve D7: the untracked `Implementation Roadmap.md` plans F07 to F20, and which items are finished in the repo was not verified. For each feature below, check the repo state (what exists in `scripts/` and `tests/`), port what exists, and never implement a missing feature in bash. Features that exist only on paper are listed in `STATUS.md` as "not in 2.0".

Depends on: P1 and P3-08 merged. Bash is read from the oracle worktree.

## Waves

| Wave | Tasks (parallel) | Starts when |
|---|---|---|
| 1 | P4-01, P4-02, P4-03 | P3 merged and D7 resolved |
| 2 | P4-04 | P4-01 merged |
| 3 | P4-05, P4-06, P4-07 | P4-02, P4-04 merged |
| 4 | P4-08 | P4-05 merged |

## Exit criteria

- One small table test per package; a ledger with missing records produces `N/A`, never a number.
- With `ROUTING=on`, `run-role` returns exit 7 when valid evidence for this diff exists and 8 when routing declines and no valid result exists; with routing off the behaviour is the P3-08 one.
- A budget stop happens **before** the spawn that would exceed the limit; verification is never shortened by a budget.
- Test code of the phase within budget (~70 lines).

---

## P4-01 pricing {#p4-01}

- **Wave:** 1 · **Depends:** P3 · **Size:** S
- **Source:** `scripts/pricing.sh`, `templates/pricing.json`, `docs/design-usage.md`.
- **Go:** `internal/pricing/`, `cmd/sdkit/pricing.go` (`sdkit pricing refresh|status`; `sdkit doctor --update-pricing` calls it).
- **writes:** `internal/pricing/`, `cmd/sdkit/pricing.go`.
- **Notes:** `<project-dir>/pricing.json` is the one table every cost figure comes from (Bedrock list prices). `refresh` fetches over `net/http` and writes atomically; a failed fetch keeps the old file.
- **Delete:** `scripts/pricing.sh`.
- **Tests (≤ 10 lines):** price resolution by model id incl. unknown model → unavailable.
- **Done when:** the table passes.

## P4-02 telemetry {#p4-02}

- **Wave:** 1 · **Depends:** P3 · **Size:** M
- **Source:** `scripts/telemetry.sh`, `docs/design-telemetry.md`.
- **Go:** `internal/telemetry/` (implements the `Telemetry` interface of P1-07; `now-ms`, `emit`, correlation ids, event schema), `cmd/sdkit/telemetry.go`.
- **writes:** `internal/telemetry/`, `cmd/sdkit/telemetry.go`.
- **Notes:** off unless `TELEMETRY=1`; one JSON line per event, appended with `store.AppendLine`; local only; a failure here never changes a decision. Wire it into `sdd` in `cmd/sdkit/sdd.go` through a one-line registration in `internal/telemetry/register.go` (do not edit `internal/sdd/`).
- **Delete:** `scripts/telemetry.sh`.
- **Tests (≤ 5 lines):** an event line has the schema fields.
- **Done when:** `sdd` emits `phase.reject` for a refused move when the setting is on.

## P4-03 digest, lookup, knowledge {#p4-03}

- **Wave:** 1 · **Depends:** P3 · **Size:** S
- **Source:** `scripts/digest.sh`, `scripts/lookup-cache.sh`, `scripts/knowledge.sh`, `docs/guide.md` (context economy).
- **Go:** `internal/digest/`, `internal/lookup/`, `internal/knowledge/`, `cmd/sdkit/{digest,lookup,knowledge}.go`.
- **writes:** those files.
- **Notes:** bounded views of long text (the full text stays on disk and is named in the view; failure lines are kept); scout answers cached per question and per code state; content-hash-versioned discovery notes reusable while the files they were written for are unchanged.
- **Delete:** the three scripts.
- **Tests (≤ 5 lines):** a cache entry is invalid after the hashed file changes.
- **Done when:** the table passes.

## P4-04 usage {#p4-04}

- **Wave:** 2 · **Depends:** P4-01 · **Size:** L
- **Source:** `scripts/usage.sh`, `docs/design-usage.md`.
- **Go:** `internal/usage/`, `cmd/sdkit/usage.go` (`record`, `report <task>`, `scan`, `budget`, `context`; budgets `MAX_SPAWNS`, `TOKEN_BUDGET_TASK`, modes `strict` and `guided`).
- **writes:** `internal/usage/`, `cmd/sdkit/usage.go`.
- **Notes:** ledger schema v2 (one JSON line per agent run and model), deduplication, price resolution through `pricing`, coverage warnings. Off unless `USAGE_LOG=1`. `usage context` reads a **Claude Code transcript only**; the transcript path and format come from the hook input (check the Windows path form against `P0-09`). Implements the `Budget` interface of P3-08.
- **Delete:** `scripts/usage.sh`.
- **Goldens (≤ 2):** a report with full records; a report with missing records (`N/A`).
- **Tests (≤ 15 lines):** dedupe table; cache hit ratio arithmetic; missing token record → unavailable.
- **Done when:** goldens match.

## P4-05 route {#p4-05}

- **Wave:** 3 · **Depends:** P4-02, P4-04 · **Size:** L
- **Source:** `scripts/route.sh` (633 lines, 89 `jq` calls), `docs/design-routing.md`, `skills/spec-driven-dev/references/model-routing.md`.
- **Go:** `internal/route/`, `cmd/sdkit/route.go`.
- **writes:** `internal/route/`, `cmd/sdkit/route.go`.
- **Notes:** a **deterministic function, no model call**: which adapter and model run a role, whether to run, reuse valid evidence, retry, escalate or stop. Risk floors and mandatory gates are never lowered. Reservations with pid liveness and TTL (`proc.Alive`). Hard cost policy vs advisory. Registry of models. Exit-code semantics 7 and 8 as in `02-contracts.md`.
- **Delete:** `scripts/route.sh`.
- **Goldens (≤ 3):** a decision for reuse, for escalate, for budget stop.
- **Tests (≤ 25 lines):** one decision table: (risk, role, evidence state, budget state, retries) → action.
- **Done when:** goldens match and the table passes.

## P4-06 report {#p4-06}

- **Wave:** 3 · **Depends:** P4-02, P4-04 · **Size:** L
- **Source:** `scripts/report.sh`, `docs/design-report.md`, `hooks/value-report.sh`.
- **Go:** `internal/report/` (implements the `Reporter` interface of P1-07), `cmd/sdkit/report.go` (`task|session|latest|history [--detailed|--json|--markdown]`, `finalize`).
- **writes:** `internal/report/`, `cmd/sdkit/report.go`.
- **Notes:** off unless `VALUE_REPORT=on`; printed once per task on stderr; **no model call**; savings against a stated baseline; honest labels (actual, estimated, unavailable). Orchestration adds agent runs, so the report never assumes it saves money.
- **Delete:** `scripts/report.sh`.
- **Goldens (≤ 2):** a task report and a report with missing data.
- **Tests (≤ 10 lines):** savings arithmetic and no double counting.
- **Done when:** goldens match.

## P4-07 usage and budget hooks {#p4-07}

- **Wave:** 3 · **Depends:** P4-02, P4-04 · **Size:** S
- **Source:** `hooks/guard-budget.sh`, `hooks/usage-log.sh`, `hooks/value-report.sh`.
- **Go:** `internal/hook/{guard_budget,usage_log,value_report}.go`.
- **writes:** those files.
- **Notes:** `guard-budget` (PreToolUse on `Agent|Task`): registers an independent role's spawn, counts it, checks the task's token budget, and stops before the spawn that goes over (`strict`) or warns (`guided`). `usage-log` (SubagentStop, Stop): appends token usage from the transcript and, with `TELEMETRY=1`, an `agent.end` event. `value-report` (SessionStart, SessionEnd): never blocks and never prints unless on.
- **Delete:** the three hook scripts.
- **Tests (≤ 5 lines):** over-budget spawn is refused in `strict`, warned in `guided`.
- **Done when:** the table passes.

## P4-08 run-role integration {#p4-08}

- **Wave:** 4 · **Depends:** P4-05 · **Size:** S
- **writes:** `internal/runrole/route.go`, `internal/runrole/budget.go` (new files in the package P3-08 owns; P3 is merged by now).
- **Do:** implement `Router` and `Budget` for `runrole` with `route` and `usage`; with `ROUTING` off nothing changes; exit 7 and 8 as the contract says.
- **Tests:** covered by the P4-05 table and the P3-08 dry-run table.
- **Done when:** a fixture run with valid evidence returns exit 7 and runs nothing.
