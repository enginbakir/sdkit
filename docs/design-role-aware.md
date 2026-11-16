# Role-aware SDKit: shared contracts (vNext)

> **History.** This record was written for the 1.x (bash) kit. Commands are shown by their 2.0 names (`sdkit <command>`); the `tests/*.test.sh` suites, `bench/` and `jq` it names belong to 1.x (2.0 replaced the suites with a small Go test set, and the harness lives in `attic/bench/`, unmaintained). The design and the decisions still stand; see [guide.md](guide.md) for how 2.0 is built.

Status: design contract for the work packages F01-F14. Agents implement against this file; change it only through the orchestrator.

## Environments

`SDKIT_ENV=standalone|analyst|developer` in `<project-dir>/sdkit.env` (shared, persisted so `sdkit upgrade` keeps it). Default and absent value: `standalone` = today's behaviour, unchanged. `sdkit init --env <v>` sets it. "role" always means a subagent role; the install persona is called env.

- analyst: a `project-specs` repo. No stack; openspec is installed because generate-specs outputs an OpenSpec change into `specs/<id>/`. Skills: generate-specs, update-specs, generate-tasks. Gitignored pre-spec dir `prespec/` (`PRESPEC_DIR`, default `prespec`).
- developer: a source repo. Approved specs are a git submodule at `specs/` (read-only for the developer). Skill: spec-driven-dev in managed mode.

## Spec metadata (F06)

`specs/<id>/spec.meta.json`:
`{"id","version","status":"draft|approved","contentHash","review":{"verdict","contentHash","round","at"},"history":[{"version","sha","at"}]}`
- `contentHash` = sha256 over the sorted spec files of `<id>` (path + bytes). Review evidence is valid only if `review.contentHash == contentHash`.
- Immutable technical reference = git commit SHA. Version is a label.
- Versions: 0.1 first draft, 0.N pre-approval updates, 1.0 first approved, 1.N compatible, N.0 breaking.
- 1.0 promotion: the approval PR's last commit sets version 1.0 / status approved; the human merge is the approval. generate-tasks requires: the PR is merged, the spec on `main` at the given SHA has status approved, and the SHA is reachable from `origin/main`. Nobody commits to `main` directly.
- Atomic publish: the version is recorded in a meta commit BEFORE push and PR (the PR always has a diff, the meta travels with the spec); a failed push rolls the meta commit back; a failed PR step leaves the recorded version and a rerun only opens the PR. Same input twice = no new version.
- Branch: `spec/<id>` (deterministic), reused if present. After approval a change lives on `spec/<id>--<slug>` and under `specs/<id>/changes/<slug>/`; the base `contentHash` excludes `changes/` (and a change's `change.json`); see [design-spec-merge.md](design-spec-merge.md). PR is created once (look up by head first), later runs update it. Never force-push.

## Review adapter (F04), `sdkit review`

EGM is a skill plugin (`egm@egm`), not a CLI. The skills (generate-specs/update-specs) call EGM `egm-review-loop` with `TargetFile, SemanticTarget, Reviewers, Standards, GoldfishContext` taken from `<project-dir>/review.d/<artifact>.env` (rubric per artifact: REVIEWERS, STANDARDS, CONTEXT). `sdkit review` does only the deterministic parts:
- `sdkit review plan <artifact>`: print the resolved reviewers/standards/rubric.
- `sdkit review begin <id>`: record contentHash, round counter; refuse past `SPEC_REVIEW_MAX_ROUNDS` (default 4).
- `sdkit review record <id> --verdict PASS|BLOCK|PARTIAL|NOT_RUN --isolation fresh|none --findings <file>`: store evidence bound to the current contentHash. `isolation=none` can never yield PASS.
- `sdkit review check <id>`: exit 0 only if the latest evidence is PASS, isolation=fresh and its hash equals the current contentHash (stale evidence rejected).
- `sdkit review accept <id> <finding-id> [--who] --why` (`--who` defaults to `JIRA_USER`; `begin <id> --final` opens one verification round outside the cap, once per id): human exception, keyed on finding id + text hash (editing the finding voids it), append-only `acceptances.jsonl`.
Findings: `{id, severity: BLOCKER|MAJOR|MINOR, evidence, disposition: open|fixed|accepted}`. BLOCK if any open BLOCKER/MAJOR. Reviewers never approve what they generated: generation and review are separate subagents.
If the host has no fresh-subagent facility the verdict is NOT_RUN, never PASS. Open risk: EGM `egm-review-loop` has `disable-model-invocation: true`; whether another skill may invoke it is unverified.

## Task contract (F07/F08)

Required Jira subtask fields, rendered as a fenced `sdkit-task` JSON block in the description (machine-readable, tolerant of Jira rendering):
`externalId` (`<specId>:<taskKey>`, upsert key), `specId`, `specVersion`, `specSha`, `acceptanceCriteria[]` (spec AC ids), `risk` (low|medium|high), `dependencies[]` (Jira keys), `parent`; optional `scope[]` (paths/globs; the analyst does not set it, the developer finds files in the repo).
Missing/empty required field = intake rejected. Never auto-assign.

`sdkit jira` verbs: `fetch <KEY>` (normalised JSON on stdout), `deps-ready <KEY>`, `upsert <externalId> <json>`, `comment <KEY> <text>`, `transition <KEY> <status>`, `change-request <KEY> <json>`. Backend is the built-in Jira client (`internal/jira`) (mockable; opt-in egress `JIRA_EGRESS=allow`, credentials by env var name only). Exit codes: 0 ok, 2 usage, 3 egress/auth, 4 contract invalid, 5 not found. A failed call must never produce success evidence.

## Managed execution (F09/F10/F12)

`SDD_MANAGED=on` (set by `SDKIT_ENV=developer`). `sdkit sdd start --ticket KEY` stores `ticket, specSha, specId, deps`. No second state machine: phases stay `intake→proposed→red→green→verified→pr→done`.
- advance proposed: contract complete, `specs/` submodule HEAD (or the pinned SHA via `git -C specs cat-file`) resolves the `specSha`, deps ready.
- advance verified: re-check specSha still resolvable; evidence fingerprint rules unchanged.
- Spec defect: orthogonal flag `.blocked={kind:"spec-change",ref}` in `.sdd/state.json`; advance/gate refuse while set; `sdkit jira change-request` creates the Jira item. Resume only after the pinned SHA changes to an approved revision and `sdkit sdd revalidate` re-runs intake checks.
- PR targets `sdkit base` (release branch). `spec-driven-dev` calls no OpenSpec skill in any mode; standalone writes the proposal from the approved plan.

## File ownership (no file has two writers)

- A (F01,F02): sdkit init, sdkit doctor, bin/sdkit, sdkit upgrade, templates/sdkit.env, templates/sdkit.gitignore, templates/prespec/*, tests/env.test.sh
- C (F04): sdkit review, core/roles/spec-reviewer.md (+ generated agents via build-adapters), templates/review.d/*, tests/review.test.sh
- D1 (F07/F08 core): adapters.d/jira.sh, sdkit jira, tests/jira.test.sh, tests/stubs/jira-*
- B (F03,F05,F06): skills/generate-specs, skills/update-specs, sdkit spec, sdkit pr open, tests/spec.test.sh
- D2 (F07 skill, F11): skills/generate-tasks, tests/tasks.test.sh
- E (F09,F10,F12): sdkit sdd, skills/spec-driven-dev/*, tests/managed.test.sh
- F (F13,F14): docs/*, README.md, tests/test-map.conf, count sync, regression/compat tests
