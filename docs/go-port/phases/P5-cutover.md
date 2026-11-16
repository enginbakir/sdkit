# P5 Cutover

Goal: the repository contains Go, data and documentation only. Every reference to a script path is rewritten, the bash is gone, the packaging and the versions are ready, and the user has a checklist to approve the irreversible steps. **The orchestrator prepares, verifies and stops; it does not tag, push, publish or bump the version on its own** (`06-release.md`).

Depends on: P4 merged, or P3 merged with the "not in 2.0" list agreed (D6).

## Waves

| Wave | Tasks (parallel) | Starts when |
|---|---|---|
| 1 | P5-01, P5-03 | P4 (or P3 per D6) merged |
| 2 | P5-02 | P5-01 merged |
| 3 | P5-04 | P5-02 merged (counts need the final tree) |
| 4 | P5-05 | P5-04 merged |
| 5 | P5-06 | P5-05 merged |

## Exit criteria

- The cutover checklist in `06-release.md` is all ticked except the user-confirmed steps.
- `go build ./... && go vet ./... && go test -race ./...` green; `GOOS=windows go vet ./...` clean; `sdkit dev adapters --check` and `sdkit dev scan` clean.
- `grep -rE 'scripts/[a-z-]+\.sh|hooks/[a-z-]+\.sh|bin/[a-z-]+\.sh|\$KIT/scripts|KIT="\$\(sdkit where\)"'` over `skills core agents adapters templates docs README.md CONTRIBUTING.md CLAUDE.md` finds nothing outside `docs/go-port/` and `docs/upgrading.md` (which names the old paths on purpose).
- Total test code ≈ 1,200 lines plus `internal/testutil` ≤ 150 (`04-testing.md`).

---

## P5-01 Rewrite references in skills, roles and wiring {#p5-01}

- **Wave:** 1 · **Depends:** P4/P3 · **Size:** M
- **writes:** `skills/**`, `core/roles/*.md` (not `core/roles/README.md`), `core/rules/**`, `agents/**` and `adapters/**` (regenerated, never by hand), `hooks/hooks.json`, `templates/**` (except `templates/ci/**`), `tools/rewrite-refs/` (temporary).
- **Do:**
  - Write a one-off tool `tools/rewrite-refs/` that applies the command map of `01-architecture.md` (`$KIT/scripts/sdd.sh gate edit X` → `sdkit sdd gate edit X`, `scripts/run-role.sh` → `sdkit run-role`, and so on for every script) and run it over the write set. Delete the tool when done.
  - `hooks/hooks.json`: one `sdkit hook <event>` entry per event (D4), no `${CLAUDE_PLUGIN_ROOT}`.
  - `templates/settings.plugin.json` and `settings.copy.json` allow-lists: `Bash(*/sdd.sh *)`, `Bash(*/spec.sh *)`, `Bash(*/review.sh *)` and the other script patterns become `Bash(sdkit *)` (keep the deny list; keep `Bash(sdkit prespec *)` semantics).
  - Adapter wiring files (`adapters/cursor/hooks.json`, `adapters/gemini/settings.json`, the OpenCode plugin) call `sdkit hook`; the OpenCode plugin uses `spawnSync("sdkit", ["hook", ...])`.
  - Regenerate `agents/` and `adapters/*/agents` with `sdkit dev adapters`; the diff must be explained by the rewritten role text only.
  - Work through the bash-only snippet list of `contracts-inventory.md` (P0-07): `mkdir -p`, `mv`, `cp`, `cat`, `jq`, `$(...)`. Replace each by an `sdkit` command or by an instruction the agent follows with its own file tools; the text must work in PowerShell, cmd and bash. `SKILL.md` stays short; detail stays in `references/`.
  - Skills stay host-neutral: host-specific tool names go through `references/hosts.md`.
- **Delete:** the temporary tool.
- **Tests:** none; the grep in the exit criteria is the gate.
- **Done when:** grep clean for the write set; `sdkit dev adapters --check` clean; `jq .` (or `go run` JSON check) passes on every touched JSON file.

## P5-02 Delete the bash {#p5-02}

- **Wave:** 2 · **Depends:** P5-01 · **Size:** S
- **writes (deletions only):** `bin/*.sh`, `bin/sdkit`, `bin/emit/` (if any is left), `hooks/*.sh`, `scripts/*.sh`, `stacks/*/scripts/`, `stacks/*/detect.sh`, `adapters.d/`, `tests/*.sh`, `tests/old/`, `tests/docker/`, `tests/lib/`, `tests/stubs/`, `tests/fast.sh`, `tests/test-map.conf`, `tools/golden/`, `bench/` (moved to `attic/bench/` per D5, excluded from the Go build), `packaging/scan-publishable.sh`, `packaging/homebrew/gen-formula.sh`, `Formula/` content (regenerated in P5-03).
- **Keep:** `adapters/git/*` shims, `stacks/*/verify.d.example/*.example`, `testdata/golden/`, `tests/windows-smoke.ps1`, every data file listed in `01-architecture.md`.
- **Do:** list the files, delete them in one commit `chore(cutover): remove the bash runtime`, confirm `git ls-files '*.sh'` shows only the allowed ones.
- **Done when:** `git ls-files | grep -E '\.sh$'` returns only the shims and `*.sh.example`; build and tests still green.

## P5-03 Packaging and CI templates {#p5-03}

- **Wave:** 1 · **Depends:** P4/P3 · **Size:** M
- **writes:** `.goreleaser.yaml`, `.github/workflows/release.yml`, `Formula/sdkit.rb` (template for the generated formula), `packaging/homebrew/README.md`, `packaging/scoop/sdkit.json`, `packaging/scan-allow.txt` if needed, `templates/ci/**`.
- **Do:**
  - GoReleaser: six targets, archives (zip for Windows), checksums, Homebrew formula and Scoop manifest generation to a local `dist/` only. **No publish block.**
  - `Formula/sdkit.rb`: downloads the release binary, `depends_on "node"`, no `jq`; keep the `test do` block (`sdkit version`, `sdkit where`).
  - `templates/ci/{github-actions,gitlab-ci,bitbucket-pipelines,pr-check}.yml`: install a **pinned `sdkit` release with checksum verification** read from the base branch, then `sdkit sdd ci-check` (`06-release.md`, "CI gate"). Windows CI examples included for GitHub Actions.
  - `packaging/homebrew/README.md` and the maintainer notes updated for the new release flow.
- **Tests:** none; `goreleaser release --snapshot --clean` (if installed) and YAML parse.
- **Done when:** a local snapshot build produces the six archives and the checksum file.

## P5-04 Docs, rules and counts {#p5-04}

- **Wave:** 3 · **Depends:** P5-02 · **Size:** M
- **writes:** `README.md`, `CONTRIBUTING.md`, `CLAUDE.md`, `docs/*.md` except `docs/go-port/` and `docs/upgrading.md`, `core/roles/README.md`, `stacks/README.md`.
- **Do:**
  - `CLAUDE.md` and `CONTRIBUTING.md` describe Go: layout, build, `go vet`, `go test ./...`, `sdkit dev adapters`, the checking commands. Keep the rules that stay (generic kit, resolver, never hardcode `.claude/`, never overwrite project files, hooks off by default, roles narrow, short `SKILL.md`, `spec-driven-dev` is a driver, sync of counts and tables, commits per `core/rules/git.md`). Replace the bash and shell-test rules.
  - `README.md`: platform badge and Requirements (macOS, Linux, Windows; `git` and `node`; no `jq`), the Windows install in Quick Start, the "Windows is not supported" line removed, command tables updated. Keep the README short; details in `docs/`.
  - Counts and tables in sync (CLAUDE.md "When you change something"): `README.md`, `core/roles/README.md`, `docs/guide.md` §2, §6, §7, §10, §15, `docs/cheatsheet.md`. Counts of "scripts" become the list of `sdkit` subcommands.
  - Docs stay English; details live in `docs/guide.md` and other files link to it.
  - `docs/README.md`: add the Go port directory to the reading map until the port is done, then remove it.
- **Done when:** the grep of the exit criteria is clean for these files and every count matches `ls`/`sdkit help`.

## P5-05 Versions and upgrade guide {#p5-05}

- **Wave:** 4 · **Depends:** P5-04 · **Size:** S
- **writes:** `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`, the badge lines in `README.md`, `docs/upgrading.md`, `Formula/sdkit.rb` version field, `packaging/scoop/sdkit.json` version field.
- **Do:** set the version chosen in D1 in **both** plugin JSON files and the README badge (fix the existing mismatch: label 1.9.0, URL 1.8.0), the formula and the Scoop manifest, all in one commit. Write the 1.x to 2.0 section of `docs/upgrading.md`: `brew upgrade sdkit` (or the Windows install), `sdkit upgrade` in each repo (what it removes and rewrites), git hooks path, the CI template change and the pinned version, `jq` no longer needed, pre-1.0 repos go through the last 1.x first (D2), refresh the Claude Code plugin with `/plugin`.
- **Done when:** the version string is identical in all five places (`grep`).

## P5-06 Final verification and handoff {#p5-06}

- **Wave:** 5 · **Depends:** P5-05 · **Size:** S
- **Do:** run every gate of `05-workflow.md` §5 on `v2`; run the cutover checklist of `06-release.md` and tick it in `STATUS.md`; run `sdkit dev scan --history` and report findings (do not fix secrets silently); have **one verifier subagent that did not write the code** read the checklist and the diff of the cutover commits and answer PASS or BLOCK with reasons; run the five scenarios and the smoke script locally where possible.
- **Then stop** and give the user the short list of irreversible steps that need an explicit yes: tag `bash-final`, create branch `v1`, merge `v2` into `master`, push, tag the release, run the first Windows CI job, publish artifacts, formula and Scoop manifest.
- **Done when:** the report is written to `docs/go-port/out/final.md` and the user has answered.
