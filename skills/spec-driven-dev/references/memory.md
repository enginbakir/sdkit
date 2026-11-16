# Project memory

Read this only when `MEMORY=1` in `<project-dir>/sdkit.env`. Otherwise skip it; nothing here applies.

Durable knowledge lives in `<project-dir>/<MEMORY_DIR>` (default `memory/`), managed by `sdkit memory`. Short-term task state stays in `work/<slug>/` (`state.json`, `plan.md`, `notes.md`) and is deleted at closeout.

| Layer | Where | Lifetime |
|---|---|---|
| Working | the conversation | one session |
| Task | `work/<slug>/` | until closeout; survives `/clear` and compaction |
| Project | `memory/` (entries `fact`, `howto`, `decision`); `INDEX.md` links `known-pitfalls.md` and `adr/` | until removed; `sdkit memory stale` flags old ones |
| Personal | the host's own memory | the host decides; the kit does not touch it |

## Reading
The session hook injects `INDEX.md` only (one line per entry). Open an entry when its line matches the task; `sdkit memory search <term>` if unsure. Do not read all entries. A host without the hook (Codex, OpenCode, Cursor, Gemini): read `INDEX.md` yourself at intake.
An entry is a claim made on its `updated` date. If it contradicts the code you are looking at, trust the code and tell the user the entry needs fixing.

## Writing: during the task
Append candidates to `work/<slug>/notes.md` as you learn them, one line each, so nothing depends on your context surviving:
```
MEMORY: fact|howto|decision | <slug> | <title> | <one or two sentences>
```
Worth keeping: a fact that is not visible in the code and cost effort to find, a command sequence you had to discover, a small decision with its reason. Not worth keeping: anything the code or git history already says, task progress, opinions, anything secret (`sdkit memory` refuses secret-looking text).
Bigger decisions go to `adr/`, bug classes to `known-pitfalls.md` (`incident-to-rule`), as before.

## Writing: closeout (§8)
`MEMORY_PROMOTE` decides: `ask` (default): `sdkit memory candidates <slug>`, show the list, let the user choose with the host's question tool, then `sdkit memory promote <slug> <n...|all>`. `auto`: promote all. `off`: skip. Never promote without the user under `ask`. Run `sdkit memory stale` and mention any stale entries the task touched.

## Task-scoped discovery notes (`sdkit knowledge`)
`sdkit knowledge put <slug> <key> --files "a b"` stores a "how does X work" summary; `get` returns it only while each covered file has the same content hash (finer than `sdkit lookup`, which drops on any diff change). A note is the writer's claim, marked UNTRUSTED, and is refused to `verifier`, `reviewer` and `security-auditor` (`--role`): they read the code and diff themselves.
