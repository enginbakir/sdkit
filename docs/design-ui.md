# Design: `sdkit ui`, a local panel for analysts

Status: draft. Not implemented. Issue: [#39](https://github.com/wecii/sdkit/issues/39) (basic web UI for non-technical roles: analyst, PM, QA).

## Problem

The analyst side of the kit (`generate-specs`, `update-specs`, `merge-specs`, `generate-tasks`) is used by analysts and product managers who do not work in a terminal. Two places hurt:

1. Onboarding: Jira URL and token, Bitbucket token, role, language, `openspec`, an agent host. One missed value and the first run fails with an error written for developers.
2. The flow itself: pre-spec, gaps, questions, review rounds, spec PR, then (after the Tech Lead merges) Jira tasks. The state is in files and in an agent conversation, so a non-technical person cannot see where they are.

## Decisions taken

| Question | Decision |
|---|---|
| Hosted or local | Local. `sdkit ui` is a subcommand of the existing binary, bound to the repo it is started in. Hosted panel (Atlassian OAuth, server-side runners) is out of scope for now. |
| Credentials | The user pastes tokens into the UI. No OAuth in this phase. |
| Agent host | Claude Code, running on the analyst's own machine. The headless driver targets Claude Code first; other hosts follow through the same `sdkit run-role` host table. Moving the agent to the cloud is a later step; until then everything is local. |
| Who | Analysts, PMs, QA and any role that does not develop. Tech Lead and Solution Architect stay on the CLI (they review and merge PRs). The development flow (`spec-driven-dev`) is not in the UI. |
| Access control | The repo is the boundary. Private repo: only someone who can clone it can run the panel. Public repo: anyone can run it, but without a tracker configured in `sdkit.env` the panel shows an empty state, not a broken one. There is no user database and no login. |
| Trackers | Jira + Bitbucket first. Other trackers later, behind the same `TRACKER` / `PR_HOST` settings. |

## What the UI is

A thin driver. It owns no workflow logic; the logic stays in the skills and in `sdkit`.

```
browser  <->  sdkit ui (localhost)  --reads-->   files: .sdkit/work, specs/, review state, `sdkit spec next <id>`
                                    --runs-->    deterministic verbs: doctor, prespec check, tasks plan-check, ...
                                    --drives-->  one headless agent session per run (the host runs the skill)
```

State is derived from the repo on every request (`sdkit spec next`, review state, `work/`). The server keeps no database; closing the browser or the server loses nothing.

### The hard part: the skills are agent conversations

`generate-specs` asks the analyst questions through the host's question tool and runs review rounds in fresh subagents. A UI cannot wrap a CLI call for this. It has to host the session:

- The server starts the agent host headless (the `sdkit run-role` code already builds these commands for every host) with the skill as the task.
- **Ask bridge.** `docs`/`hosts.md` gets one new row for the primitive "Ask the user" in a UI session: `sdkit ui ask --json <questions>`. It blocks until the user answers in the browser and prints the answer. The skill text does not change; only the host table does. This works on every host, with no MCP server.
- The stream of the session (step banners `[sdkit] generate-specs - Step n/8`, tool activity) is parsed into the stepper. Raw logs are behind a "details" toggle, never the main view.

## Screens

1. **Connect** (first run, and "fix this" from Health). Fields: Jira base URL, Jira email, Jira token, Bitbucket token, language, role. Each token is checked on save (Jira `myself`, Bitbucket current user) and the identity is shown back ("Connected as ..."), which also sets `JIRA_USER`. Tokens are never shown again, only "saved, replace".
2. **Health.** `sdkit doctor` in plain language: what is missing, one button each (install `openspec`, log in to the agent host, set the git remote credentials). No raw exit codes.
3. **Home.** The analyst's specs and their state, from `sdkit spec next`: draft, in review, PR open, merged, tasks created. A "needs you" badge when the next step is `person`.
4. **New spec.** Paste or upload the pre-spec (it lands in the git-ignored pre-spec directory, `sdkit prespec check` runs), pick the id.
5. **Run.** The 8-step stepper. A question is a form (single or multi-select with the recommended default marked, "accept all defaults"). Review findings that need a decision appear as cards with the proposed edit. End state: PR link, version, verdicts. The text "review PASS is not approval; the Tech Lead decides" stays visible.
6. **Tasks.** Enabled only for a spec merged on `origin/main`. Pick the Jira parent key (validated against Jira) and the SHA (listed from main, not typed). Show the `plan-check` summary and the AC-to-task table, one confirm button (the plan gate), then the created tasks as Jira links and the report.

## Secrets and local security

- Tokens go to the OS keychain; fallback is `sdkit.local.env` (git-ignored) with mode 600. They are passed to child processes as the environment variables the kit already names, and never returned to the browser.
- Listen on `127.0.0.1` only, random port, a per-start secret in the URL, `Origin`/`Host` checks against DNS rebinding and CSRF.
- The existing guards (`SECRET_GLOBS`, hooks, `sdkit scan secrets`) still apply to the agent session.

## Risks found while reading the code

- **The analyst still needs an agent host and its login** (an LLM subscription or key). The UI can detect and guide it, but cannot remove it. The host is Claude Code (decided); each analyst needs it installed and logged in.
- **Git push credentials.** `fix(spec): let git push prompt for credentials on the terminal` assumes a terminal. In a UI session there is none. Publishing needs HTTPS with the pasted Bitbucket token (credential helper scoped to the run) or SSH.
- **Install is still a step.** The user must have the binary and the clone. A one-click installer and a "clone this repo" helper belong to the same work item; without them the panel only helps people who already got that far.
- **Windows.** Process and path code follows `proc_unix.go`/`proc_windows.go`; the headless host driver has to as well.

## Phases

0. **Spike (riskiest first):** headless host driven by `sdkit ui`, the ask bridge, step banners parsed to a stepper, on `generate-specs` only. Exit criterion: one spec run to PR from the browser without touching a terminal.
1. **Connect + Health:** wizard, token validation, `doctor` in plain language.
2. **Home + New spec + Run:** the full `generate-specs` screens.
3. **Tasks:** `generate-tasks` with the plan gate.
4. **`update-specs`, `merge-specs`**, then a read-only view for the Tech Lead (PR and review state).

## Open questions

- Who pays for the Claude Code access of each analyst (subscription or API key)?
- What does QA do in the panel? #39 names the role but the analyst skills do not cover QA's work yet (the kit has a `qa` role only inside development).
- How are the binary and the clone installed for someone who has never used git?
- Is the Bitbucket token (HTTPS) acceptable for push, or is SSH the standard in the organisation? (Bitbucket Cloud or Server/Data Center changes the validation calls.)
- Language of the UI itself: `ANALYST_LANGUAGE` (default `tr`) only, or switchable?
