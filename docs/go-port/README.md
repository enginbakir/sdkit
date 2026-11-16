# SDKit Go port: orchestrator brief

You are the **orchestrator** of a rewrite: the kit's bash runtime becomes **one Go binary, `sdkit`**, for macOS, Linux and native Windows (PowerShell or cmd; no WSL, no Docker, no Git Bash needed by the kit). The bash is deleted at the end, not kept next to the Go code. Tests are **rewritten from scratch and kept very small** (`04-testing.md`).

Nothing in this directory is implemented yet. Read the repository state before acting: if a task is already done, verify its exit criteria cheaply, mark it done and move on.

## Read first, in this order

| # | File | Why |
|---|---|---|
| 1 | `/CLAUDE.md`, `/CONTRIBUTING.md` | Repo rules. They bind every worker. Rules about bash style and bash tests are replaced by `05-workflow.md` and `04-testing.md` on branch `v2` |
| 2 | `00-decisions.md` | Settled and open decisions. Use the defaults unless the user changed them |
| 3 | `01-architecture.md` | Package layout, command map, code rules |
| 4 | `02-contracts.md` | What must not change |
| 5 | `03-cross-platform.md` | Rules every worker applies from the first line of code |
| 6 | `04-testing.md` | Test budget and the allowed test shapes |
| 7 | `05-workflow.md` | Branches, worktrees, worker loop, merge, commits |
| 8 | `STATUS.md` | The live task table. **You maintain it** |
| 9 | `phases/P0-foundation.md` … `P5-cutover.md` | Task cards with waves, dependencies, write sets |
| 10 | `06-release.md`, `07-risks.md` | Needed from P2 on and before P5 |

## How to run it

1. Create the integration branch `v2` from `master` (`05-workflow.md` §1). Never commit to `master`.
2. Work phase by phase (`P0` to `P5`). A phase is done when every task in it is merged and its exit criteria hold (`phases/Pn-*.md`).
3. Inside a phase, work in **waves**. A wave is a set of tasks whose dependencies are done and whose `writes:` sets are disjoint. Start every task of a wave **at once as parallel subagents, each in its own git worktree** (`05-workflow.md` §2). Do not start a later wave until the earlier wave is merged into `v2` and `go build ./... && go vet ./...` is green there.
4. Hand a worker only its task card plus the files named in "Read" below. Do not forward this conversation. Use the handoff template.
5. Workers never merge. You merge task branches into `v2` in wave order, run the gates (`05-workflow.md` §5), then update `STATUS.md`.
6. Between phases, run the phase exit criteria yourself (or with one verifier subagent that did not write the code) and record the result in `STATUS.md`.

Do not run more subagents than the wave has tasks. Do not spawn a worker for a one-file fix you can do in a minute.

## Worker handoff template

Fill the fixed lines first, the changing lines last.

```
ROLE: developer (Go). One task. Do not touch files outside WRITES.
REPO RULES: /CLAUDE.md; docs/go-port/01-architecture.md, 02-contracts.md, 03-cross-platform.md, 04-testing.md, 05-workflow.md (§3 is your loop).
TASK: <ID and title>   CARD: docs/go-port/phases/<file>.md#<anchor>
WORKTREE: <path>   BRANCH: v2-<ID>   BASE: v2
WRITES: <the card's write set, nothing else>
READ (the bash source is the specification): <the card's source files>
DEPENDS ON (already merged): <task IDs>
TEST BUDGET: <lines>   GOLDENS: capture before you delete any bash (05-workflow.md §3)
DONE WHEN: <the card's "Done when">
REPORT: at most 15 lines: files changed, tests added (count, lines), goldens captured, anything you could not do or decided differently, and every contract (02-contracts.md) you had to touch. Long output goes to docs/go-port/out/<ID>.md (git-ignored).
```

## When to stop and ask the user

- A task needs a change to `02-contracts.md`, or its result contradicts a "settled" decision in `00-decisions.md`.
- An open decision in `00-decisions.md` blocks the next wave and has no default.
- A worker exceeded its test budget, or a golden mismatch cannot be explained as an intended change.
- The Windows spike (`P0-09`) shows a host cannot run `sdkit` hooks on native Windows.
- Anything that needs credentials, a paid call, a tag, a release, a push or a version bump. Those are never done by default.

## What you must not do

- Do not weaken a gate to make a test pass. Do not turn missing evidence into PASS.
- Do not add a Go dependency without asking (`00-decisions.md` D8).
- Do not port the old shell tests. Do not run `tests/old/`.
- Do not commit to `master`, tag, bump versions or push unless the user asks. Commit on `v2` and task branches only, one logical change per commit (`core/rules/git.md`).
- Do not edit generated files by hand (`agents/`, `adapters/*/agents`): they come from `sdkit dev adapters` once it exists, and from `bin/build-adapters.sh` before.

## Directory map

```
docs/go-port/
  README.md             this brief
  STATUS.md             live task table (orchestrator updates)
  00-decisions.md       decisions, defaults, open questions
  01-architecture.md    layout, command map, code rules
  02-contracts.md       what must stay byte- or behaviour-compatible
  03-cross-platform.md  Windows and portability rules
  04-testing.md         test budget and shapes
  05-workflow.md        branches, worktrees, loop, merge, commits
  06-release.md         artifacts, packaging, CI gate, cutover checklist
  07-risks.md           risks and mitigations
  phases/P0-foundation.md   P1-enforcement-core.md   P2-setup.md
         P3-integrations.md  P4-cost-measurement.md   P5-cutover.md
```
