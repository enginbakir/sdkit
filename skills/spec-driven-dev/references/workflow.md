# Workflow profiles, transitions and recovery

Read at step 0 (choosing a profile) or when a move is refused. The code decides; this says what it decides. `<sdd> transitions` lists the moves and their entry rules, `<sdd> policy` what the open task requires.

## Profile (process weight) is not risk (the gates)
The user confirms the risk label; you propose the profile with it and the user may override. Both are fixed once the task starts.
- `small`: `low` risk only (refused otherwise), no contract overhead, nothing mandatory skipped. For a one-file change you do yourself in one session.
- `standard` (default, omit `--profile`): the workflow in `SKILL.md`, unchanged.
- `high-assurance`: any risk. `<sdd> advance verified` needs an independent verifier PASS for this diff even at `low`; handoffs to writing and deciding roles (`developer test-writer devsecops engineer verifier reviewer security-auditor`) must carry a contract (`references/contract.md`, hook asks / `sdkit run-role` exits 2 without one); an agent that never answered blocks `green` and `verified`. Use it when the user asks for strict evidence, or the change is irreversible.
A valid contract never stands in for the verifier.

## Refused moves (state is left untouched; `phase.reject` carries the reason)
`not-next`, `unknown-phase`: phases are not skipped; the only way back is `abort`. `no-artifact`, `no-dod`, `no-design`: write the missing artifact. `tests-pass` (not RED) / `tests-fail` (not GREEN): fix the tests. `no-verdict`, `stale-verdict`, `verdict-block`: run the verifier on the current diff (§5, §6). `agents-pending`: wait, or `sdkit contract close <id> --reason interrupted`, then `sdkit state <slug> retry <id>` once. `risk-floor`: reopen with the higher label. `concurrent`: another advance won; re-read `<sdd> status`.

## Recovery rules
- One automatic retry per task id (`sdkit state retry`); a second failure of any kind goes to the user. A refused move is not a retry.
- A diff edited after `verified` is stale (`status` shows `evidence=stale`): verify again, `<sdd> advance verified` is allowed while stale. Push stays denied until then.
- Interrupted agent (host error, killed): its handoff stays pending; close it, retry once with a new `ID:` and `RETRY-OF:`.
- Resume after /clear: `sdkit state <slug> show` and `<sdd> status`. Full checkpoint/resume is not part of this.
