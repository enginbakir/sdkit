# Workflow: branches, worktrees, worker loop, merge

## 1. Branches

| Branch | Purpose |
|---|---|
| `master` | 1.x. Critical fixes only during the port. Never receives Go work before the cutover |
| `v2` | integration branch, created from `master`. Only the orchestrator merges into it |
| `v2-<TASK-ID>` | one branch per task, created from the tip of `v2` when its wave starts |
| `bash-final` (tag, P5) | the last commit with the bash runtime |
| `v1` (branch, P5) | critical fixes for 1.x after the cutover |

Tasks of the same wave never share a file (their `writes:` sets are disjoint), so merging them in any order does not conflict. If two tasks do conflict, the cards are wrong: fix the card, do not resolve it silently.

Untracked `Implementation Roadmap.md` and `Spec Merge Plan.md` belong to the user; do not move, commit or delete them.

## 2. Worktrees and parallelism

- One task = one worktree = one worker. Create it from `v2`: `git worktree add ../go-port-<ID> -b v2-<ID> v2`. (Inside this repo you may use the Orca CLI instead of raw `git worktree`.)
- A wave starts only when its dependencies are merged into `v2`. All tasks of a wave start together.
- Workers run only the tests of their package (`go test ./internal/<pkg>/...`) plus `go build ./... && go vet ./...` in their worktree. They do not run the whole suite per edit.
- A worker never merges, rebases `v2`, pushes, tags, or edits `STATUS.md`.
- After a wave: remove its worktrees (`git worktree remove`) once the branch is merged.

## 3. The worker loop (per task)

1. **Read** the card, the contracts it names, and the bash source it lists, **from the oracle worktree** `../go-port-oracle` (a read-only checkout of `master`, created once by the orchestrator; bash files deleted on `v2` stay readable there). The bash is the specification: behaviour beats this document where they differ, unless the card says otherwise. Read the existing shell tests only for the refusals and bypass attempts worth keeping as table rows.
2. **Capture goldens first** (only for the cases the card lists): `tools/golden/capture.sh <case-spec>` runs the **bash** implementation and writes `testdata/golden/<pkg>/<case>.txt`. This must happen **before** the bash file is deleted, and it is the only oracle the port has.
3. **Write the Go** in the card's `writes:` set. Follow `01-architecture.md` code rules and `03-cross-platform.md`.
4. **Write the tests** within the card's budget and shapes (`04-testing.md`). Run them.
5. **Delete the ported bash file(s)** listed on the card's "Delete" line in the same commit as their Go replacement. Shared bash libraries (`env.sh`, `lib.sh`, `shared.sh`) go only when the card says so. The old shell test suites are **not** touched per task: P5-02 deletes them all. `v2` is never run as a hybrid: bash left on `v2` may call a script that is already gone, and that is accepted, because goldens come from the oracle and the Go gates are the only gates on `v2`.
6. **Check**: `gofmt -l .` empty, `go build ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, the package tests.
7. **Commit** on the task branch: `feat(<pkg>): <what>` (imperative, at most 72 characters, `core/rules/git.md`). One logical change per commit; goldens and tests in the same commit as the code they check.
8. **Report** (15 lines at most, template in `README.md`).

A worker that finds the bash behaviour wrong keeps the behaviour, and lists the bug in its report. Fixing bugs is not part of the port.

## 4. Goldens

`tools/golden/capture.sh` (bash, maintainer-only, deleted in P5-02) is built in `P0-08`. A case spec is a small file: working fixture (a scratch repo built by a script), command line, stdin, environment. The tool runs the bash version **from the oracle worktree** (`--bash-root ../go-port-oracle`, never from `v2`, so it keeps working after the bash is deleted there) and records command, stdin, stdout, exit code. `internal/testutil/golden` replays the same spec against the Go binary in `go test` and compares. Rules:

- Only decision output and text from `02-contracts.md`. Never whole-file dumps.
- Normalise volatile parts (timestamps, temp paths, hashes) with the placeholders the tool defines.
- A golden that differs because the new behaviour is intended is updated by the worker with a line in the report; an unexplained difference is a bug.
- At most 30 golden files in the repo (raised from 25 by the user, Q6).

## 5. Merge gates (orchestrator, per task branch, in wave order)

1. Branch contains only files in the card's `writes:` plus the listed deletions (`git diff --name-status v2...v2-<ID>`).
2. `gofmt -l .` empty; `go build ./...`; `go vet ./...`; `GOOS=windows go vet ./...`; `GOOS=linux go vet ./...`; `go test ./...`.
3. Test lines added are within the card's budget (`git diff --stat -- '*_test.go' testdata`).
4. No new dependency in `go.mod` (D8) and no `os.Exit` outside `cmd/`.
5. `git merge --no-ff v2-<ID>` into `v2`. Update `STATUS.md` (state, commit, notes).

After a wave: run `go test ./...` once on `v2`. After a phase: the phase's exit criteria, ideally by a verifier subagent that did not write the code, given only the exit criteria and the diff.

## 6. Commits and branch hygiene

- `<type>(<scope>): <description>`, imperative, at most 72 characters (`core/rules/git.md`). Types used here: `feat`, `refactor`, `test`, `build`, `ci`, `docs`, `chore`.
- Commit messages end with the attribution lines the session's system reminder gives, if any.
- No force pushes, no history rewrites on `v2`.
- Do not push, tag, publish or bump a version unless the user asks (P5 lists these as user-confirmed steps).

## 7. Parallel-safety checklist for a new task card

- `writes:` lists directories or files, never `**` over a shared root.
- It does not edit `cmd/sdkit/main.go`, `go.mod`, `kit.go` (owned by P0-01) or another task's package.
- A new command registers itself from `cmd/sdkit/<command>.go`.
- If it needs a function from a package still being written in the same wave, the card is wrong: move it to the next wave.
