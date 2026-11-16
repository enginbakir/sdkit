---
name: generate-tasks
description: Analyst skill. Turns an approved, merged spec (given as a Jira parent key and an exact commit SHA) into atomic Jira subtasks with risk, acceptance criteria, dependencies and scope, checks full AC coverage and dependency cycles, asks the analyst to confirm, then upserts the subtasks under the existing parent and writes a traceability report. Also consumes developer spec-change requests.
license: MIT
metadata:
  author: spec-driven-kit
  version: "1.0"
---

Analyst environment (`SDKIT_ENV=analyst`). You derive the work; `sdkit tasks` does every deterministic check and `sdkit jira` is the task contract. Contract: `docs/design-role-aware.md` ("Task contract"). Read a reference only at the step that names it.

Input: Jira **parent key** (must exist), **spec id**, exact **SHA**. Never create a parent, never assign anyone, never read specs from the working tree.

## Steps

1. **Approval.** Write `tasks.json` skeleton (specId, specVersion, specSha, parent) and run `sdkit tasks acs <tasks.json>`. Every sdkit tasks verb except `cycles` first calls `sdkit spec approved-sha <id> --sha <SHA>` and refuses (exit 3) unless the SHA is on `origin/main` and the spec is approved. Stop on refusal; do not work around it.
2. **Read the spec at the SHA** only through `sdkit tasks acs` / `git show <SHA>:specs/<id>/...`. Not the working tree, not a branch. `acs` and `trace` skip ACs the spec marks `(withdrawn)` (they are not tested, so they need no task); `--include-withdrawn` lists them again.
3. **Revision with changes.** If `sdkit tasks changes <tasks.json>` lists slugs, this SHA is a merged revision: create tasks ONLY for the ACs `sdkit tasks acs <tasks.json> --revision` prints (what those changes added or modified), taken from each change's section of `tasks.md` (`## Change: <slug>`) and re-derived atomically; set `change` to the slug on every such task. Tasks of earlier changes already exist in Jira (done or not): never touch them; a modified AC always gets a NEW task that names the earlier task(s) of the same ACs in its `summary`. Ordinals like `3.12` are never identities. Use that AC list for step 4's `trace`. With no changes (first base), all ACs.
3b. **Derive atomic tasks**: one reviewable change each, with `ref`, `title`, `risk` (from `<project-dir>/project.md` rubric; never invent), `acceptanceCriteria[]`, `dependsOn[]` (refs), `scope[]` (paths/globs). Format and key rules: `references/task-format.md`.
4. **Check**: `sdkit tasks plan-check <tasks.json>` first (size 12-20 tasks, 4-12 ACs per task, single-AC and chain limits, gated ACs apart, risk rubric filled; it prints `pack:` suggestions; apply them with `sdkit tasks regroup <tasks.json> --merge A,B,C --out <file>`), then `sdkit tasks trace <tasks.json> <ac-list>` (the `--revision` list for a revision) (every AC mapped, no orphan) and `sdkit tasks cycles <tasks.json>`. Fix and repeat; never confirm a plan that fails either.
5. **Plan gate**: show the analyst the `plan-check` summary and the AC-to-task table, dependencies, risk and scope; wait for explicit confirmation. No Jira write before it.
6. **Validate and upsert** in `sdkit tasks order` sequence: `sdkit tasks render <tasks.json> <ref> --keys map.json --json` (it runs `sdkit jira validate-task`; any failure stops everything before the first write), then `sdkit jira upsert <externalId> <json>` (or the Jira MCP tools, `references/jira-write.md`). Append each result line to `results.jsonl` and add `externalId -> key` to `map.json`.
7. **Report**: `sdkit tasks report <tasks.json> results.jsonl --out <report.md>`. It exits 1 and says NOT a success if any task has no result. A failed external call is never reported as done; say which tasks exist and which do not, and rerun (idempotent by `externalId`, no duplicates).

## Spec change requests (developer to analyst)

When a developer finds a defect in the spec, a change request arrives (`templates/change-request.md`). How to consume it: `references/change-request.md`.
