# tasks.json and task keys

```json
{"specId":"checkout","specVersion":"1.0","specSha":"<40 hex>","parent":"PROJ-12",
 "tasks":[{"ref":"A","title":"...","summary":"why and what, one paragraph","slug":"optional","risk":"low|medium|high",
           "acceptanceCriteria":["AC-1"],"dependsOn":["B"]}]}
```

- `ref` is a local label for this draft; `dependsOn` uses refs. `render` maps them to `externalId`, then to Jira keys (`--keys map.json`), because the contract's `dependencies[]` holds Jira keys. Create in `sdkit tasks order` so a dependency's key exists first.
- `externalId = <specId>:t<hash>`; the hash covers the sorted ACs and optional `slug`, never the title or the position. A rerun with the same ACs keeps the key; changing a task's ACs is a new task. Two tasks with identical ACs need distinct `slug`s (sdkit tasks says so).
- Size: aim for `TASKS_TARGET_MIN`-`TASKS_TARGET_MAX` tasks (default 12-20), typically 4-12 ACs each; one rule or behaviour family is one task (e-mail rules V-4..V-7 are one task, not four). Split only for (i) a different gate (an AC marked pending/conditional on an open question is its own task, `blockedBy: ["Q1"]` puts "Blocked: Q1" in its summary), (ii) a different risk, (iii) independent delivery. `sdkit tasks plan-check` measures this.
- Atomic: one change a reviewer can hold in their head, one risk.
- AC ids are the spec's own (`AC-<n>`); never rename them. Every AC maps to at least one task; a task without an AC is an orphan and is rejected.
- Required by the contract: externalId, specId, specVersion, specSha, acceptanceCriteria, risk, dependencies, parent. Missing or empty means intake rejects it.

## `change` (tasks of a merged revision)

Optional string, the slug of the spec change the task belongs to (`specs/<id>/changes/<slug>/`). It is part of the task key (`sdkit tasks ids`), so the same ACs in a later change get a new `externalId` instead of updating a possibly done subtask. Leave it out for the first base. Completion is never recorded in the spec; Jira is the only truth.
