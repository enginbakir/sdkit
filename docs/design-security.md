# Security hardening (F19)

> **History.** This record was written for the 1.x (bash) kit. Commands are shown by their 2.0 names (`sdkit <command>`); the `tests/*.test.sh` suites, `bench/` and `jq` it names belong to 1.x (2.0 replaced the suites with a small Go test set, and the harness lives in `attic/bench/`, unmaintained). The design and the decisions still stand; see [guide.md](guide.md) for how 2.0 is built.

The audit separates what the kit **actually enforces** (hook/script code) and counts only what is written in a prompt as "not enforced".

| Area | Enforcement | Where |
|---|---|---|
| Read-only roles | no Edit/Write in the `agents/*.md` tool list + the `guard-readonly` hook | `sdkit audit policy` checks both |
| Secrets | `guard-secrets`; `sdkit redact` in views (summary/envelope output, error messages, audit values) | full text on disk is **not modified** (evidence integrity) |
| Data egress to a provider | the built-in LiteLLM adapter (`internal/adapter`): `LITELLM_EGRESS=allow`, host allow-list, text-only (independent and writing roles are refused), key via stdin | the audit record is written **first**: no record, no request |
| Audit trail | `<project-dir>/audit.jsonl`, hash-chained, metadata only (kind, role, host, bytes); no prompt/answer/content, values over 120 characters are rejected | `sdkit audit verify` finds edits/deletions in the middle; it **cannot prevent truncating the tail** |
| External adapter | `adapters.d/*.sh` writable permissions and `capabilities` validity | `sdkit audit policy` |

`sdkit redact` works with known formats (AWS key id, GitHub/Slack tokens, `sk-…`, JWT, Bearer, `password=`, private key blocks). An unknown format passes through: **a filter, not a guarantee**. `sdkit plan writes` and `sdkit worktree merge` are unchanged for the sandbox/worktree boundary. No critical issue was found in the audit this round; for the path I added myself (the litellm adapter), the key not appearing in `ps` and being masked in error messages is pinned by a test. A live provider and live Claude hook payloads were not tried. `tests/security.test.sh`.
