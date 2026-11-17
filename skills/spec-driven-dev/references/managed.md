# Managed mode (`SDD_MANAGED=on`)

Read at step 0 when `<project-dir>/sdkit.env` has `SDD_MANAGED=on` (set by `SDKIT_ENV=developer`). The analyst side already wrote and approved the spec and split it into Jira subtasks; this repo implements one subtask. The phases and every gate stay the same (`intake → proposed → red → green → verified → pr → done`); managed mode only adds checks. Contract: `docs/design-role-aware.md`, sections "Task contract" and "Managed execution".

What does not happen here: no PRD, architecture or spec is written or regenerated, and nothing under `SPECS_DIR` (default `specs/`, the read-only approved-specs submodule) is edited (`<sdd> gate edit` denies it).

## Steps mapped to Jira

| Step | Managed mode |
|---|---|
| 0. Intake | `<sdd> start --ticket <KEY> [--risk <label>] [--profile ...]`. It fetches the subtask (`sdkit jira fetch`), validates the task contract (`sdkit jira validate-task`), checks that the pinned `specSha` is a commit in `SPECS_DIR` and that the submodule's HEAD contains it, and that every dependency is done (`sdkit jira deps-ready`). Any failure refuses the start and says why (a missing field, an unknown revision, the open dependencies). The risk comes from the task; `--risk` can only raise it, the risk floor still applies. Show the user the ticket, the spec revision and the risk before you continue. |
| 0b. Role | A subtask carries `area` (backend, frontend, qa...): show it with the ticket and stay inside that role's part (a backend subtask does not edit the UI; the contract with the other roles is in the task and in `design.md` `## Roles` at the pinned revision). A group task (`group: true`) is refused: only its subtasks are implemented. |
| 1. Explore | Read the spec at the pinned revision (`git -C <SPECS_DIR> show <specSha>:<path>`) and the acceptance criteria the task names; `scout` for the code. |
| 2. Plan gate | No interview and no `plan.md`: the task's acceptance criteria are the plan; you find the files in the repo (the task's `scope`, if one is set, is a hint). Show the user one screen (ticket, spec revision, risk, the files you expect to touch, the ACs, dependencies) and ask once: go on, or stop. Anything beyond what the ACs need is a question for the user, not a silent extension. Only `high` needs more: write `design.md` with the race/ordering/timing scenario (the gate requires it), with the `engineer` role if the code is not obvious. |
| 3. Propose | Nothing is proposed. `start` wrote `<project-dir>/work/<KEY>/proposal.md` with one `D<n>: <AC id>: <the spec's WHEN/THEN> \| Verified by: automated test` line per acceptance criterion, taken from the approved spec at the pinned revision. Run `<sdd> advance proposed` (it re-fetches the contract, re-checks the revision and the dependencies, runs `sdkit dod check`). Edit a line only to name a more precise Verified-by method; a condition that cannot be tested as written is a spec defect (below), not something to rewrite here. |
| 4-6. Implement, verify | Unchanged. `<sdd> advance verified` also re-checks that the pinned `specSha` still resolves; the attestation records `ticket` and `specSha`. |
| 7. PR | Push and open the PR against `<diff-base>` (`sdkit base`: the release branch with `BASE_BRANCH_MODE=release`), then `<sdd> advance pr` (it records the PR head). Then `<sdd> evidence-sync --pr <url>`: the evidence summary and the PR link as a Jira comment, then the status (`JIRA_REVIEW_STATUS`, default `In Review`; `--status none` to leave it). Safe to re-run: the same evidence is sent once, and a failed call records nothing. |
| 8. Closeout | After the merge: `<sdd> done --merged` (= `advance done`): it checks that the PR head is in the resolved base and refuses otherwise, so done is never declared. A squash merge is not detected: tell the user and let them confirm in Jira. The rest of §8 (memory, telemetry, work dir, worktrees) as usual. |

## Spec defect: block, change request, revalidate

When the spec is wrong, contradictory or not implementable as written (a spec gap in §6 terms), do not work around it and do not edit `SPECS_DIR`:

1. `<sdd> block --spec-change --reason "<what is wrong>" [--section <spec section>] [--proposal "<suggested fix>"]`. It files the change request in Jira (`sdkit jira change-request`, linked as blocking the subtask) and only then sets the block; when the call fails it exits non-zero and nothing is blocked: fix the Jira access and run it again. It also moves the subtask to `JIRA_BLOCKED_STATUS` (default `Blocked`) when that transition exists (a warning otherwise).
2. While blocked, every `<sdd> advance` and `<sdd> gate` (code edits, push, PR) refuses. Tell the user the change request key and stop.
3. Resume only when the analyst side has approved and merged the revised spec AND updated the subtask to pin the new revision: update the submodule (`git -C <SPECS_DIR> fetch`, then `git -C <SPECS_DIR> checkout <new sha>`, and commit the pointer), then `<sdd> revalidate --sha <new sha>`. It refuses the same SHA, a revision `sdkit spec approved-sha` does not confirm, and a contract that still pins the old one; it re-runs the intake checks, regenerates the DoD (the old one is kept as `proposal.md.prev`), drops every verdict, evidence record and the attestation of the old revision, and returns the task to `intake`, then runs the proposed gate again. Re-apply your DoD refinements and continue from §3; RED and verification run again.

`<sdd> status` shows `ticket=`, the pinned spec and `blocked=` while a block is set; `<sdd> transitions` lists the managed additions.
