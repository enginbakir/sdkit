# Risks

| Risk | Why it matters | Mitigation | Owner |
|---|---|---|---|
| **No oracle once bash is deleted** | A rewrite can drift silently, and the tests are new | Goldens captured from bash **before** deletion (`05-workflow.md` §4); contracts inventory (P0-07); scenarios built from real refusals; the generated adapter files are a byte-exact oracle for the emitters (P2-02) | every worker |
| **Guard regexes and globs differ** | A guard that stops matching is a hole, not a bug | `internal/glob` with shell semantics; RE2 check of every regex; the bypass cases of the current guard tests become table rows (`gate-security`, `hardening`, `security` suites) | P0-04, P1-11 |
| **Moving target** | Roadmap work (F07 to F20) still changes `route`, `usage`, `telemetry` in bash | Freeze new bash features on `master`; port P4 last; D7 decided before P4 | orchestrator |
| **Hook latency** | Hooks run on every tool call; Windows process start is slow | One process per event (D4); no network or git call in a hook unless the bash had one; measure in the Windows smoke | P1-06 |
| **Host behaviour on Windows is unverified** | Hook shell, `PATH` resolution of `sdkit.exe`, command quoting differ per agent | `P0-09` spike first; results written to `spike-windows-hosts.md`; cards marked `(assumption)` revisited | P0-09 |
| **Plugin and binary version skew** | Plugin skills call a command the installed binary lacks | `doctor` and `check-tools` compare versions; skills never call a command newer than their own version | P1-13, P2-04 |
| **Test budget creep** | The point of the rewrite is a small suite | Per-card line budgets; the gate in `05-workflow.md` §5 counts added test lines | orchestrator |
| **Maintainer on macOS only** | Windows bugs found late | Same Go tests on a Windows runner; Windows code only in `*_windows.go` and `proc`; `GOOS=windows go vet` on every merge | all |
| **Attestation or state format drift** | Repos in the field hold committed attestations and `state.json` | Field names frozen (`02-contracts.md`); fingerprint change only with an upgrade note (D11) | P1-07 |
| **CI gate weakened** | The check must not be editable by the PR it judges | Pinned, checksummed release read from the base branch; scenario B | P5, P1-07 |
| **jq key order and number formatting** | Committed JSON and hashed chains depend on it | Ordered structs; golden for `audit` chain and `attestations/`; compare number formatting (integers, floats) with the bash output | P3-07, P1-07 |
| **Embedded data vs files** | Skills must be both embedded and present for the plugin | One source (the repo dirs); `go:embed` reads them; a test that the embedded list equals the on-disk tree is not needed, `go build` fails on a missing path | P0-01 |
| **Large single task (sdd core)** | `sdd.sh` is 912 lines and the heart of the kit | One worker, split into reviewable commits in the card order; a second subagent verifies against the gate matrix | P1-07 |
