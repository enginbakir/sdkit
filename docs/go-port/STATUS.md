# Status

The orchestrator updates this file after every merge into `v2`. States: `todo`, `running`, `merged`, `blocked`, `cut` (not in 2.0, with the reason). Test lines = added test code against the card budget.

Setup done: [x] oracle worktree `../go-port-oracle` at `master` (detached, 1153507) · [x] branch `v2` created (worktree `../go-port-v2`) · [x] D7 resolved (before P4): F07–F20 all implemented in 1.x with tests; P4-01..08 ported; F10 test-select (D12), F11/F20 bench (D5) not in 2.0; the rest already ported in P1–P3

## Tasks

| ID | Title | Wave | Depends | Budget | State | Branch / commit | Notes |
|---|---|---|---|---|---|---|---|
| P0-01 | Scaffold | 0 | - | 0 | merged | v2-P0-01 / 5e264dc | exit-code consts in internal/cli; no go.sum (no deps) |
| P0-04 | glob | 1 | 01 | 30 | merged | v2-P0-04 / dc6a8d0 | foldCase param; HasDirPrefix for hooks' prefix/ form; 25 test lines |
| P0-05 | proc + gitx | 1 | 01 | 15 | merged | v2-P0-05 / 1d9f33c | 30 test lines, 16 without package/import boilerplate; Shell(ctx,cmd)*exec.Cmd + RunShell |
| P0-06 | CI and release skeleton | 1 | 01 | 0 | merged | v2-P0-06 / 2256026 | YAML parsed only; actionlint/goreleaser not installed |
| P0-07 | Contracts inventory | 1 | 01 | 0 | merged | v2-P0-07 / 90fc164 | 100 rows + 16 bash snippets; 8 gaps in 02-contracts.md found (see Open questions) |
| P0-08 | Golden harness + testutil | 1 | 01 | helpers ≤150 | merged | v2-P0-08 / 6fac451 | testutil 136 lines + 11-line sample; spec next to golden; sample = sdkit version |
| P0-02 | envfile + project | 2 | 05, 08 | 60 | merged | v2-P0-02 / 96bc544 | ~54 test lines; 2 goldens via BASH_ENV probe; $NAME expands only earlier settings (bash also expanded env like $HOME: note for upgrading.md); Bool accepts 1/on/true/yes |
| P0-03 | store | 2 | 05 | 30 | merged | v2-P0-03 / 5c5e102 | lock <path>.lock dir+pid; stale 3s dead owner / 10s any; 30s bounded wait (bash waits forever, card says bounded); ~28 test lines |
| P0-05b | Shell selection (Q2) | P1 w1 | 05 | 10 | merged | v2-P0-05b / 5c22a51 | sh/bash -c, powershell/pwsh -NoProfile -Command, cmd /S /C; unset: sh>powershell>cmd; Windows paths only vetted |
| P0-09 | Windows host spike | 2 | 01, 06 | 0 | merged | v2-P0-09 / 68c376d | 11/30 confirmed from docs, 19 need CI run; P2-03 blocked for Codex/Gemini/OpenCode (Q3); needs the user to push for the CI part |
| P1-01 | state + workflow | 1 | P0 | 40 | merged | v2-P1-01 / d3ce10b | 38 test lines; 2 goldens via BASH_ENV probes; Pending hook var for contracts; telemetry skipped until P4; set spawns <x> now errors |
| P1-02 | verdict | 1 | P0 | 40 | merged | v2-P1-02 / 00fe9e0 | 38 test lines; 2 goldens hand-run; TODO wire: verdictRecorder (P1-07), Options.Accepted -> plan dod accept (P1-03) |
| P1-03 | plan | 1 | P0 | 30 | merged | v2-P1-03 / 4491bae | 28 test lines; 1 golden hand-run; also ported dod accept/accepted/acceptances; plan writes exits 1 on git error |
| P1-04 | base, worktree, capped | 1 | P0 | 15 | merged | v2-P1-04 / 2a72122 | 11 test lines; golden hand-run (oracle sdkit has no capped); TODO: worktree merge <id> <tasks.md> -> plan.CheckWrites after P1-03; capped.ActiveSlug -> state pkg after P1-01 |
| P1-05 | verify engine + stack interface | 1 | P0 | 15 | merged | v2-P1-05 / 5a80146 | 15 test lines; 1 golden (hand-run oracle verify.sh); .sh policies no longer need +x (Windows); passing step output no longer echoed |
| P1-06 | hook framework + host adapters | 1 | P0 | 20 | merged | v2-P1-06 / eaa0b93 | 19 test lines; 1 golden (5 cases) via own capture-adapt.sh; Register(event,tools,name,fn); exits 0/2 only; compact JSON |
| P1-W1 | wire wave-1 seams (verdict accept, worktree writes) | 1 | 02, 03, 04 | 5 | merged | v2-P1-W1 / 0986d91 | 0 test lines; checked side by side with oracle |
| P1-07 | sdd core | 2 | 01-04 | 100 | merged | v2-P1-07 / e52cba3 | 92 test lines; 6 goldens (own capture with stderr); fingerprint kept but byte sort (bash used locale sort): note for upgrading.md; refusal texts still say sdd.sh (P5) L; one worker |
| P1-08 | stacks go, python | 2 | 05 | 10 | merged | v2-P1-08 / 2b032b9 | 11 test lines (budget 10, accepted); pytest re-run via Exec on failure |
| P1-09 | stacks jvm, spring-boot | 2 | 05 | 10 | merged | v2-P1-09 / ea16d57 | ~11 test lines (budget 10, accepted); orchestrator fixes: INTEGRATION_MODULE_GLOB via glob.Match, moduleOf clash |
| P1-10 | stacks nextjs, react-native | 2 | 05 | 10 | merged | v2-P1-10 / 3e97937 | 7 test lines; Steps untested beyond Detect; Windows .cmd path only vetted |
| P1-11 | guards A | 2 | 06 | 80 | merged | v2-P1-11 / e4b9a07 | 80 test lines/28 rows; 1 golden (4 cases); guard-git skips resolve-base (follow-up); kept bash bugs: sed -n denied in readonly, one prefix stripped |
| P1-12 | phase/PR guards, git-hook guard | 3 | 05, 06, 07 | 20 | merged | v2-P1-12 / d65e43c | 18 test lines/19 rows; 1 golden; silent outside a kit project (1.x denied); SpecChecker nil until P3-09 |
| P1-13 | lifecycle hooks | 3 | 02, 06, 07, 08 | 10 | merged | v2-P1-13 / da57e44 | 10 test lines; TODO: session-context MEMORY index (P3-06) + role-tier notice; hooks/hooks.json still names .sh (P2-02/P5-01) |
| P1-14 | scenarios A, B, C | 4 | all P1 | 220 | merged | v2-P1-14 / 8067def | 220 test lines (A28 B49 C35 + 108 helpers); skip on Windows (sh test cmds); hook latency median 32.6 ms arm64 record hook latency here |
| P2-01 | setup library | 1 | P1 | 15 | merged | v2-P2-01 / 847463e | 15 test lines; 1 golden byte-identical; kit hook detection also matches 'sdkit hook'; ordered JSON like jq |
| P2-02 | emitters + `dev adapters` | 1 | P1 | 0 | merged | v2-P2-02 / cfe1af2 | --check clean (11 role files x 5 hosts); generated header still names build-adapters.sh (P5) gate: `--check` byte-identical |
| P2-07 | prespec | 1 | P1 | 5 | merged | v2-P2-07 / 5ff9a90 | 10 test lines (budget 5; accepted: two fixture setups, already minimal); backslash names refused |
| P2-03 | init | 2 | 01, 02, P1-08..10 | 15 | merged | v2-P2-03 / 771fad7 | 15 test lines; 1 golden (3 file lists + detect table); hooks call sdkit hook; Codex has no hook wiring (as 1.x); Codex/Gemini/OpenCode Windows unverified (Q3); follow-ups in P2-W1 needs P0-09 settled |
| P2-04 | doctor | 2 | 01 | 10 | merged | v2-P2-04 / b2a8f66 | 10 test lines (trimmed from 25); FAIL lines identical to oracle; pricing check waits for P4-01; plugin vs binary version warn |
| P2-05 | sync + upgrade | 2 | 01 | 10 | merged | v2-P2-05 / 565d1b8 | 14 test lines (budget 10, accepted); upgrade removes ~95 unedited 1.x copies (scripts, hooks, adapters.d, .claude/hooks), keeps edited ones; pre-1.0 layout -> exit 1 |
| P2-W1 | fix P2-03 follow-ups (CI templates, hooks.json, DetectMode, sync stack) | 2 | 03 | 5 | merged | v2-P2-W1 / 128d213 | 0 test lines; CI templates call sdkit sdd ci-check, binary from GitHub Releases + sha256 (version pinned from base ref); hooks.json -> sdkit hook; DetectMode 2.0 copy; sync reads STACK |
| P2-06 | scenario E + Windows smoke | 3 | 03, 04, 05 | 100 | merged | v2-P2-06 / bb2ca3b | ~99 lines (E 60 + smoke 39); smoke not run (no pwsh); spike workflow deleted; 1.x bug kept: upgrade overwrites edited managed copies (lib.go:163) |
| P3-01 | pr | 1 | P1 | 15 | merged | v2-P3-01 / aa244fd | 15 test lines; httptest auth (Bearer ATCTT / Basic) + body; bitbucket via net/http, gh/glab via proc; orchestrator fix: skip bash -n without bash |
| P3-02 | jira | 1 | P1 | 15 | merged | v2-P3-02 / 401aeb7 | 14 test lines; jiratest fake ~43 lines (helper); token in sdkit.env always refused; TODO: audit record before egress after P3-07 |
| P3-03 | spec change + apply | 1 | P1 | 25 | merged | v2-P3-03 / 303b3d7 | 20 test lines; 1 golden byte-identical (7 cases); AC hash kept; specCmds map for P3-09 |
| P3-04 | tasks | 1 | P1 | 15 | merged | v2-P3-04 / 3e065e8 | 12 test lines; byte-identical to oracle on fixture; ids pinned; order = first ready in file order (bash used platform tsort); needs sdkit spec approved-sha (P3-03/09) |
| P3-05 | contract + hook | 1 | P1 | 10 | merged | v2-P3-05 / d17afec | 10 test lines; check-contract on PreToolUse Agent/Task + SubagentStop; workflow.Pending wired; telemetry deferred to P4 |
| P3-06 | memory | 1 | P1 | 5 | merged | v2-P3-06 / dbbbeb3 | 7 test lines (budget 5, accepted); session-memory SessionStart hook prints MEMORY=1 index (P1-13 TODO closed) |
| P3-07 | recover, redact, audit | 1 | P1 | 10 | merged | v2-P3-07 / ee3d901 | 10 test lines; 1 golden; chain byte-compatible both ways with oracle; audit policy checks embedded kit only; recover execs sdkit contract/dispatch/route (route nil seam P4) |
| P3-08 | adapter + run-role | 1 | P1 | 15 | merged | v2-P3-08 / 9fc729e | 14 test lines; 1 golden (claude+codex dry-run); exit codes kept (+124 timeout); seams in internal/runrole/seams.go (Router P4-08, Budget P4-05, Contract, Envelope); adapter.Audit/Redact nil -> litellm refuses L |
| P3-10 | review | 1 | P1 | 10 | merged | v2-P3-10 / f7f7adf | 11 test lines (budget 10, accepted); review.Status for P3-09; hash matches oracle |
| P3-12 | dispatch | 1 | P1 | 5 | merged | v2-P3-12 / 514cea1 | 5 test lines; own overlap helper (plan.conflict unexported); recover must call dispatch.Release (P3-07) |
| P3-W1 | wire P3 seams (run-role contract/audit/redact, jira egress audit) | 2 | 02, 05, 07, 08 | 5 | merged | v2-P3-W1 / 7a1f3c6 | 0 test lines; jira egress audit checked (chain ok); run-role contract only dry-run checked; litellm audit unchecked -> local E2E |
| P3-09 | spec | 2 | 01, 03, 10 | 15 | merged | v2-P3-09 / f712630 | 15 test lines; spec check folded into existing golden (24 files); SpecChecker supplied; approved-sha via ls-tree (Windows-safe) supplies `SpecChecker` |
| P3-11 | managed sdd | 2 | 01, 02, P1-07 | 0 | merged | v2-P3-11 / 7426b2c | 0 test lines (scenario D); hand flow vs jiratest ok; seam sdd.SpecApprovedSHA (wire after P3-09); transitions shows SPECS_DIR from env only; SDD_JIRA_CMD dropped covered by scenario D |
| P3-13 | scenario D | 3 | P3 | 70 | merged | v2-P3-13 / 4d0dfb9 | 67 test lines; covers start --ticket refusals, specs dir deny, phases, pr open via fake gh, evidence-sync idempotent, block/revalidate; orchestrator fix: jiratest mutex decision point: `2.0.0-beta` (D6, user) |
| P4-01 | pricing | 1 | P3 | 10 | merged | v2-P4-01 / 672915b | 6 test lines; Lookup unknown -> false (N/A); pricing.Check/Print + RefreshPricing for doctor (wire in P4-W); HTTP fetch path not exercised |
| P4-02 | telemetry | 1 | P3 | 5 | merged | v2-P4-02 / 523d932 | 5 test lines; sdd.Tel wired; missing emits: workflow.decision, contract.check, route (P4-W); summary/list/archive not ported (P4-06?) |
| P4-03 | digest, lookup, knowledge | 1 | P3 | 5 | merged | v2-P4-03 / b6f2b80 | ~8 test lines (budget 5, accepted); Envelope seam wired; lookup sha1 keys + knowledge sha256 kept (1.x caches stay valid) |
| P4-04 | usage | 2 | 01 | 15 | merged | v2-P4-04 / f8f50f3 | 14 test lines; 1 golden (full + N/A); Budget seam wired (stop before spawn); pricing via Lookup only (provider/date windows dropped); live pull (Codex) not ported L |
| P4-05 | route | 3 | 02, 04 | 25 | merged | v2-P4-05 / 6f37ef0 | 25 test lines (8 rows: floors, mandatory gates, BLOCK, retries); 1 golden byte-identical; Router API for P4-08; reservationsLister wired; workflow.decision emitted L |
| P4-06 | report | 3 | 02, 04 | 10 | merged | v2-P4-06 / 8f50ea8 | 10 test lines; 1 golden (finalize, task full/missing, sessions, history); sdd.Rep wired; value-report hook SessionStart/SessionEnd; telemetry.Summarize partial L |
| P4-07 | usage and budget hooks | 3 | 02, 04 | 5 | merged | v2-P4-07 / 2913b71 | 9 test lines (budget 5, accepted after trim); value-report moved to P4-06; NOT ported: AGENT_<ROLE>_TIER model check + ROUTING=on policy check in guard-budget -> P4-W |
| P4-08 | run-role integration | 4 | 05 | 0 | merged | v2-P4-08 / 969c804 | 0 test lines; exit 7 (reuse, CLI not called), exit 8 (security BLOCK), ROUTING off unchanged — by hand; retry/escalate not run live |
| P4-W | P4 wiring: doctor pricing, guard-budget tier + ROUTING checks, contract/workflow telemetry | 4 | 01, 02, 05, 07 | 5 | merged | v2-P4-W / 9ff5d86 | 0 test lines; doctor pricing block matches oracle; guard-budget tier (deny) + ROUTING (ask) checks; workflow.decision + contract.check events; hook returns first problem only |
| P5-01 | rewrite references | 1 | P4/P3 | 0 | merged | v2-P5-01 / e5a0883 | 108 files (skills 26, core 12, templates 13, generated 56); allow-lists -> Bash(sdkit *); B1-B16 handled; leftovers: emitter header, core/*.env, adapters.json, roles README (P5-04) |
| P5-03 | packaging + CI templates | 1 | P4/P3 | 0 | merged | v2-P5-03 / 1798756 | brews/scoops to dist/ (skip_upload); Formula + Scoop templates; pr-check.yml ported; Windows GH Actions example; snapshot build unverified (no goreleaser) |
| P5-07 | `sdkit dev scan` (port packaging/scan-publishable.sh; no card had it) | 1 | P4 | 5 | merged | v2-P5-07 / 081b124 | 11 test lines (budget 5, accepted); parity with bash; tree: 5 fake keys in security golden + 1 URL cred in pr_test (fix in P5-W); history: same + 6 author identities |
| P5-W | make `sdkit dev scan` clean: build fake secrets at test time, allow-list test URL | 1 | 07 | 5 | merged | v2-P5-W / 641c7f8 | fake keys split with <> markers in security golden; one allow entry for the devscan test URL |
| P5-02 | delete the bash | 2 | 01 | 0 | merged | v2-P5-02 / 24c0c9e | deleted bin, scripts, tests (61), tools/golden, capture .sh; bench -> attic/bench; verify + sdd fixtures now Go (+43 net); left .sh: 14 *.sh.example; tests still call sh in TestSDDGolden/capped/guards |
| P5-04 | docs, rules, counts | 3 | 02 | 0 | merged | v2-P5-04 / 72c42c6 | 26 files; counts: 8 skills, 11 roles, 20 hooks, 41 subcommands; gate grep clean; design docs keep 1.x history notes |
| P5-05 | versions + upgrade guide | 4 | 04 | 0 | merged | v2-P5-05 / 3af62d2 | 2.0.0 in 5 places (user approved; v2 only, no tag); upgrading.md 2.0 section with every collected note; host wiring/CI pin wording to be checked in P5-06 |
| P5-W2 | verifier BLOCK fixes: sdd.sh/init.sh in Go messages, upgrading.md claims, Windows line | 5 | 06 | 0 | merged | v2-P5-W2 / d9ff7d7 | 32 messages -> sdkit sdd/init; 6 sdd goldens wording only; upgrading.md 3 claims corrected; Windows line fixed |
| P5-06 | final verification | 5 | 05 | 0 | merged | v2 | verifier BLOCK -> P5-W2 -> PASS; report docs/go-port/out/final.md (git-ignored); stops for the user's yes |

## Phase gates

| Phase | Exit criteria checked | By | Result |
|---|---|---|---|
| P0 | [x] | orchestrator (mechanical checks) | green: build, vet (darwin/windows/linux), 7 pkgs pass, version/where/help ok, stdlib only, both docs present; Windows CI run pending (Q3) |
| P1 | [x] | orchestrator (mechanical) + scenarios A–C | green: go test -race all pkgs, GOOS=windows vet clean; gate edit refused before RED / allowed after (scenario A); pre-push refused before verified (A); malformed JSON exit 0 no output; scenarios skip on Windows (sh test cmds) — Windows smoke P2-06. hook latency: median 32.6 ms (min 25, first run 640), arm64 Mac |
| P2 | [x] | orchestrator (hand run) + scenario E | green: go test -race, adapters --check clean; scratch repo: init 5 hosts ok, doctor only FAIL = project.md TODOs; low-risk task start→verified with gate refusing code before RED. windows-smoke.ps1 + CI step exist, never run (no pwsh; push owed, Q3) |
| P3 | [x] | orchestrator (mechanical) + scenario D | green: scenario D (-race), run-role --dry-run 5 hosts table, pr host + Bitbucket auth (httptest), spec check + pre-push spec gate (hand, P3-09), no curl/jq/awk exec (python3 only as the python stack's interpreter). go test ./... 17 s after parallel scenarios. Unverified: run-role contract refusal on a real run, litellm audit (local E2E) |
| P4 | [x] | orchestrator (mechanical + hand) | green: go test ./... ; missing ledger records print N/A (usage + report goldens); ROUTING=on → exit 7 with valid evidence (CLI never called), exit 8 on a standing BLOCK, ROUTING off unchanged (P4-08 by hand); budget stops before the spawn (usage Check before launch; guard-budget strict asks); no verification shortened. Not run live: route retry/escalate, pricing HTTP fetch |
| P5 | [x] (except user steps) | orchestrator + independent verifier (PASS after P5-W2) | checklist ticked except Windows smoke and user confirmations; see out/final.md |

## Test budget

| Phase | Budget (lines) | Added so far |
|---|---|---|
| P0 | ~110 + helpers ≤150 | ~129 test lines (excl. package/import) + testutil 136 |
| P1 | ~640 | ~606 (per-task reports, excl. package/import) |
| P2 | ~150 | ~163 (small accepted overruns: P2-05 +4, P2-07 +5) |
| P3 | ~210 | ~200 (per-task reports) |
| P4 | ~70 | ~77 (P4-03 +3, P4-07 +4 accepted) |
| Total | ~1,200 + helpers | 0 |

## Open questions for the user (add here when a worker raises one)

- Q1 (P0-07) **answered: added to 02-contracts.md as is.** `02-contracts.md` omits existing 1.x behaviour. Proposal: add it as written (documenting, not changing): run-role exit 6 (evidence answer not recorded); optional `sdd status` fields (`profile=`, `verdict=`, `evidence=stale`, `ticket= spec=`, `blocked=`); analyst-skill step line form; Stop `{decision:block}` and SessionStart `additionalContext`/`systemMessage` hook outputs; `VERIFY_CHANGED_SRC`; `advance` refusals on stderr vs `gate` on stdout; `sdd` exit 4 `no active task`. Details: `contracts-inventory.md` §10.
- Q2 (P0-09) **answered: support all three (sh, PowerShell, cmd); see 03-cross-platform.md item 2. Done in P0-05b.** `03-cross-platform.md` item 2 defaults user command strings (`VERIFY_CMD`, ...) to `cmd /C` on Windows; no agent host uses cmd (Claude Code: Git Bash, else PowerShell; Cursor/Gemini: PowerShell). Keep `cmd /C`, or switch the default to PowerShell (Git `sh` when present)?
- Q3 (P0-09) **answered: not now (P1); before P2-03: continue without a push; Codex/Gemini CLI/OpenCode Windows hook wiring marked unverified until the Windows CI run or the local end-to-end run.** running `.github/workflows/spike-windows.yml` needs a push of `v2` (user). Blocks P2-03 for Codex, Gemini CLI, OpenCode.
- Fact (P0-09): Claude Code on Windows without Git Bash treats exit codes other than 0/2 as errors, and sends `C:\...` paths; hooks exit only 0/2 and guards normalise backslashes for every host.

Note: task branches are `v2-<ID>` (git cannot hold `v2` and `v2/<ID>` at once).

## User requirements

- Before 2.0 is called done (P5-06): run the built `sdkit` locally end to end on this Mac, **through the skills in a real Claude Code session** (not only the CLI), on a scratch repo. Record each row's result in the P5 gate row.
  1. Setup: `go build`, binary on PATH, `/scaffold-init` (calls `sdkit init`), `/scaffold-doctor` (calls `sdkit doctor`).
  2. Analyst: a pre-spec → `/generate-specs` (PRD, ADRs, reviewed spec, PR) → `/update-specs` after an edit → `/merge-specs` for two sibling changes → `/generate-tasks` from the merged spec (Jira subtasks; needs a test Jira project and credentials from the user, else the fake Jira of scenario D).
  3. Developer: `/spec-driven-dev` on (a) a free-text low-risk task, (b) a Jira subtask from step 2 (managed mode), (c) a high-risk task: risk classification, plan, proposed → red → green → verified, spawned verifier + reviewer + security-auditor via `run-role` with registered evidence, PR through `sdkit pr`.
  4. Enforcement seen live: guard-phase denies an out-of-phase edit, guard-pr denies `_evidence` from the shell, git pre-commit/pre-push refuse, stop-verify blocks "done" on a broken build.
  5. `/incident-to-rule` turns one failure from step 3 into a rule.
  Hosts: Claude Code fully; Codex, Cursor, Gemini CLI, OpenCode at least `scaffold-init` + one guarded edit each, where installed.

- Q4 (P3-13) **answered: fix it; done (`internal/sdd/attest.go` excludes `audit.jsonl`).** every Jira call appends to `<project-dir>/audit.jsonl`, which is neither git-ignored nor excluded from the diff fingerprint, so in managed mode a Jira call makes the evidence stale. 1.x had the same behaviour (`adapters.d/jira.sh` + `sdd.sh` fingerprint excludes only `.sdd` and `attestations`). Proposal: exclude `audit.jsonl` from the fingerprint (one pathspec) and note it in upgrading.md. Needs the user's yes (bug fix, D11 fingerprint note).
- Q5 (P3) **answered: continue to P4, no tag.**
- Q6 (P3) **answered: golden cap raised to 30 (05-workflow.md §4).**

## Upgrade notes collected for `docs/upgrading.md` (P5-05)

- envfile: `$NAME` expands only from settings read earlier; process variables such as `$HOME` no longer expand (P0-02).
- store: a lock wait gives up after 30 s with an error; 1.x waited forever (P0-03).
- verify: `.sh` policies in `verify.d/` run through `sh` without the executable bit; `chmod -x` no longer disables one (rename it `_x` instead). Passing steps no longer echo their output (P1-05).
- attest fingerprint: `<project-dir>/audit.jsonl` is no longer part of the diff fingerprint, so a Jira or litellm call does not make evidence stale (Q4).
- attest fingerprint: same algorithm, but file names sort by byte (C locale); 1.x used the locale's `sort`, so fingerprints of mixed-case paths under a non-C locale differ once (P1-07, D11).
- usage: prices resolve by model id from `pricing.json` only; provider- and date-windowed rates and the flat v1 table are gone; `usage live pull` (Codex rollout) is not ported; invalid JSON lines are skipped instead of failing the report (P4-04).
- hooks: the Claude Code JSON is compact, not pretty-printed (P1-06).
- managed mode: `SDD_JIRA_CMD` is gone; Jira calls use the built-in client (`JIRA_BASE_URL` can point at a bridge) (P3-11).
- `guard-phase` and `guard-pr` do nothing in a repo without `.sdkit/sdkit.env`; 1.x denied there (P1-12).
- known 1.x bug kept (fix after 2.0, or before if the user asks): `upgrade` re-runs `init`, which records the hash of a locally edited managed copy (`internal/setup/lib.go` `InstallManaged`), so the next `sync` overwrites the edit (P2-06).
- bash quirks kept on purpose, fix after 2.0: `guard-readonly` denies `sed -n`; strips only one `env`/`sudo` prefix; `guard-secrets` matches the raw path (P1-11). `advance red` only needs a non-zero exit (P1-07).

## Cut from 2.0 (D6, D7)

- F10 `scripts/test-select.sh` + `tests/test-map.conf` (D12: `go test ./...` runs whole).
- F11/F20 `bench/` harness (costbase, tbench, history): kept as is, unmaintained, out of the build (D5).
