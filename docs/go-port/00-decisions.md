# Decisions

## Settled (by the user)

| # | Decision |
|---|---|
| S1 | The runtime is rewritten in **Go**, one binary `sdkit`, for macOS, Linux and native Windows. |
| S2 | **One implementation.** macOS and Linux move to Go too; the bash runtime is removed. No bash is needed on any OS. |
| S3 | Windows is native (PowerShell or cmd). No WSL, no Docker, no Git Bash requirement. |
| S4 | **Tests are rewritten from scratch and kept very small.** The shell suites are not ported (`04-testing.md`). |
| S5 | The kit is maintained from macOS; Windows is covered by the same Go tests on a Windows CI runner plus one smoke job. |

## Open, with the default the orchestrator uses until the user says otherwise

| # | Question | Default | Affects |
|---|---|---|---|
| D1 | Version number of the Go release | **2.0.0** (command paths change) | P5 |
| D2 | Pre-1.0 layouts (`.agentkit`, `.claude`, `.codex`, `project.env`) and `bin/migrate.sh` | **Dropped.** A repo on a pre-1.0 layout first runs the last 1.x `sdkit upgrade --migrate` | P0-02, P2-05 |
| D3 | `sdkit install` (registers the kit home for git hooks) | **Dropped.** The binary is on `PATH`; `sdkit where` replaces the registration | P2 |
| D4 | Hook dispatch | **One process per event**, an internal list of guards, merged decision (deny > ask > allow). Named subcommands `sdkit hook <name>` stay for debugging | P1-06 |
| D5 | `tests/old/`, `bench/` | `tests/old/` deleted. `bench/` moved out of the build, kept as is, unmaintained | P5-02 |
| D6 | What gates `2.0.0` | `2.0.0-beta` after P3. Final `2.0.0` after P4, or after P3 with P4 features listed as "not in 2.0" if the roadmap is not settled | P4, P5 |
| D7 | Roadmap items F07 to F20 (untracked `Implementation Roadmap.md`): which are done in the repo was **not verified** | Before P4, check each item against the repo; port what exists, never implement a missing one in bash | P4 |
| D8 | Go dependencies | **Standard library only.** `golang.org/x/sys` is allowed if a Windows process call needs it. No CLI framework, no JSON library | all |
| D9 | Minimum Go version | The version in `go.mod` at P0-01 (installed here: 1.27.2). Do not use experimental packages | P0-01 |
| D10 | Windows package channel | Release zip + Scoop manifest first; `winget` later | P5-03 |
| D11 | Fingerprint algorithm of attestations | Keep the 1.x algorithm unless a worker finds it depends on a bash or `shasum` quirk. Any change is written in `docs/upgrading.md` | P1-07 |
| D12 | Bash helpers kept for maintainers | None. `scripts/test-select.sh`, `tests/fast.sh`, `tests/docker/`, `tests/test-map.conf` are deleted. `go test ./...` runs whole | P5-02 |

## Needs a real Windows host (cannot be settled by reading)

`P0-09` collects the facts: how Claude Code, Codex, Cursor, Gemini CLI and OpenCode run hook commands on native Windows, which shell they use, whether they resolve `sdkit` (`.exe`) from `PATH`, and how they quote commands. Until then every Windows-specific choice in the cards is a working assumption, marked `(assumption)`.
