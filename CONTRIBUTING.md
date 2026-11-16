# Contributing to the kit

This file is for people who change **the kit itself**. To use the kit in a project see [README.md](README.md) and
[docs/README.md](docs/README.md).

## Core rule: the kit stays generic

**Nothing project-specific is baked into** skills, agents, hooks or code: no branch names, cluster names, service names, team jargon or risk criteria.
These come from the project's `<project-dir>/sdkit.env` and `project.md`. If you find yourself saying "this is only true in our project" about a change, it belongs
not in the kit but in that project's project dir (`<project-dir>` = `.sdkit/`). (A fix was made for exactly this reason in the past: `fix: keep project-specific wording out of the generic hooks and agents`.)

Second rule: **do not overwrite project-side files.** `sdkit init` never overwrites existing `project.*`, `rules/`, `settings.json` files, not even with `--force`;
`--force` is only for the managed files of copy mode.

## Build and layout

The kit is one Go binary (`go.mod`: module `github.com/wecii/sdkit`). `go build ./cmd/sdkit` builds it; `go run ./cmd/sdkit <command>` runs it from the repo. `cmd/sdkit/` only dispatches (one file per command); the logic is in `internal/<package>`; `kit.go` embeds `core skills agents adapters stacks templates`, so the binary carries the kit. The package map and the code rules are in [docs/go-port/01-architecture.md](docs/go-port/01-architecture.md); the rules in short:

- A package takes its inputs as parameters. Only `cmd/` and `internal/hook` read stdin, `os.Args` and the environment; settings arrive as a struct built by `internal/envfile`.
- No `os.Exit` outside `cmd/`. JSON that is hashed or committed uses structs, not `map` iteration.
- Code must run on Windows: process code in `proc_unix.go` / `proc_windows.go`, paths through `filepath`, no `sh` for kit logic.
- Comments say why, not what; a command's header comment is its usage.

## What goes where

| I want to add | Where | Also |
|---|---|---|
| Skill | `skills/<name>/SKILL.md` (frontmatter: `name`, `description`, `license`, `metadata`) | `description` must say "when to use it"; `docs/guide.md` §5 and the README tables |
| Role (agent) | `core/roles/<name>.md` (frontmatter: `name`, `description`, `kind`, `access`, `tier`, `independent`, optional `deep`), then `sdkit dev adapters` | **`agents/` and `adapters/<host>/` are generated, do not edit them by hand.** Keep access as narrow as possible (independent roles `read-only`); fix the output format; `core/roles/README.md` and `guide.md` §6 |
| Hook (guard) | `internal/hook/<name>.go` registering itself with `Register(event, tools, name, fn)`; the event wiring in `hooks/hooks.json` only if it is a new event | Read the setting through the hook's settings helper; document the setting that turns it off (`sdkit.env`); the table in `guide.md` §7; the copy-mode wiring in `internal/setup` (`init`/`doctor`); a row in the guard table test |
| CLI command | `cmd/sdkit/<command>.go` (registers itself in `init()`) and `internal/<package>` | The command list in `README.md`, `docs/guide.md` §2, §9, §15 and `docs/cheatsheet.md` |
| `sdkit.env` setting | `templates/sdkit.env` if it is team policy, `templates/sdkit.local.env` if it is personal (limit, budget, host/model, memory behaviour); default lines are appended to existing files automatically by `init`/`upgrade`. If you make a key personal, the personal-key split moves it too | Pick a safe default (empty = feature off); `guide.md` §10 |
| Generic rule | `core/rules/` | Must be language-independent; `sdkit init` copies it into the project |
| Flow step / detail | `skills/spec-driven-dev/SKILL.md` stays short; details go under `references/` | Token economy: a reference is read only at the relevant step; do not bloat `SKILL.md` |
| New host | The "Adding a new host" list below | `guide.md` §14, §16 |
| Tool-independent guard | `internal/guard` + the two-line shims in `adapters/git/` | The same rule as the Claude hooks, at commit/push time |
| Language/stack | `internal/stack/<name>.go` + `stacks/<name>/` | The "Adding a new stack" list below, `guide.md` §14 |
| Project template | `templates/` | `sdkit init` writes it, `sdkit doctor` checks it (`internal/setup`); update both |
| Example `verify.d` policy | `stacks/<stack>/verify.d.example/*.sh.example` | The `FAIL:`/`WARN:` protocol |

## Rules for writing hooks

- Input is JSON on stdin, handled by the framework in `internal/hook` (host adapters for claude, cursor, gemini and opencode translate it). The result is a `permissionDecision` (`deny` / `ask`) or, for Stop, a block.
- **Make off the default**: if the setting is empty, return no decision (exit 0, no output). A hook without a setting makes noise in every project.
- Block only on purpose: outside the case you need to decide on, the exit code is 0; the hook's own error or panic must not lock up the session (the framework recovers and exits 0).
- The deny/ask message must state the reason and the way out ("see `project.md`", "the active release branch is X").
- Hooks are safety nets, not a security boundary; write the escape routes into `guide.md` §13.
- Hooks are wired for Claude Code in `hooks/hooks.json` (`sdkit hook <Event>`, so `sdkit` must be on `PATH`); Gemini and Cursor call the same entry with `sdkit hook --host cursor|gemini`, OpenCode through a plugin that spawns `sdkit hook`. Their counterpart everywhere is `sdkit guard` (git hook): if you add a new guard, think about all of them together.
- Stop hooks must check `stop_hook_active` (so there is no infinite loop) and set a timeout in `hooks.json`.

## Verification (before submitting)

Tests are Go and deliberately few ([docs/go-port/04-testing.md](docs/go-port/04-testing.md)): test decisions, not plumbing. Allowed shapes: a **table test** per package for pure decision logic (a new bug is a new row, not a new file), a **golden** file (`testdata/golden/<pkg>/`) only for output that skills or docs quote, and the five **scenarios** (a real flow in a temp git repo with the real binary). Helpers are in `internal/testutil`. If you changed a guard, a gate or a parser, add a row; if you changed a flow, check the scenario that covers it. The whole suite stays under 20 s and runs on macOS, Linux and Windows without Docker or `jq`.

```bash
gofmt -l .                          # prints nothing
go build ./...
go vet ./...
GOOS=windows go vet ./...           # the Windows build must stay clean
go test ./...                       # the whole suite, about 20 s; no test selection
go run ./cmd/sdkit dev adapters --check   # are the roles and generated files in sync
go run ./cmd/sdkit dev scan               # nothing project-specific or secret in the tree
```

CI runs the same on ubuntu, macos and windows (`.github/workflows/go.yml`, with `-race`), and the Windows job also runs `tests/windows-smoke.ps1`.

Then try it end to end in a throwaway repo:

```bash
go build -o /tmp/sdkit ./cmd/sdkit && export PATH="/tmp:$PATH"      # sdkit must be on PATH
tmp="$(mktemp -d)" && git -C "$tmp" init -q && cd "$tmp"
sdkit init --stack go "$tmp"                       # plugin mode (the stack cannot be detected in an empty repo, --stack is required)
sdkit doctor "$tmp"                                # expected FAILs: only project.md with TODOs and (if there is no commit) BASE_BRANCH
sdkit init --mode copy --stack go "$tmp2"          # a separate throwaway repo for copy mode
sdkit init --host claude,codex --stack go "$tmp3"  # Codex files: AGENTS.md, .agents/, .codex/, .githooks/
sdkit init --host opencode,cursor,gemini --stack go "$tmp4"   # .sdkit/, .opencode/, .cursor/, .gemini/
```

For the parallel tools, try `sdkit plan waves` with a sample `tasks.md` (try waves, an unknown id and a cycle) and `sdkit worktree add|merge|clean` in a temporary repo;
for `sdkit guard`, stage a `.env` / protected path / frozen test in a temporary repo and try `git commit` (it must refuse); a clean file must pass.

If you changed a hook, run it by hand with an input that triggers it and one that **does not**:

```bash
echo '{"tool_name":"Read","tool_input":{"file_path":".env"}}' | CLAUDE_PROJECT_DIR="$tmp" sdkit hook guard-secrets
```

A guard is not trusted until it has been seen to "fail and then pass": try both the case it must block and the case it must allow. On Windows run the same in PowerShell; `tests/windows-smoke.ps1` is the model.

## Adding a new host

1. `internal/emit`: the host's begin and role emitters, output to `adapters/<host>/`; wire it into the `Dirs` list, run `sdkit dev adapters`, commit the generated files.
2. `core/hosts.env` (tool and permission names, read-only shell allow-list) and `core/tiers.env` (`<HOST>_MODEL_LIGHT|STANDARD|STRONG`; empty = the session's model).
3. `internal/setup` (`init`: `--host` values, project dir name, host files; `doctor`: host checks, CLI note; `upgrade`: host detection).
4. Guard wiring: if the host does not resemble the Claude hook JSON, add a host adapter in `internal/hook/hosts.go` or write a plugin like OpenCode's; otherwise only the git hooks (`adapters/git/`) remain. Check how the host starts a hook command on Windows.
5. A case for the host in `internal/runrole` (headless run flags) and a check with `sdkit run-role ... --dry-run`.
6. The table in `skills/spec-driven-dev/references/hosts.md`, `README.md`, `docs/guide.md` §13, §14, §16 and the table in `core/roles/README.md`.
7. A row in the hook scenario (C) for the host's JSON, and in the `init` scenario (E) for its files. If you did not try it on a live install, mark it as not validated in the README "Limits" and in guide.md §13.

## Adding a new stack

1. `internal/stack/<name>.go`: implement the `Stack` interface (`Name`, `Detect`, `Tools`, `IsSource`, `Fmt`, `Steps`) and `Register` it from `init()`. The contract is in [stacks/README.md](stacks/README.md). Data files go in `stacks/<name>/`: `permissions.txt`, `rules/`, `verify.d.example/`, optional config files and `known-pitfalls.md` (seed for the project file).
2. The detection order (first match wins; `Detect` must not give false positives) is in `internal/stack`; `sdkit init --stack` overrides it.
3. `Tools()` feeds `sdkit doctor` and the `check-tools` hook; a missing tool is a `warn`, not a `fail`.
4. `permissions.txt`: patterns to add to the Claude `settings.json` "allow" list (their narrowest form).
5. Shared logic stays in `internal/verify`; a stack only writes `Steps`. The Node-based stacks share `internal/stack/node.go`.
6. `templates/sdkit.env` (if there is a new stack setting), `README.md`, `docs/guide.md` §14 and `docs/cheatsheet.md`; a table row in the stack tests.

## Versioning and release

1. Make the change. If you changed roles in `core/roles/`, run `sdkit dev adapters` and commit the generated files too. Update the related tables in `docs/guide.md` and the README (including part counts: number of skills/roles/hooks and the list of `sdkit` subcommands; currently 8 skills, 11 roles, 20 hooks, 41 subcommands; count with `ls skills`, `ls core/roles`, the `Register` calls in `internal/hook` and `sdkit help`).
2. Bump the version in `.claude-plugin/plugin.json` **and** `.claude-plugin/marketplace.json` together (both must match; the binary reads `plugin.json`), and in the version badge at the top of `README.md`.
   - patch: fix, docs
   - minor: new skill/agent/hook/setting (off by default)
   - major: a change that requires existing projects to update files in their project dir by hand
3. If the release needs anything done by hand, add an "X → Y" section to `docs/upgrading.md` (`sdkit upgrade` adds new files itself; everything else is written there).
4. Releases are built from a tag by the release workflow (GoReleaser: archives for macOS, Linux and Windows, the Homebrew formula and the Scoop manifest; see [docs/go-port/06-release.md](docs/go-port/06-release.md)). The tag must match `plugin.json`. Projects do not see a new plugin version until the version is bumped and it lands on the branch the marketplace watches (the default branch). Repos using the plugin update with `/plugin`; those in copy mode run `sdkit sync`, so changing the path of managed files affects projects in copy mode.

## Commits and branches

[core/rules/git.md](core/rules/git.md) applies: `<type>(<scope>): <description>`, imperative mood, subject ≤ 72 characters, branch `<type>/<short-name>`.
In this repo the scope is the kit part (`hook`, `skills`, `verify`, `init` ...); a doc change is `docs:`.

## Docs

- `README.md` is kept in English and short: what it is, install, links.
- `docs/` is in English. Details live in `guide.md`; `cheatsheet.md` and the README only summarize and link to the guide, do not duplicate information.
- If a number or table appears in several files (hook count, subcommand list, settings list), update all of them in the same change.
