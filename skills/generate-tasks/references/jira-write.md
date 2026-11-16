# Writing to Jira

Two transports, one contract. `sdkit jira validate-task` is the validator for both; `sdkit tasks render` calls it before any write.

- **sdkit jira (deterministic)**: `sdkit jira upsert <externalId> <json>` needs `JIRA_EGRESS=allow` and credentials by env var name (see `adapters.d/sdkit jira`). Idempotent by label `sdkit-ext-<externalId>`: finds the existing subtask and updates it, else creates it under the parent. Prints `{key, externalId, action}`; exit 3 egress/auth, 4 contract, 5 external failure.
- **Jira MCP tools (used by the agent)**: search for the label `sdkit-ext-<externalId>` first; update if found, create a subtask under the parent if not. Use the description from `sdkit tasks render` verbatim (the fenced `sdkit-task` block), add labels `sdkit` and `sdkit-ext-<externalId>`. After the call, read the issue back and run `sdkit jira validate-task` on its description; only then write the result line to `results.jsonl`.

Rules for both: never create a parent; never set an assignee; a non-zero exit, an error response or a missing key means no result line (so `report` fails). Do not retry blindly: search by label first, so a half-finished run cannot duplicate.

## Before the first write, and when the plan is regrouped
- `sdkit preflight tasks <tasks.json>` first: SHA shape and approval, parent key and its existence, Jira token and egress, risk rubric. It names every cause at once.
- `sdkit jira upsert <externalId> <file|->` takes the `render --json` output as a file or on stdin.
- Regrouping after tasks already exist in Jira: build the new plan (`sdkit tasks regroup`), create the new tasks, then `sdkit jira supersede <old keys> --by <new key>` (it only prints the plan until the person confirms and you add `--yes`; it comments, moves to `JIRA_SUPERSEDED_STATUS` when set, and deletes nothing).
