# P3 Integrations

Goal: everything that talks to the outside or to the analyst side: pull requests (Bitbucket, GitHub, GitLab), Jira, specs and spec changes, tasks, EGM-based review, agent contracts, running roles as separate processes on any host, recovery, redaction, audit, project memory. The managed (Jira-driven) mode of `sdd` closes here. After P3 the whole spec-driven flow works on all OSes: **`2.0.0-beta` decision point** (D6; the user decides, the orchestrator never tags).

Depends on: P1 merged (P2 is independent except for `doctor` messages).

Reading bash that was deleted: `git show master:<path>` or the oracle worktree `../go-port-oracle`.

## Waves

| Wave | Tasks (parallel) | Starts when |
|---|---|---|
| 1 | P3-01, P3-02, P3-03, P3-04, P3-05, P3-06, P3-07, P3-08, P3-10, P3-12 | P1 merged |
| 2 | P3-09 (needs 01, 03, 10), P3-11 (needs 01, 02) | wave 1 merged |
| 3 | P3-13 | wave 2 merged |

## Exit criteria

- Scenario D passes. `sdkit run-role <role> --dry-run` prints the right command for each of the five hosts (table test).
- `sdkit pr open` picks the right host from the origin URL, and a Bitbucket request carries `Authorization: Bearer` for an `ATCTT...` token and basic auth otherwise (httptest).
- `sdkit spec check` and the spec gate (c) of `guard pre-push` work (P1-12 `SpecChecker` supplied).
- No `curl`, `python3`, `jq` or `awk` is used anywhere in Go code (`grep` for the strings in `exec` calls).
- Test code of the phase within budget (~210 lines including scenario D).

---

## P3-01 pr {#p3-01}

- **Wave:** 1 · **Depends:** P1 · **Size:** M
- **Source:** `scripts/open-pr.sh`, `scripts/pr-check.sh`, `docs/hosts.md`.
- **Go:** `internal/pr/` (host detection `PR_HOST=auto` from the origin URL; Bitbucket REST through `net/http`: create, find existing, update, close; GitHub through `gh`; GitLab through `glab`), `cmd/sdkit/pr.go` (`sdkit pr open|check`).
- **writes:** `internal/pr/`, `cmd/sdkit/pr.go`.
- **Notes:** credentials as the bash: `BITBUCKET_EMAIL` + `BITBUCKET_TOKEN` (basic) or an `ATCTT...` access token (Bearer, no email), an origin URL with embedded credentials, `PR_TOKEN_REPO`; tokens never in `sdkit.env`. Update and close are used by spec changes (P3-03/09). `pr-check` is the cheap risk-based PR check of the kit itself and vendoring repos.
- **Delete:** `scripts/open-pr.sh`, `scripts/pr-check.sh`.
- **Goldens (≤ 1):** the request body for a Bitbucket create.
- **Tests (≤ 15 lines):** host detection table; auth header table (httptest).
- **Done when:** golden matches.

## P3-02 jira {#p3-02}

- **Wave:** 1 · **Depends:** P1 · **Size:** M
- **Source:** `scripts/jira.sh`, `adapters.d/jira.sh`, `docs/design-role-aware.md` ("Task contract").
- **Go:** `internal/jira/`, `cmd/sdkit/jira.go`, `internal/testutil/fakejira.go` (an `httptest` Jira, ≤ 40 lines, counts toward the helper budget).
- **writes:** those files.
- **Notes:** the egress gate `JIRA_EGRESS=allow` (off means no call), `JIRA_API_TOKEN` and `JIRA_USER` only from `sdkit.local.env` or the environment (**`jira` refuses a token found in `sdkit.env`**), Cloud basic auth with the e-mail, contract validation (required fields), subtask creation idempotent by key, comment, transition by status name, link types from settings. The MCP path is the agent's, not this code's.
- **Delete:** `scripts/jira.sh`, `adapters.d/jira.sh`.
- **Tests (≤ 15 lines):** contract validation table (missing field → named refusal); token in `sdkit.env` refused.
- **Done when:** scenario D (P3-13) covers it.

## P3-03 spec change and apply {#p3-03}

- **Wave:** 1 · **Depends:** P1 · **Size:** M
- **Source:** `scripts/spec-change.sh`, `scripts/spec-apply.sh`, `docs/design-spec-merge.md`, `skills/update-specs`, `skills/merge-specs`.
- **Go:** `internal/spec/change.go`, `internal/spec/apply.go` (AC parsing: `#### Scenario: AC-<capability>-<n> - ...`, content hashes, `change.json`, roots = a base `specs/<id>/` or a change `specs/<id>/changes/<slug>/`, drift facts, merge facts, apply on a scratch copy), `cmd/sdkit/spec.go` registers `spec change|apply` (the other `spec` subcommands are P3-09, in their own file `cmd/sdkit/spec_check.go`).
- **writes:** `internal/spec/change.go`, `internal/spec/apply.go`, `cmd/sdkit/spec.go`.
- **Notes:** spec-change is pure facts: no model, no network, no git, and it never writes a file; `spec-apply` is the file-writing half. Keep that split. Output order must be deterministic (it was `awk`/`sort` based).
- **Delete:** `scripts/spec-change.sh`, `scripts/spec-apply.sh`.
- **Goldens (≤ 3):** AC list with hashes for a fixture spec; a conflict report for two sibling changes; an apply result.
- **Tests (≤ 25 lines):** AC parse and hash table; sibling overlap table.
- **Done when:** goldens match.

## P3-04 tasks {#p3-04}

- **Wave:** 1 · **Depends:** P1 · **Size:** S
- **Source:** `scripts/tasks.sh`, `skills/generate-tasks/references/task-format.md`.
- **Go:** `internal/tasks/`, `cmd/sdkit/tasks.go`: trace every AC to a task, reject dependency cycles, stable ids, render, report; idempotent upsert plan (the Jira writes go through `internal/jira` in P3-02's API, called by the skill).
- **writes:** `internal/tasks/`, `cmd/sdkit/tasks.go`.
- **Delete:** `scripts/tasks.sh`.
- **Goldens (≤ 1):** the rendered task list for a fixture.
- **Tests (≤ 15 lines):** trace gap, cycle, stable id table.
- **Done when:** golden matches.

## P3-05 contract and its hook {#p3-05}

- **Wave:** 1 · **Depends:** P1 · **Size:** M
- **Source:** `scripts/contract.sh`, `hooks/check-contract.sh`, `docs/design-contracts.md`, `skills/spec-driven-dev/references/contract.md`.
- **Go:** `internal/contract/`, `internal/hook/check_contract.go`, `cmd/sdkit/contract.go`.
- **writes:** those files.
- **Notes:** off unless `CONTRACTS=1`. Advisory in the hook (never blocks; an invalid handoff asks first in `strict`). Other hosts get the same checks from `run-role` (exit 4 and 5).
- **Delete:** `scripts/contract.sh`, `hooks/check-contract.sh`.
- **Tests (≤ 10 lines):** valid and invalid handoff, valid and invalid answer.
- **Done when:** `run-role` integration (P3-08) can call `contract.Check`.

## P3-06 memory {#p3-06}

- **Wave:** 1 · **Depends:** P1 · **Size:** S
- **Source:** `scripts/memory.sh`, `skills/spec-driven-dev/references/memory.md`.
- **Go:** `internal/memory/`, `cmd/sdkit/memory.go`. On by default (`MEMORY=1`), one index line per entry, `--memory-vcs ignore` honoured.
- **writes:** `internal/memory/`, `cmd/sdkit/memory.go`.
- **Delete:** `scripts/memory.sh`.
- **Tests (≤ 5 lines):** add and list keeps one index line each.
- **Done when:** the skill's commands exist with the same arguments.

## P3-07 recover, redact, audit {#p3-07}

- **Wave:** 1 · **Depends:** P1 · **Size:** S
- **Source:** `scripts/recover.sh`, `scripts/redact.sh`, `scripts/audit.sh`, `docs/design-security.md`.
- **Go:** `internal/recover/`, `internal/redact/`, `internal/audit/`, `cmd/sdkit/{recover,redact,audit}.go`.
- **writes:** those files.
- **Notes:** `redact` replaces credential-shaped strings with `[REDACTED:<kind>]`, deterministic patterns only (RE2 check of each pattern). `audit` is an append-only, **hash-chained** JSONL: metadata only; ordered fields; the chain must verify against a chain written by the bash for a fixture (golden). `recover scan` closes bookkeeping a crash left open and never dispatches or retries; liveness through `proc.Alive`.
- **Delete:** the three scripts.
- **Goldens (≤ 2):** a redaction sample; an audit chain of 3 entries.
- **Tests (≤ 10 lines):** redaction patterns table; chain break detection.
- **Done when:** the golden chain verifies.

## P3-08 adapter and run-role {#p3-08}

- **Wave:** 1 · **Depends:** P1 · **Size:** L
- **Source:** `scripts/run-role.sh`, `scripts/adapter.sh`, `adapters.d/litellm.sh`, `core/adapters.json`, `core/hosts.env`, `core/tiers.env`, `docs/design-adapters.md`, `skills/spec-driven-dev/references/hosts.md`.
- **Go:** `internal/adapter/` (the five contract verbs; built-in hosts `claude codex opencode cursor gemini`; the OpenAI-compatible LiteLLM adapter through `net/http` with an egress permission and secret handling), `internal/runrole/` (headless run of one role: fresh context, only the handoff, pinned directory, read-only when the role is, `--dry-run`, `--tier`, `--host`, `--handoff`, `--cwd`, signal forwarding, process-tree kill), `cmd/sdkit/runrole.go`.
- **writes:** `internal/adapter/`, `internal/runrole/`, `cmd/sdkit/runrole.go`.
- **Notes:** default host: `VERIFIER_HOST` for verifier roles, else `RUN_ROLE_HOST`. At high risk it **registers the spawn** and records the verdict itself (trust `spawned`) through `sdd` internals. Exit codes: 0 ran, 2 policy/config, 3 budget (`--ignore-budget` after the user agreed), 4 and 5 contract; **7 and 8 are produced only with `ROUTING=on` and are wired in P4-08**: leave a seam (`Router` interface, nil means no routing). Budget checks use a `Budget` interface with a nil default until P4. A host that reports no tokens leaves the token budget silent. Never log prompts.
- **Delete:** `scripts/run-role.sh`, `scripts/adapter.sh`, `adapters.d/litellm.sh`.
- **Goldens (≤ 2):** `--dry-run` output for claude and codex.
- **Tests (≤ 15 lines):** `--dry-run` table: five hosts × read-only/not → command line pieces (sandbox flag, cwd, model/tier).
- **Done when:** goldens match and the table passes.

## P3-09 spec {#p3-09}

- **Wave:** 2 · **Depends:** P3-01, P3-03, P3-10 · **Size:** M
- **Source:** `scripts/spec.sh`, `docs/design-spec-merge.md`, `skills/generate-specs`, `skills/update-specs`.
- **Go:** `internal/spec/spec.go` (publish, check, drift, `spec/<id>` and `spec/<id>--<slug>` branches, version labels, review gate: **publish refuses without a fresh review PASS bound to the content hash**), `cmd/sdkit/spec_check.go`, and the `SpecChecker` implementation registered for `guard pre-push` (gate (c), `SPEC_CHECK_ON_PUSH=1`).
- **writes:** `internal/spec/spec.go`, `cmd/sdkit/spec_check.go`, `internal/guard/speccheck.go`.
- **Delete:** `scripts/spec.sh`.
- **Goldens (≤ 2):** `spec check` for a mergeable and a not-mergeable spec.
- **Tests (≤ 15 lines):** publish refused without PASS; refused when the PASS is stale.
- **Done when:** goldens match.

## P3-10 review {#p3-10}

- **Wave:** 1 · **Depends:** P1 · **Size:** M
- **Source:** `scripts/review.sh`, `templates/review.d/*.env`, `skills/generate-specs/references/review.md`.
- **Go:** `internal/review/`, `cmd/sdkit/review.go`: the deterministic half of the spec review. The PASS is bound to the document's content hash, any edit makes it stale, a review without fresh isolation is `NOT_RUN` and never PASS, `SPEC_REVIEW_MAX_ROUNDS` then a person decides, `JIRA_USER` recorded when a finding is accepted.
- **writes:** `internal/review/`, `cmd/sdkit/review.go`.
- **Delete:** `scripts/review.sh`.
- **Tests (≤ 10 lines):** PASS/stale/NOT_RUN table.
- **Done when:** the table passes.

## P3-11 managed sdd {#p3-11}

- **Wave:** 2 · **Depends:** P3-01, P3-02, P1-07 · **Size:** M
- **Source:** the managed section of `scripts/sdd.sh` in the oracle: `m_contract`, `m_deps`, `m_sha`, `m_stale`, `m_merged`, `m_write_dod`, `managed_proposed_gate`, `cmd_start_managed`, `cmd_block`, `cmd_revalidate`, `cmd_evidence_sync`, and the managed `done --merged`; `skills/spec-driven-dev/references/managed.md`.
- **Go:** `internal/sdd/managed.go` implementing the `Managed` interface of P1-07, and the commands `start --ticket`, `block --spec-change`, `revalidate --sha`, `evidence-sync`, `done --merged`.
- **writes:** `internal/sdd/managed.go`, `internal/sdd/managed_cmds.go`.
- **Notes:** `start --ticket KEY` fetches and validates the task contract, checks the pinned `specSha` exists in `SPECS_DIR` and every dependency is done, writes the DoD from the ACs. While `.blocked` is set every `advance` and every code edit is refused; `revalidate` resumes only on a new approved revision and drops old evidence. Outside managed mode these commands print the usage like the bash did.
- **Delete:** nothing (the file is already gone with P1-07).
- **Tests:** covered by scenario D.
- **Done when:** scenario D passes.

## P3-12 dispatch {#p3-12}

- **Wave:** 1 · **Depends:** P1 · **Size:** S
- **Source:** `scripts/dispatch.sh`, `skills/spec-driven-dev/references/parallel.md`.
- **Go:** `internal/dispatch/`, `cmd/sdkit/dispatch.go`: idempotent ledger of parallel tasks and the rule that decides whether a task may start now (it starts nothing).
- **writes:** `internal/dispatch/`, `cmd/sdkit/dispatch.go`.
- **Delete:** `scripts/dispatch.sh`.
- **Tests (≤ 5 lines):** a task with an overlapping write set is refused while another runs.
- **Done when:** the table passes.

## P3-13 scenario D {#p3-13}

- **Wave:** 3 · **Depends:** all of P3 · **Size:** S
- **writes:** `internal/scenario/scenario_managed_test.go`.
- **Do:** scenario D as in `04-testing.md`, with `FakeJira` and a fake `gh`.
- **Tests (≤ 70 lines).**
- **Done when:** passes under `-race`.
