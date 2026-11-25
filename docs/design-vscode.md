# Design: the VS Code extension

Status: spike on `feat/vscode-extension` (separate branch until the v3 release; the version is not bumped before then).
Extension: **Spec-Driven Kit**, id `sdkit.sdkit`, in `editors/vscode/`. Audience as the panel (`docs/design-ui.md`): analyst, PM, QA.

## Decisions

| Question | Decision |
|---|---|
| UI | Native VS Code parts only: TreeView, QuickPick and InputBox, status bar, Output channel, the normal editor, SecretStorage. No webview, and no dependence on `sdkit ui`. |
| Logic | Stays in Go. The extension is a thin driver of `sdkit ext <verb>`; state is derived from the repo on every call. |
| Agent | A headless `claude -p` stream-json session, driven by Go (`internal/ui` `Run`), not the Agent SDK. Questions use the same `sdkit ui ask` bridge the skills already know. |
| Binary | Not embedded in the first slice: the extension takes `sdkit` from PATH (`sdkit.binaryPath`). Next: a single universal VSIX that downloads the release archive of its own version and checks its checksum. Embedding (6 platform VSIX) only if networks block the download. |
| Targets | VS Code first (Marketplace, Open VSX, `.vsix` on the release). macOS, Linux, Windows. |
| First slice | Onboarding (Health, Connect) and Specs/Run. Tasks, Reports, update/merge screens follow. |

## The Agents window (VS Code 1.147): not used

VS Code ships its own Claude and Codex agent SDKs and an Agents window, but (checked on 1.147.0, 2026-11) its extension points (`agentSessionsWorkspace`, `agentsWindowConfiguration`, the chat session providers) are proposed APIs, which a Marketplace extension cannot use, and the stable `vscode.d.ts` (1.146) has only chat participants. On the analyst's machine the window offered Copilot only, with no way to add Claude. So the extension runs its own headless session and draws it with native parts. Look again when the window opens to third-party providers.

## The protocol: `sdkit ext run <action> <id>`

No server. One JSON line per message on the standard streams (`internal/ext/run.go` is the source of truth):

```
stdout  {"type":"event","event":{kind: step|text|tool|result|status, ...}}
        {"type":"question","id":1,"questions":{"questions":[...]}}     the agent waits for the person
        {"type":"end","failed":false}                                  always last
stdin   {"type":"answer","id":1,"answers":{"<question>":"<label, label>"}}
        {"type":"stop"}                                                (end of input stops the run as well)
```

The ask bridge that the session's `sdkit ui ask` calls is a loopback listener inside the `ext run` process, with a key per run; it exists only while the run does. Worktrees, resume, the `SDKIT_TASK` filing, the Bitbucket credential for publishing and the run ledger are the panel's own functions, reused.

## Mapping to VS Code

| Panel | Extension |
|---|---|
| Stepper | TreeView "Run", built from the step banners (`steps.ts`) |
| Conversation | The run page: a virtual Markdown document (`sdkit-run:/<id>.md`, `transcript.ts`) drawn by VS Code's own Markdown preview in the main editor area and redrawn on every event. A "Şu an" box at the top says what matters now (a question waiting, the end, the last message); the conversation follows, with each question and the person's answer. View-only: questions are answered in the QuickPick, and the status bar brings a closed one back. The raw log stays in the Output channel. |
| Question form | QuickPick (single or multi), InputBox for free text; `ignoreFocusOut`, because the agent waits |
| Finding card, plan gate | Not in the spike: asked as a plain choice; later a diff editor and a Markdown preview |
| Waiting badge | Status bar item and a warning notification |

## Onboarding

- **Language:** the extension speaks Turkish (the analysts' language); the strings are in the views and `connect.ts`/`fixes.ts`, so another language is a later change.
- **Connect** stores through the panel's own `SaveConnection`, into `sdkit.local.env` (personal, git-ignored, mode 600), not into VS Code's SecretStorage: the kit's hooks and `sdkit` commands read the tokens from that file, and a terminal session would not see SecretStorage. The tokens go to `sdkit ext connection save` on **stdin**, never on a command line; no verb ever prints one. An empty token field keeps the saved token.
- **Fix rows:** `Item.Fix` carries a key (`connect`, `install-openspec`, `install-claude`, `login-claude`); the extension maps it to an action in `fixes.ts` (a terminal the person can see, or a page when Node is missing). The web panel offers `connect` only and ignores the rest. `sdkit` itself installs none of OpenSpec, Node or Claude Code.

## Specs: actions and new pre-spec

Clicking a spec offers what its state allows (`specActions`: start or continue `generate-specs`, work in the Tech Lead's comments or start a change, merge); a spec waiting on the Tech Lead offers nothing and says so. A run refreshes the list when it ends. **New spec** asks for a name (the kit's rule: letters, digits, `-`, `_`), opens an untitled Markdown tab with a template, and the title-bar button "Ön-speci kaydet" sends the text on stdin to `sdkit ext prespec <id>` (it refuses when git would not ignore the pre-spec directory; an existing name comes back as `exists` and is replaced only after the person agrees). Saving offers to start `generate-specs` at once.

## Git is not the person's job

A non-technical person never types `add`, `commit`, `push`, `fetch` or `pull`. Creating and updating a spec is already the agent's: `sdkit spec publish` stages, commits, pushes and opens the PR, and the kit moves between `spec/<id>` branches itself (worktrees keep the main folder on the base branch). What was left was staying current with the server, so `sdkit ext sync` does it:

- `git fetch` with the saved Bitbucket credential and no prompt; then, **only** when the folder is on the base branch, has no unsaved edits and is simply behind, `merge --ff-only`. It never discards, merges or rewrites anything.
- Everything else is left alone with a reason in plain words (JSON `state`: `current`, `updated`, `offline`, `dirty`, `other-branch`, `diverged`, `no-remote`). The extension says only what the person can act on: new changes taken in (status bar), unsaved edits or a diverged copy (warning). Offline and another branch are silent.
- It runs when the extension starts, on "Yenile", and before a run; a run on a copy that is dirty or diverged asks first.
- Not built yet: hand-editing a spec's files in the editor with a "send" button (it would start `update-manual`, which publishes); a "diverged" copy is only reported, never repaired.

## The welcome screen

A VS Code **Walkthrough** (`contributes.walkthroughs`, `media/*.md`), the native page in the middle of the window, not a webview. Each check has two steps, "✓ ok" and "✗ missing", shown by the context keys `sdkit.ok.<check>` / `sdkit.fail.<check>` that the extension sets from `sdkit ext health` after every refresh (`Item.Check` names the check: setup, claude, claude-login, openspec, jira, bitbucket, project). A check fails when an item of that name is a problem or a todo. It opens by itself the first time the extension runs, and later when the Spec-Driven Kit view is chosen while something the person must do is open (once per window; the Tech Lead's share does not count). The title bar of the Readiness view and the command "Karşılama ekranını aç" open it at any time.

## Read verbs

`sdkit ext health [--host <cmd>]`, `sdkit ext specs`, `sdkit ext spec <id>`, `sdkit ext connection` print one JSON document (the panel's Health, spec list and spec page) and exit; a failure is the reason on stderr and exit 1. The extension shows them as the Readiness and Specs views.

## Not done yet (spike limits)

- Not run inside a real VS Code window (no `code` on the build machine); the extension compiles and its decision logic is tested, the Go side is tested against a stand-in host.
- Health, Connect (tokens in SecretStorage), Specs list, new spec, hand edits: need the Go commands `sdkit ext health|specs|...` that share logic with `internal/ui`.
- Setup of Node, `openspec` and Claude Code: `sdkit` does not install them (`sdkit init --install-openspec` does `openspec` only, with npm present); the Onboarding view has to.
- One run at a time in the extension; the Go side already allows several specs in worktrees.
- Packaging, CI matrix, signing, publisher accounts.
