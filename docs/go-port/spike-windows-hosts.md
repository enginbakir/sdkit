# Windows host spike (P0-09)

Read from each host's official documentation on 2026-11-13. Status per answer: **confirmed (source)** (the page says so, quoted or paraphrased closely), **unverified** (not stated, or only implied), **unverified, needs CI run** (`.github/workflows/spike-windows.yml` step N would settle it; the workflow has not been run). No answer is **confirmed (CI run)** yet.

The six questions: (1) shell that runs a hook command, (2) `sdkit` found on `PATH` as `sdkit.exe`, (3) quoting of the command string, (4) shell the agent uses for a command from a skill, (5) encoding of the stdin JSON (BOM?), (6) path format inside the JSON.

## Sources

- Claude Code: [hooks](https://code.claude.com/docs/en/hooks), [setup](https://code.claude.com/docs/en/setup)
- Codex: [hooks](https://learn.chatgpt.com/docs/hooks) (was developers.openai.com/codex/hooks), [Windows sandbox](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- Cursor: [hooks](https://cursor.com/docs/agent/hooks), [terminal](https://cursor.com/docs/agent/terminal)
- Gemini CLI: [hooks](https://geminicli.com/docs/hooks/), [hooks reference](https://geminicli.com/docs/hooks/reference/), [shell tool](https://geminicli.com/docs/tools/shell/)
- OpenCode: [plugins](https://opencode.ai/docs/plugins/), [Windows (WSL)](https://opencode.ai/docs/windows-wsl/), [tools](https://opencode.ai/docs/tools/)

Two pages were read only in part (Claude Code hooks, 293k characters, first 200k read), so "not found" there means "not found in the part read".

## Claude Code

| # | Answer | Status |
|---|---|---|
| 1 | The `command` string goes to "Git Bash on Windows, or PowerShell when Git Bash isn't installed". Field `shell` (`"bash"` or `"powershell"`) chooses; default bash, or powershell on Windows without Git Bash. `CLAUDE_CODE_GIT_BASH_PATH` points at a Git Bash in an unusual place. | confirmed (hooks) |
| 2 | Shell form: resolved by Git Bash or PowerShell, which both find `sdkit.exe` on `PATH` (shell behaviour, not stated). Exec form (`command` + `args`): "requires `command` to resolve to a real executable such as a `.exe`"; `.cmd`/`.bat` shims are not spawnable. So `sdkit.exe` works in exec form. Whether the host's `PATH` includes the user's install dir is not stated. | exec form confirmed (hooks); `PATH` lookup unverified, needs CI run (step 1) |
| 3 | Shell form: quote each placeholder in double quotes (`node \"${CLAUDE_PLUGIN_ROOT}\"/scripts/x.js`). Exec form: "No shell tokenization happens on any platform", so no quoting problem. In PowerShell a command that is only a quoted path is printed, not run: set `"shell": "powershell"` and prefix `&`. | confirmed (hooks) |
| 4 | Bash tool: Git Bash when Git for Windows is installed; "Without Git for Windows, Claude Code runs shell commands via the PowerShell tool". The PowerShell tool is also available beside Bash when Git is present (on by default for claude.ai and Console accounts). | confirmed (setup) |
| 5 | Stdin encoding and BOM not stated. The docs' own PowerShell example reads `[Console]::In.ReadToEnd() \| ConvertFrom-Json`. Without Git Bash an exit code other than 0 or 2 counts as a failure, so a crash is not fail-open there (`onFailure: block` makes it uniform). | unverified, needs CI run (step 3, though the host's own bytes need a live Claude Code run) |
| 6 | File-tool `tool_input` paths: "the path arrives with backslash separators, even when your hook runs under Git Bash where `$PWD` looks like `/c/project`", e.g. `"file_path": "C:\\project\\src\\index.ts"`. The docs say to normalise separators before comparing. `cwd`, `transcript_path` and `CLAUDE_PROJECT_DIR` formats on Windows are not stated. | `file_path` confirmed (hooks); `cwd`/`CLAUDE_PROJECT_DIR` unverified |

Verdict: hooks can run `sdkit` natively. Prefer exec form (`command: "sdkit"`, `args: [...]`, shell `none`); fall back to shell form under Git Bash or PowerShell.

## Codex

| # | Answer | Status |
|---|---|---|
| 1 | The page does not name the shell. It offers `commandWindows` / `command_windows`, "an optional Windows-only command override" (example `py -3 C:\enterprise\hooks\...py`), which implies the plain `command` is not guaranteed to run on Windows. Commands run with the session `cwd`. | shell unverified; `commandWindows` confirmed (hooks) |
| 2 | Not stated. | unverified, needs CI run (step 1 and 5) |
| 3 | Not stated. Examples use `$(git rev-parse --show-toplevel)`, a POSIX form; the page does not say whether it works on Windows. | unverified |
| 4 | Not stated for commands in general. The Windows page says the app "can run natively in PowerShell with a Windows sandbox instead of requiring WSL"; it recommends the native sandbox first and WSL for Linux-native tooling. | PowerShell for the app confirmed (windows-sandbox); command shell unverified |
| 5 | "Every command hook receives one JSON object on `stdin`". Encoding not stated. | JSON on stdin confirmed; encoding unverified |
| 6 | Fields `session_id`, `transcript_path` (may be null), `cwd`, `hook_event_name`, `model`; Windows format of `cwd` not stated. Manifest hook paths are relative to the plugin root. | unverified |

Verdict: unknown. Use `commandWindows` to pin a Windows command (for example `sdkit.exe hook ...`) and confirm the shell with a live run.

## Cursor

| # | Answer | Status |
|---|---|---|
| 1 | "On Windows, the desktop app runs hook commands through PowerShell and pipes the JSON into the command." The CLI is not mentioned. | confirmed (hooks), app only |
| 2 | Not stated. `command` may be "a shell string, an absolute path, or a relative path". | unverified, needs CI run |
| 3 | PowerShell string. Team hooks without `.ps1`/`.bat`/`.cmd` get `.ps1` appended on Windows. Project hooks run from the project root, user hooks from `~/.cursor/`. | PowerShell confirmed; quoting rules unverified |
| 4 | Terminal page does not say which shell the agent terminal uses on Windows. | unverified |
| 5 | JSON is piped into the command; PowerShell scripts read `$input`, other programs read stdin. Encoding not stated. Windows PowerShell 5.1 pipes to a native exe in its console code page (settable), which is why a BOM or ANSI bytes are possible. | piping confirmed; encoding unverified, needs CI run (step 3) |
| 6 | Common fields include `workspace_roots`; `CURSOR_PROJECT_DIR` is exported. Windows format not stated. Enterprise config lives at `C:\ProgramData\Cursor\hooks.json`. | unverified |

Verdict: yes through PowerShell (app); `sdkit.exe` must be on the `PATH` PowerShell sees. Needs a run to confirm.

## Gemini CLI

| # | Answer | Status |
|---|---|---|
| 1 | Hook docs: `command` is "the shell command to execute"; no Windows shell named. The shell tool page says: "On Windows, commands execute with `powershell.exe -NoProfile -Command`" and "On other platforms, they execute with `bash -c`". Whether hooks use the same path is not stated. | tool shell confirmed (shell tool); hook shell unverified |
| 2 | Not stated. Hooks run "with a sanitized environment", which may drop or alter `PATH`. | unverified, needs CI run |
| 3 | If hooks share the shell tool's path, a PowerShell command string. | unverified |
| 4 | PowerShell: `powershell.exe -NoProfile -Command`. | confirmed (shell tool) |
| 5 | JSON on stdin; stdout must be a single JSON object, stderr is logged. Encoding not stated. | unverified, needs CI run (step 3) |
| 6 | `cwd` and `transcript_path` are absolute; Windows format not stated. `GEMINI_PROJECT_DIR` is "the absolute path to the project root"; `CLAUDE_PROJECT_DIR` is an alias. | unverified |

Verdict: probably yes through PowerShell, unconfirmed for hooks.

## OpenCode

| # | Answer | Status |
|---|---|---|
| 1 | No shell command hooks are documented. Extension is by plugins: JS/TS files in `.opencode/plugins/` (plugins "add tools, hooks, integrations, commands, agents"). A plugin would have to spawn `sdkit.exe` itself. | confirmed (plugins) that none is documented; plugin runtime unverified |
| 2 | Not applicable until a plugin spawns it; `exec`/`spawn` from the plugin runtime looks up `PATH`. | unverified, needs CI run |
| 3 | Not applicable (argument array from the plugin, no shell string). | unverified |
| 4 | Not stated. OpenCode "can run directly on Windows" but "we recommend using WSL for the best experience". The tools page does not name a shell. | native support confirmed (windows-wsl); shell unverified |
| 5 | Plugin passes its own data to the child's stdin; the encoding is whatever the plugin writes (UTF-8 if it passes a JS string). | unverified |
| 6 | Plugin entries accept absolute paths and `file://` URLs; format of tool paths is not stated. | unverified |

Verdict: no native shell hooks; unknown for plugin spawn. Recommended by the host: WSL. The generated plugin should call `sdkit.exe` with an argv array, never a shell string.

## Tally

Per host, of 6 answers: Claude Code 4 confirmed / 2 unverified (items 2 and 5 partly); Codex 1 / 5; Cursor 2 / 4; Gemini CLI 2 / 4; OpenCode 1 / 5. About 10 confirmed against 20 unverified. Everything needing a CI run is listed in the tables above; the workflow answers the host-independent half (shell behaviour, `.cmd` shims, PowerShell 5.1 encoding), not what each host does at run time.

## What the workflow checks (not yet run, needs the user to push and dispatch it)

`.github/workflows/spike-windows.yml`, `workflow_dispatch` only, `windows-latest`. `sdkit hook` does not exist yet, so the probes pipe a JSON payload into `sdkit.exe version` (which ignores stdin) and a tiny stdin byte dumper.

1. `sdkit version` through `cmd /C`, `powershell -Command` (5.1), `pwsh -Command` and Git's `sh -c`/`bash -c`; `where.exe` and `Get-Command` on `sdkit`.
2. JSON piped into `sdkit.exe` from each of the four shells.
3. First bytes of stdin as delivered by each shell to a child process (BOM? UTF-16? CRLF?).
4. What `> file` writes in Windows PowerShell 5.1 (expected UTF-16LE with BOM `ff fe`), in pwsh, and in `cmd`.
5. Whether `exec.LookPath` finds npm `.cmd` shims (`npm`, `openspec`), whether `exec.Command` runs them directly, and whether `cmd /C` does.

Note: this cannot show what a host does itself; that needs a live Claude Code, Codex, Cursor, Gemini CLI or OpenCode session on Windows.

## Cards that rely on an assumption, and the answer that changes them

No card text carries a literal `(assumption)` tag. The tag appears in `docs/go-port/03-cross-platform.md:6` and is referenced by `docs/go-port/phases/P1-enforcement-core.md:5` and `docs/go-port/07-risks.md:9`. The cards below depend on Windows host behaviour.

| Where | Depends on | Answer that changes it |
|---|---|---|
| `docs/go-port/03-cross-platform.md:6` (item 2, `cmd /C` for user command strings, tagged `(assumption)`) | Which shell users' `VERIFY_CMD` strings are written for | If hosts and users run PowerShell/Git Bash, `cmd /C` is wrong: default to `sh -c` when Git's `sh` is on `PATH`, else PowerShell. Step 1 shows which of the four shells runs `sdkit.exe` as expected. |
| `docs/go-port/03-cross-platform.md:3,8` (item 8, UTF-16 and BOM tolerance) | Encoding of stdin JSON and `>` redirects | Step 3 and 4. If a host or shell sends a BOM, `sdkit hook` must strip it on stdin, not only in `envfile`. |
| `docs/go-port/03-cross-platform.md:7` (item 3, `exec.LookPath` and `.cmd`) | Whether `LookPath` finds `.cmd` and whether Go refuses to run it | Step 5. Go 1.19+ refuses relative-path results, and `.cmd` needs `cmd /C`. |
| `docs/go-port/phases/P2-setup.md:5,58` (P2-03, hook wiring per host) | Questions 1 to 3 per host | Claude Code: emit exec form (`command: "sdkit"`, `args`). Codex: emit `commandWindows`. Cursor: PowerShell string. Gemini CLI and OpenCode: unknown until a live run. OpenCode: JS plugin, no shell hook. |
| `docs/go-port/phases/P2-setup.md:92` (P2-06, `tests/windows-smoke.ps1`, deletes this workflow) | Which shells the smoke test must cover | Steps 1 and 2 decide the shell list. |
| `docs/go-port/phases/P0-foundation.md:85` (P0-06 `proc.Shell(cmdString)`) | Same as item 2 above | Same as item 2 above. |
| `docs/go-port/phases/P0-foundation.md:51` (P0-02 CRLF, BOM, UTF-16) | Same as item 8 above | Same as item 8 above. |
| Guard cards in `docs/go-port/phases/P1-enforcement-core.md` (protected paths, frozen files) | Path format in hook JSON | Claude Code sends `C:\...` with backslashes (confirmed): guards must normalise with `filepath.ToSlash` and compare case-insensitively. Other hosts unknown; do the same for all. |
| `docs/go-port/phases/P1-enforcement-core.md:123,133,143` (tool names, wrappers on Windows) | `LookPath` and `.cmd`/`.bat` | Step 5. |

## Findings that block or reshape a card

- **P2-03 cannot finish for Codex, Gemini CLI and OpenCode** until a live Windows run settles the hook shell and `PATH`. P2 stays blocked per the card.
- **OpenCode has no documented shell hooks.** If the Go port promises "hooks on every host", the OpenCode adapter on Windows is a plugin that spawns `sdkit.exe`; its card must say so.
- **Claude Code without Git Bash fails closed on any exit code other than 0 or 2.** `sdkit hook` must only ever exit 0 or 2 on Claude Code, which matches the "never block on own error" rule in `CLAUDE.md`.
- **`cmd /C` as the default for `VERIFY_CMD` (`03-cross-platform.md:6`) is contradicted by the sources**: Claude Code, Cursor, Gemini CLI and the Codex app all use PowerShell or Git Bash, never `cmd`. Recommend changing the default.
