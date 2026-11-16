# P1 Enforcement core

Goal: the gated workflow runs on every OS in **standalone mode**: phase machine, evidence, git-hook guards, every agent hook, the verify engine and the six stacks. After P1 a developer can already be protected by `sdkit`, even before `init` is ported (P2). Bash is read from the oracle worktree `../go-port-oracle` (P0). Managed (Jira) mode, PRs and spec tooling are P3; cost, routing and telemetry are P4: `sdd` calls them only through small interfaces (see P1-07).

Depends on: all of P0 merged. (P0-09 may still be open; tasks marked `(assumption)` are revisited when it lands.)

## Waves

| Wave | Tasks (parallel) | Starts when |
|---|---|---|
| 1 | P1-01, P1-02, P1-03, P1-04, P1-05, P1-06 | P0 merged |
| 2 | P1-07 (needs 01 to 04), P1-08, P1-09, P1-10 (need 05), P1-11 (needs 06) | wave 1 merged |
| 3 | P1-12 (needs 05, 06, 07), P1-13 (needs 02, 06, 07, 08) | wave 2 merged |
| 4 | P1-14 | wave 3 merged |

## Exit criteria

- Scenarios A, B and C (`04-testing.md`) pass on macOS, and `GOOS=windows go vet ./...` is clean.
- `sdkit sdd gate edit <path>` refuses a code edit before RED and allows it in GREEN; `sdkit hook guard-phase` returns the deny JSON for the same input.
- `sdkit guard pre-push` refuses a push before `verified`.
- A hook given malformed JSON exits 0 with no output.
- Hook latency: a `PreToolUse` event for `Bash` runs in well under 100 ms on the dev machine (measure and write the number in `STATUS.md`).
- Test code of the phase within the card budgets (~640 lines including the scenarios).

---

## P1-01 state + workflow {#p1-01}

- **Wave:** 1 · **Depends:** P0 · **Size:** M
- **Source:** `scripts/state.sh`, `scripts/workflow.sh`, `sdkit_active_slug` in `scripts/env.sh`, the `get`/`put`/`need_state` helpers and the state layout of `scripts/sdd.sh`.
- **Go:** `internal/state/` (typed `State` for `<project-dir>/.sdd/state.json` and the per-task resumable state of `state.sh`, `Read`, `Update` through `store`, `ActiveSlug`), `internal/workflow/` (profiles `small|standard|high-assurance`, what a profile and risk require, `capabilities`), commands `cmd/sdkit/{state,workflow}.go`.
- **writes:** `internal/state/`, `internal/workflow/`, `cmd/sdkit/state.go`, `cmd/sdkit/workflow.go`.
- **Notes:** field names and the `.sdd/` file names stay (`02-contracts.md`). A profile never removes a gate; `small` is low risk only. `workflow policy [--json]` and `transitions` text are quoted by skills.
- **Delete:** `scripts/state.sh`, `scripts/workflow.sh`.
- **Goldens:** `workflow policy` for (low, standard), (medium, standard), (high, high-assurance); `state show` of a task.
- **Tests (≤ 40 lines):** one table: profile × risk → required roles and gates; `small` with medium refused.
- **Done when:** goldens match; the table passes.

## P1-02 verdict {#p1-02}

- **Wave:** 1 · **Depends:** P0 · **Size:** M
- **Source:** `scripts/parse-verdict.sh`, `hooks/record-verdict.sh` (for how the result is recorded).
- **Go:** `internal/verdict/` (`Parse(text) Result` with format and semantic checks, normalisation, the "PASS without evidence is not PASS" rule, malformed reasons), command `sdkit verdict parse [--record]`.
- **writes:** `internal/verdict/`, `cmd/sdkit/verdict.go`.
- **Notes:** the block format is `DOD / FINDINGS / VERIFY / VERDICT`, exact. A malformed block is a format failure (re-ask), not a BLOCK and not a retry. `--record` calls into `sdd` only through an interface (`Recorder`) set by P1-07, so this package does not import `sdd`.
- **Delete:** `scripts/parse-verdict.sh`.
- **Goldens:** valid PASS, valid BLOCK, PASS with no VERIFY evidence, malformed block, extra text around the block.
- **Tests (≤ 40 lines):** one table of those cases plus lowercase and whitespace variants.
- **Done when:** goldens match.

## P1-03 plan {#p1-03}

- **Wave:** 1 · **Depends:** P0 · **Size:** M
- **Source:** `scripts/dod-check.sh`, `scripts/check-plan-parity.sh`, `scripts/check-writes.sh`, `scripts/plan-waves.sh`.
- **Go:** `internal/plan/` (`CheckDoD`, `Parity`, `CheckWrites`, `Waves`), commands `sdkit dod check`, `sdkit plan parity|writes|waves`.
- **writes:** `internal/plan/`, `cmd/sdkit/dod.go`, `cmd/sdkit/plan.go`.
- **Notes:** tasks.md line grammar: `- [ ] T3 ... | writes: a/,b.go | after: T1 | size: S`. Tasks with overlapping `writes:` never share a wave; `after:` orders them; cycles are an error. `D<n>: ... | Verified by: ...` lines make the DoD.
- **Delete:** the four scripts.
- **Goldens:** waves for a 5-task graph; parity failure text; DoD missing list.
- **Tests (≤ 30 lines):** waves table (disjoint, overlap, `after:`, cycle); writes check (inside, outside).
- **Done when:** goldens match.

## P1-04 base, worktree, capped {#p1-04}

- **Wave:** 1 · **Depends:** P0 · **Size:** S
- **Source:** `scripts/resolve-base.sh`, `scripts/worktree.sh`, `scripts/capped.sh`.
- **Go:** `internal/base/`, `internal/worktree/`, `internal/capped/`; commands `sdkit base`, `sdkit worktree add|remove|list`, `sdkit capped -- <cmd>`.
- **writes:** those three packages and `cmd/sdkit/{base,worktree,capped}.go`.
- **Notes:** `base` honours the CI-provided base ref first, then `DIFF_BASE_CMD`, then the branch flow (`BASE_BRANCH`, release flow in `project.md` is read by the agent, not by this code). `capped` keeps the exit code, the failing lines and the tail; output size limits as in the bash; runs the command with `proc.Shell`.
- **Delete:** the three scripts.
- **Goldens:** `capped` on a failing command with long output.
- **Tests (≤ 15 lines):** `capped` keeps failure lines and the exit code.
- **Done when:** golden matches.

## P1-05 verify engine + stack interface {#p1-05}

- **Wave:** 1 · **Depends:** P0 · **Size:** M
- **Source:** `scripts/verify-core.sh`, `scripts/verify-lib.sh`, `stacks/generic/*`, and the shape of `stacks/go/scripts/verify.sh` + `steps.sh`.
- **Go:** `internal/verify/` (engine: changed set = committed + staged + unstaged + untracked against the diff base, `--fast`, `--since-head`, `--no-cache`, cache per code state, runs `verify.d` policies), `internal/stack/stack.go` (the interface: `Detect(root)`, `Tools()` as `required|name|hint` records, `Fmt(file)`, `Steps(ctx)`), `internal/stack/generic.go`, command `sdkit verify`.
- **writes:** `internal/verify/`, `internal/stack/stack.go`, `internal/stack/generic.go`, `cmd/sdkit/verify.go`.
- **Notes:** **user-facing contract:** policies in `<project-dir>/verify.d/` receive the environment variables `VERIFY_ROOT`, `VERIFY_MERGE_BASE`, `VERIFY_CHANGED`, `VERIFY_CHANGED_GO` (names and formats unchanged; see `scripts/verify-lib.sh`). Policies run in name order, `_*` skipped, `*.example` ignored, `.sh/.ps1/.cmd/.exe` per `03-cross-platform.md` item 11. A passing result is cached per code state. Output is trimmed to a summary plus the failing lines.
- **Delete:** `scripts/verify-core.sh`, `scripts/verify-lib.sh`, `stacks/generic/detect.sh`.
- **Goldens:** a failing policy's report; a cached second run.
- **Tests (≤ 15 lines):** policy discovery order and skipping; cache hit on an unchanged tree.
- **Done when:** golden matches.

## P1-06 hook framework + host adapters {#p1-06}

- **Wave:** 1 · **Depends:** P0 · **Size:** M
- **Source:** `hooks/lib.sh`, `hooks/adapt.sh`, `hooks/hooks.json` (the matcher table), `adapters/opencode/plugins/spec-driven-kit-guard.js`, `adapters/cursor/hooks.json`, `adapters/gemini/settings.json`.
- **Go:** `internal/hook/` (`Input`, `Decision`, a registry `Register(event, tools, name, fn)`, the dispatcher with merge **deny > ask > allow**, Claude output, the Cursor and Gemini translators, panic recovery), `cmd/sdkit/hook.go` (`sdkit hook [--host H] <event|name>`).
- **writes:** `internal/hook/hook.go`, `registry.go`, `hosts.go`, `dispatch.go`, `cmd/sdkit/hook.go`. Guards live in other files of the same package (`guard_<name>.go`) written by P1-11, P1-12, P1-13, P3-05 and P4-07 and register themselves from `init()`.
- **Notes:** project dir = `CLAUDE_PROJECT_DIR` else the git top level else the cwd (as `lib.sh`); the host root variables as `adapt.sh`; `agent_type` from `subagent_type` or `agent_type`. `decide` semantics: allow with no output exits 0. Any error ends in exit 0 with no output. Do **not** edit `hooks/hooks.json` or the adapter JSON files yet (P5-01a rewrites them).
- **Delete:** `hooks/lib.sh`, `hooks/adapt.sh`.
- **Goldens:** one `adapt` translation per host for a deny and an ask.
- **Tests (≤ 20 lines):** merge table (deny beats ask beats allow; two allows; error in one guard); malformed JSON input.
- **Done when:** goldens match.

## P1-07 sdd core {#p1-07}

- **Wave:** 2 · **Depends:** P1-01, P1-02, P1-03, P1-04 · **Size:** L (one worker, commits split in the order below)
- **Source:** `scripts/sdd.sh` **except the managed section** (`m_contract`, `m_deps`, `m_sha`, `m_stale`, `m_merged`, `m_write_dod`, `managed_proposed_gate`, `cmd_start_managed`, `cmd_block`, `cmd_revalidate`, `cmd_evidence_sync`, and the managed `done`; those are P3-11). Read the design notes in `docs/design-workflow.md`, `design-evidence.md`.
- **Go:** `internal/sdd/` split in files so commits stay reviewable: `phase.go` (start, advance, transitions, status, phase, abort, done), `gate.go` (edit and push gate, `SDD_MODE`, `SDD_SKIP`), `floor.go` (`RISK_FLOOR_GLOBS`: label floor, `floor --validate`), `evidence.go` (`evidence`, `_spawn`, `_spawn-close`, `_evidence`, `_verdict`, validity per diff, trust `spawned`, staleness), `attest.go` (attestation write, `fingerprint`, `check-fresh`), `ci.go` (`ci-check`, `ci_settings_from_base`), `cmd/sdkit/sdd.go`.
- **writes:** `internal/sdd/`, `cmd/sdkit/sdd.go`.
- **Notes:**
  - Interfaces for later phases, defined here and given no-op defaults: `Telemetry` (`Emit(type, fields...)`), `Reporter` (`Finalize(status)`), `Managed` (the Jira path; P3-11 supplies it; absent means managed commands print the same usage as the bash does when `SDD_MANAGED` is not on). A failure in a telemetry or report call never changes a decision.
  - The gate is the single decision point for hooks, git hooks and CI. Path normalisation, fail-closed fingerprint, RED/GREEN binding, trusted CI gate: read `tests/gate-security.test.sh` for the attack cases, and turn them into table rows, not tests.
  - `advance red` runs the test command with `proc.Shell` and requires failure for the right reason (a test that passes is refused). `advance green` requires pass. Output goes to `.sdd/last-test.log`.
  - Fingerprint: keep the 1.x algorithm (D11) unless it depends on `shasum`/`sort` locale behaviour; if it does, say so in the report.
  - CI: `ci-check` reads settings from the **base** ref, never from the PR's working tree, and the base's rules apply (`06-release.md`, "CI gate").
  - Commit order: phase, gate, floor, evidence, attest, ci.
- **Delete:** `scripts/sdd.sh` (the managed section stays readable in the oracle).
- **Goldens (≤ 8):** `status`, `transitions`, `evidence` table, `policy`, refused `advance proposed` (no DoD), refused `advance red` (test passes), `gate edit` refusal text, `floor` output.
- **Tests (≤ 100 lines):** one gate matrix table (phase × risk × path kind × mode), one transitions table, one evidence validity table (missing, valid, block, stale, untrusted, wrong-task, conflict), floor table, concurrent `advance` (one wins).
- **Done when:** goldens match and the tables pass with `-race`.

## P1-08 stacks: go, python {#p1-08}

- **Wave:** 2 · **Depends:** P1-05 · **Size:** S
- **Source:** `stacks/go/{detect.sh,scripts/*.sh}`, `stacks/python/{detect.sh,scripts/*.sh}` (data files `permissions.txt`, `rules/`, `golangci.yml`, `known-pitfalls.md`, `verify.d.example/` stay).
- **Go:** `internal/stack/go.go`, `internal/stack/python.go` (detect by `go.mod`, by `pyproject.toml`/`setup.py`/`requirements.txt`; tools list; formatter; verify steps).
- **writes:** those two files.
- **Notes:** Python runner detection (`uv`, `poetry`, plain) as `steps.sh`; the tool name on Windows is `python`, not `python3` (use `proc.LookPath` for both).
- **Delete:** the two stacks' `detect.sh` and `scripts/*.sh`.
- **Tests (≤ 10 lines):** `Detect` table (fixture file lists).
- **Done when:** `sdkit verify` on a tiny Go fixture repo runs `go vet`/`go build` steps.

## P1-09 stacks: jvm, spring-boot {#p1-09}

- **Wave:** 2 · **Depends:** P1-05 · **Size:** S
- **Source:** `stacks/jvm/*`, `stacks/spring-boot/*` scripts and detect (Maven or Gradle; Spring Boot detection reads the build file).
- **writes:** `internal/stack/jvm.go`, `internal/stack/springboot.go`.
- **Notes:** on Windows the wrappers are `gradlew.bat` and `mvnw.cmd`; prefer the wrapper when present.
- **Delete:** the two stacks' `detect.sh` and `scripts/*.sh`.
- **Tests (≤ 10 lines):** `Detect` table (jvm vs spring-boot).
- **Done when:** detection matches the README table.

## P1-10 stacks: nextjs, react-native {#p1-10}

- **Wave:** 2 · **Depends:** P1-05 · **Size:** S
- **Source:** `stacks/nextjs/*`, `stacks/react-native/*`, `stacks/_node/scripts/node-lib.sh`.
- **writes:** `internal/stack/node.go` (shared helpers), `internal/stack/nextjs.go`, `internal/stack/reactnative.go`.
- **Notes:** `package.json` detection (`next`, `react-native`); package manager detection; the Xcode check uses `runtime.GOOS == "darwin"` (`03-cross-platform.md` item 12); on Windows `npm` is `npm.cmd` (`LookPath`).
- **Delete:** the two stacks' `detect.sh`, `scripts/*.sh`, and `stacks/_node/`.
- **Tests (≤ 10 lines):** `Detect` table.
- **Done when:** detection matches the README table.

## P1-11 guards A: file and command guards {#p1-11}

- **Wave:** 2 · **Depends:** P1-06, P0-04 · **Size:** M
- **Source:** `hooks/guard-secrets.sh`, `guard-protected.sh`, `frozen-tests.sh`, `guard-readonly.sh`, `guard-noisy.sh`, `guard-prod.sh`, `guard-git.sh`.
- **Go:** `internal/hook/guard_<name>.go`, one file per guard, each registering with the dispatcher for the events and tools in `01-architecture.md`.
- **writes:** the seven `internal/hook/guard_*.go` / `frozen.go` files of this card.
- **Notes:** each guard is off or silent when its setting is empty (`SECRET_GLOBS`, `PROTECTED_PATHS`, `NOISY_CMD_PATTERNS`, release-flow settings). `guard-readonly`: a read-only role may not change files; patterns include `sed -i`, `perl -i`, redirects, `tee`, `cp`, `mv`. `guard-prod`: state-changing `kubectl` against a production context asks. `guard-git`: the release flow's git rules from `project.md` settings. Port each regex, then try the bypass cases of `tests/hardening.test.sh`, `tests/security.test.sh`, `tests/gate-security.test.sh` against it (RE2 differs from ERE).
- **Delete:** the seven hook scripts.
- **Goldens (≤ 4):** a deny JSON for a secret path, an ask for prod kubectl, a read-only role redirect, a noisy-command ask.
- **Tests (≤ 80 lines):** one table per guard group with `{guard, tool, input, settings, want}` rows: positives, negatives, and every bypass that the old suites proved.
- **Done when:** goldens match; the tables pass.

## P1-12 phase and PR guards, git-hook guard {#p1-12}

- **Wave:** 3 · **Depends:** P1-05, P1-06, P1-07 · **Size:** M
- **Source:** `hooks/guard-phase.sh`, `hooks/guard-pr.sh`, `scripts/guard.sh`, `adapters/git/pre-commit`, `adapters/git/pre-push`.
- **Go:** `internal/hook/guard_phase.go`, `guard_pr.go`, `internal/guard/` (`PreCommit`, `PrePush`), `cmd/sdkit/guard.go`; the two git-hook shims become `#!/bin/sh` + `exec sdkit guard pre-commit "$@"` (with the "sdkit not installed, guard skipped" message and exit 0).
- **writes:** the files above and `adapters/git/pre-commit`, `adapters/git/pre-push`.
- **Notes:** `guard pre-commit` checks every staged file: gate `edit`, `SECRET_GLOBS`, `.frozen`, `PROTECTED_PATHS` (override `ALLOW_PROTECTED=1`). `guard pre-push`: gate `push`, `PROTECTED_BRANCHES` (override `ALLOW_PROTECTED_PUSH=1`), spec gate (c) when `SPEC_CHECK_ON_PUSH=1` through an interface `SpecChecker` that P3-09 supplies (until then it reports the check unavailable and does not block), verify `--fast --since-head` unless `GUARD_SKIP_VERIFY=1`. `SDD_SKIP=1` bypasses the gate once; `strict` blocks, `guided` warns. `guard-pr` refuses `_spawn/_evidence/_spawn-close/_verdict` from the model's shell and writes to `attestations/`, and gates `git push`, `gh pr create`, `glab mr create`, Bitbucket API calls: port its regexes with their bypass cases.
- **Delete:** `hooks/guard-phase.sh`, `hooks/guard-pr.sh`, `scripts/guard.sh`.
- **Goldens (≤ 3):** pre-push refusal text, pre-commit frozen-test refusal, `guard-pr` deny.
- **Tests (≤ 20 lines):** `guard-pr` bypass table rows (the dangerous ones: `env`, `command`, `nohup` prefixes, quoting, `;`/`&&` chains).
- **Done when:** goldens match.

## P1-13 lifecycle hooks {#p1-13}

- **Wave:** 3 · **Depends:** P1-02, P1-06, P1-07, P1-08 · **Size:** S
- **Source:** `hooks/session-context.sh`, `check-tools.sh`, `post-edit-fmt.sh`, `stop-verify.sh`, `record-verdict.sh`, `result-size.sh`.
- **Go:** `internal/hook/{session_context,check_tools,post_edit_fmt,stop_verify,record_verdict,result_size}.go`.
- **writes:** those files.
- **Notes:** `session-context` prints branch, ticket and open-change context; `check-tools` lists tools the stack needs but cannot find, **and compares the plugin version with the binary version** (new in Go, `06-release.md`); `post-edit-fmt` formats the written file with the stack's formatter; `stop-verify` blocks "done" while uncommitted changes fail vet/build (honours `stop_hook_active`); `record-verdict` records a verifier, reviewer or security-auditor verdict for the diff as it is now (high risk: only a registered run counts); `result-size` warns above `SUBAGENT_RESULT_MAX_LINES`.
- **Delete:** the six hook scripts.
- **Tests (≤ 10 lines):** `stop-verify` does nothing when `stop_hook_active` is true.
- **Done when:** scenario C (P1-14) covers them.

## P1-14 scenarios A, B, C {#p1-14}

- **Wave:** 4 · **Depends:** P1-01 to P1-13 · **Size:** M
- **writes:** `internal/scenario/scenario_test.go` (and `scenario_hooks_test.go`).
- **Do:** write scenarios A, B and C exactly as defined in `04-testing.md`, with `TempRepo` and `RunKit`. Scenario B includes the CI-gate property (a PR that edits `sdkit.env` or the check cannot change the base's verdict).
- **Delete:** nothing (the old shell suites are removed in P5-02).
- **Tests (≤ 220 lines):** A ≈ 70, B ≈ 90, C ≈ 60.
- **Done when:** the three scenarios pass on macOS and under `go test -race`; record the hook latency measurement in `STATUS.md`.
