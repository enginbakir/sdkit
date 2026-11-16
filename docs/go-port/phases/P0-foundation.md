# P0 Foundation

Goal: a Go module that builds on three OSes, the shared building blocks every later task uses, the CI and release skeleton, and the facts the later phases depend on. No behaviour of the kit is ported yet except settings parsing.

**Bash oracle.** Before wave 0, the orchestrator creates a read-only worktree of `master` at `../go-port-oracle` (`git worktree add ../go-port-oracle master`). Workers read bash from there and capture goldens against it (`05-workflow.md` §4). It is never edited.

## Waves

| Wave | Tasks (run in parallel) | Starts when |
|---|---|---|
| 0 | P0-01 | `v2` exists |
| 1 | P0-04, P0-05, P0-06, P0-07, P0-08 | P0-01 merged |
| 2 | P0-02, P0-03, P0-09 | P0-05 and P0-08 merged (P0-09 also needs P0-06) |

## Exit criteria

- `go build ./... && go vet ./... && GOOS=windows go vet ./... && go test ./...` green on `v2`.
- `sdkit version`, `sdkit where`, `sdkit help` work from `go run ./cmd/sdkit`.
- `internal/envfile` parses every line form of `scripts/env.sh` and its table test passes.
- `docs/go-port/contracts-inventory.md` and `docs/go-port/spike-windows-hosts.md` exist (the spike may state "needs a Windows run" for items that require the user to push; P1 may start, P2 may not until it is settled).
- Total test code of the phase is within the card budgets (~110 lines plus helpers).

---

## P0-01 Scaffold {#p0-01}

- **Wave:** 0 · **Depends:** none · **Size:** S
- **Go:** `go.mod` (`module github.com/wecii/sdkit`, Go version per D9), `kit.go`, `cmd/sdkit/{main,version,where,help}.go`, `internal/cli/`.
- **writes:** `go.mod`, `go.sum`, `kit.go`, `cmd/sdkit/`, `internal/cli/`, `.gitignore` (add `/dist/`, `/sdkit`, `/sdkit.exe`, `docs/go-port/out/`).
- **Do:**
  - `internal/cli` holds a registry: `Register(name string, run func(args []string) int)`; `cmd/sdkit/main.go` looks the first argument up and calls it, nothing else. Later tasks add `cmd/sdkit/<command>.go` files with `init(){ cli.Register(...) }`. Subcommand groups (`sdkit sdd gate ...`) are dispatched inside their own file.
  - `kit.go` (package `kit`, at the module root) embeds `core skills agents adapters stacks templates` and the single file `.claude-plugin/plugin.json` (dot directories need the explicit file path).
  - `sdkit version` prints the version read from the embedded `plugin.json` (the single source of truth, no `-ldflags`). `sdkit where` prints the directory of the running executable. `sdkit help` lists registered commands.
  - Exit-code constants in `internal/cli`: 0 ok, 1 failure/refusal, 2 usage; the others from `02-contracts.md`.
  - No dependency besides the standard library.
- **Delete:** nothing.
- **Tests:** none (the build is the test).
- **Done when:** the exit criteria above for these commands hold, and `go list -deps ./... | grep -v '^[a-z/]*$'` shows only standard library packages.

## P0-02 envfile + project {#p0-02}

- **Wave:** 2 · **Depends:** P0-05, P0-08 · **Size:** M
- **Source:** `scripts/env.sh` (`sdkit_env_load`, `sdkit_env_load_file`, `sdkit_resolve_dir`, the parser and the refused-keys list), `bin/lib.sh` and `bin/sdkit` (repo root and project-dir resolution, `resolve_kit_dir`, `resolve_kit_env`), `templates/sdkit.env` and `templates/sdkit.local.env` (all keys with defaults).
- **Go:** `internal/envfile/` (parser, `Settings`), `internal/project/` (repo root through `gitx`, `Resolve(root)` returning `KitDir` and `EnvFile`, kit data access).
- **writes:** `internal/envfile/`, `internal/project/`.
- **Do:**
  - Parse exactly the line forms in `02-contracts.md`. Unsupported lines are ignored and reported on stderr as `file:line: ignored (unsupported syntax)` without their content; count them; `SDKIT_ENV_QUIET=1` silences. Refuse the dangerous keys listed in `env.sh`.
  - `$NAME` and `${NAME}` expand only from settings read earlier. Nothing else is expanded; nothing is executed.
  - Order: `sdkit.env` then `sdkit.local.env`, the second wins. A process environment variable wins only where `env.sh` lets it (check each: CI base ref `VERIFY_BASE`, `KIT_DIR`).
  - The defaults come from the **embedded** `templates/sdkit.env` (parse it with the same parser), so a default exists in one place only.
  - Tolerate CRLF, a BOM and UTF-16 (`03-cross-platform.md` item 8).
  - D2: only `.sdkit` with `sdkit.env` (plus `$KIT_DIR` when set); no `.agentkit`/`.claude`/`.codex`.
  - `Settings` exposes typed getters (`Bool`, `Int`, `List` for comma lists, `String`) and keeps unknown keys in a map.
- **Delete:** nothing (`env.sh` is still sourced by every bash script until P5-02).
- **Goldens:** 2 cases: a settings file with every line form and each ignored form; the stderr report text.
- **Tests (≤ 60 lines):** one table in `envfile`: accepted forms, ignored forms (command substitution, `;`, `&&`, bare command, unquoted space), refused keys, `$NAME` expansion, CRLF, BOM. Plus a 10-line resolver test with `TempRepo`.
- **Done when:** the table passes and the goldens match.

## P0-03 store {#p0-03}

- **Wave:** 2 · **Depends:** P0-05 · **Size:** S
- **Source:** `scripts/env.sh` (`sdkit_lock`, `sdkit_unlock`: `mkdir` lock, owner pid, stale detection), the `put()` read-modify-write in `scripts/sdd.sh`, JSONL appends in `scripts/usage.sh`, `telemetry.sh`.
- **Go:** `internal/store/`: `Lock(path)`, `Update(path, func(old []byte) ([]byte, error))` (lock, read, write atomically, unlock), `WriteAtomic`, `AppendLine(path, line)`.
- **writes:** `internal/store/`.
- **Do:** `mkdir` lock with a pid file; a lock whose owner is dead (`proc.Alive`) or older than the bash threshold is removed; bounded wait. Atomic write = temp file in the same dir then rename (retry rename up to 1 s on Windows). `AppendLine` opens with `O_APPEND` and writes one line per call.
- **Delete:** nothing.
- **Tests (≤ 30 lines):** 20 goroutines increment a counter through `Update`; a stale lock is taken over.
- **Done when:** passes with `-race`.

## P0-04 glob {#p0-04}

- **Wave:** 1 · **Depends:** P0-01 · **Size:** S
- **Source:** the three copies of `matches_glob_list` (`hooks/lib.sh`, `scripts/guard.sh`, `scripts/sdd.sh`) and the protected-prefix loop in `guard.sh` and `hooks/guard-protected.sh`.
- **Go:** `internal/glob/`: `Match(pattern, s string) bool` with **shell `case` semantics** (`*` matches any characters including `/`, `?`, `[...]`, `[!...]`, backslash escape), `MatchList(path, commaGlobs string) bool` (basename or whole path; empty entries skipped; empty list is false), `HasPrefix(path, commaPrefixes)` for `PROTECTED_PATHS`.
- **writes:** `internal/glob/`.
- **Do:** implement the matcher by hand (not `filepath.Match`). On Windows, compare case-insensitively where `03-cross-platform.md` says so (a parameter, not a global).
- **Delete:** nothing.
- **Tests (≤ 30 lines):** one table, 20+ rows: `*.pem`, `.env*`, `**/secrets/*`, `secrets/*`, `a/b.go` vs `*.go`, a path with spaces, an empty entry, a pattern with `[abc]`, a pattern that `filepath.Match` would reject.
- **Done when:** the table passes.

## P0-05 proc + gitx {#p0-05}

- **Wave:** 1 · **Depends:** P0-01 · **Size:** S
- **Source:** how the scripts call `git`, `timeout`, `kill -0`, `pkill`, `mktemp`; `scripts/capped.sh` is **not** in scope here.
- **Go:** `internal/proc/` (`Run(ctx, dir, env, name, args...)` result with stdout, stderr, code; `Shell(cmdString)` returns the command to run a user string per `03-cross-platform.md` item 2; `LookPath`; `Alive(pid)`; `KillTree`; files `proc.go`, `proc_unix.go`, `proc_windows.go`), `internal/gitx/` (`Top`, `Run(args...)`, `Lines`, `IsIgnored`, `Tracked`, `MergeBase`, `RevParse`, `Config`, `ChangedFiles` helpers used later; always `-c core.quotePath=false` for name lists).
- **writes:** `internal/proc/`, `internal/gitx/`.
- **Do:** no function calls `os.Exit`. Context timeouts everywhere. `Shell` honours `SDKIT_SHELL`.
- **Delete:** nothing.
- **Tests (≤ 15 lines):** `Shell("exit 3")` returns 3 on the host OS; `gitx.Top` in a `TempRepo`.
- **Done when:** `GOOS=windows go vet ./internal/...` is clean.

## P0-06 CI and release skeleton {#p0-06}

- **Wave:** 1 · **Depends:** P0-01 · **Size:** S
- **writes:** `.github/workflows/go.yml`, `.github/workflows/release.yml`, `.goreleaser.yaml`, `.gitattributes`.
- **Do:**
  - `go.yml`: matrix `ubuntu-latest`, `macos-latest`, `windows-latest`; steps `go build ./...`, `go vet ./...`, `gofmt -l .` must be empty, `go test -race ./...`. Windows job also runs `tests/windows-smoke.ps1` when that file exists (added in P2-06).
  - `release.yml`: manual and tag triggered, runs GoReleaser in `--snapshot` mode only. It publishes nothing until the user changes it (`06-release.md`).
  - `.goreleaser.yaml`: six targets (`06-release.md`), `CGO_ENABLED=0`, archives, checksums, no publishing section yet.
  - `.gitattributes`: `* text=auto eol=lf`, plus `*.ps1 text eol=crlf` for PowerShell scripts and `*.cmd text eol=crlf`. Check that no existing file is affected in a way that changes committed bytes (`git add --renormalize .` should show nothing); report if it does.
- **Delete:** nothing.
- **Tests:** none.
- **Done when:** `actionlint` (if installed) is clean, the YAML parses, `goreleaser check` (if installed) passes. The workflow runs on GitHub only after the user pushes; the card cannot prove that.

## P0-07 Contracts inventory {#p0-07}

- **Wave:** 1 · **Depends:** P0-01 · **Size:** S
- **writes:** `docs/go-port/contracts-inventory.md` (read-only everywhere else).
- **Do:** list every string, JSON shape, file name and exit code that a skill, role, doc, template or hook **quotes or parses** and that the Go program must therefore reproduce: the `sdd status` line, step lines, evidence table, refusal messages quoted in `skills/*/SKILL.md` and `references/`, `docs/developer-workflow.md` outputs, hook JSON, `doctor` severities (`FAIL`, `warn`), `jira.sh` and `spec.sh` output that skills parse, `workflow.sh capabilities`. One row each: string or shape, where it is quoted, which Go package produces it (use the package names of `01-architecture.md`), whether a golden is needed. End with a list of bash-only snippets in skills (`mkdir -p`, `mv`, `cp`, `cat`, `jq`, `$(...)`, `KIT="$(sdkit where)"`) that P5-01 must rewrite.
- **Delete:** nothing.
- **Tests:** none.
- **Done when:** every contract in `02-contracts.md` has at least one row, and the list of bash-only snippets names file and line.

## P0-08 Golden harness and test helpers {#p0-08}

- **Wave:** 1 · **Depends:** P0-01 · **Size:** M
- **writes:** `tools/golden/`, `internal/testutil/`, `testdata/golden/.gitkeep`.
- **Do:**
  - `tools/golden/capture.sh <case-spec> [--bash-root DIR]` (bash, maintainer-only): builds the fixture repo from a script, runs the bash command from `--bash-root` (default `../go-port-oracle`), records command, stdin, stdout, exit code to `testdata/golden/<pkg>/<case>.txt` with placeholders for temp paths, timestamps, hashes and ids.
  - Case spec: a small text format (fixture script path, `cmd:`, `stdin:`, `env:`). Document it at the top of the tool.
  - `internal/testutil`: `TempRepo(t)`, `RunKit(t, repo, stdin, args...)` (builds the binary once per test run), `Golden(t, name, got)` (compares, with `-update` flag), `FakeCLI(t, name, script)`. A `FakeJira` helper is added by P3-02 in its own file. At most 150 lines for the whole package.
- **Delete:** nothing.
- **Tests:** none (helpers are exercised by their users).
- **Done when:** one sample case (e.g. `sdd mode`) is captured from the oracle and replayed by a Go test using `Golden`.

## P0-09 Windows host spike {#p0-09}

- **Wave:** 2 · **Depends:** P0-01, P0-06 · **Size:** S
- **writes:** `docs/go-port/spike-windows-hosts.md`, `.github/workflows/spike-windows.yml` (temporary; removed in P2-06).
- **Do:** answer, for Claude Code, Codex, Cursor, Gemini CLI and OpenCode on native Windows: (1) which shell runs a hook command, (2) whether `sdkit` is found on `PATH` as `sdkit.exe`, (3) how the command string is quoted (cmd vs PowerShell vs Git `sh`), (4) which shell the agent uses for a command from a skill, (5) the encoding of stdin JSON (BOM?), (6) path format in the JSON (`C:\...` or `/c/...`). Use the official documentation of each host and cite where you read it. Prepare a workflow that, on `windows-latest`, runs the built `sdkit.exe version` through `cmd /C`, `powershell -Command`, `pwsh`, and Git's `sh -c`, pipes JSON to `sdkit hook` from each, checks UTF-16 handling of `> file` in Windows PowerShell 5.1, and whether `exec.LookPath` finds an npm `.cmd` shim.
- **Needs the user:** running that workflow requires a push. Prepare it, then stop and ask. Record the findings in the doc, mark each item **confirmed (source)**, **confirmed (CI run)** or **unverified**.
- **Delete:** nothing.
- **Tests:** none.
- **Done when:** the doc answers all six questions per host with a status; the cards that rely on an `(assumption)` are listed with the answer that changes them.
