# Design: `sdkit ui`, a local panel for analysts

Status: phases 0 (spike), 1 (Connect and Health) and the Home, New spec and Run screens of phase 2 are implemented in `internal/ui` and `sdkit ui`; the review-finding cards are too; tasks and the rest are not. Issue: [#39](https://github.com/wecii/sdkit/issues/39) (basic web UI for non-technical roles: analyst, PM, QA).

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

## Spike results (phase 0)

Checked against Claude Code 2.1 with a real session, not a mock:

- A headless `claude -p --output-format stream-json` session waits on a blocking Bash command and gets its stdout back. That is the whole ask bridge: `sdkit ui ask` posts the questions to the panel, blocks, and prints the answers; the session continues with them. No MCP server, no host hook.
- `AskUserQuestion` is disallowed in panel sessions and the run prompt tells the model to use `sdkit ui ask` with the same JSON shape, so skill text stays unchanged.
- Step banners reach the stepper from the agent's text and from tool output (the model sometimes prints them with `echo`).
- The session must find this binary as `sdkit`; the panel prepends its own directory to `PATH` for the session.
- A real `generate-specs` run (pre-spec `communication-form`, a clone of the analyst repo, push disabled) went through the panel from step 1 to the output review: 3 question rounds (Turkish, single and multi select), draft review PASS, output review BLOCK then PASS, 18 acceptance criteria, stopped before publish as told. The skill text was not changed.
- The host prints a `result` line at the end of every turn, not only at the end of the session (a background subagent finishing starts a new turn), so the panel sends the last one when the process exits.
- Models write the step banner loosely (`Step 1/8 done`), so the parser accepts a missing name or colon.
- The agent's narration came out in English although `ANALYST_LANGUAGE=tr`; the run prompt now says to speak the skill's language in every message.
- Not done yet: publishing from the panel (git credentials, see Risks), session resume after a restart, the screens of phases 1-4, a look at the page in a browser.

## Phase 1 results (Connect and Health)

- **Connect** writes only to `sdkit.local.env` (personal, git-ignored), never to the shared `sdkit.env`: `TRACKER`, `JIRA_BASE_URL`, `JIRA_PROJECT`, `JIRA_USER`, `JIRA_EGRESS=allow`, `JIRA_API_TOKEN`, `BITBUCKET_EMAIL`, `BITBUCKET_TOKEN`. It refuses when git would not ignore that file. The form is pre-filled from the team's settings, so the person usually only adds the tokens. An empty token field keeps the saved token; tokens are never sent back to the page.
- Saving runs the checks at once: Jira `myself`, and Bitbucket on the repository of `origin` (not `/user`, which a repository access token cannot read; `ATCTT` tokens go as Bearer, as in `sdkit pr`). Failures are worded for the person ("token'ın süresinin dolmadığını kontrol edin"), not as HTTP codes.
- **Health** runs `sdkit doctor` in-process, groups its lines by plain title (five missing Jira settings are one item), puts what needs doing first, and shows only the news a person acts on; developer topics (CI gate, upgrade hints) and the dozens of passing lines stay under "Teknik ayrıntılar". Claude Code installed and logged in (`claude auth status`) is checked first.
- Looked at in Orca's built-in browser on a real clone: layout, Turkish copy and the error state read well. A real mouse click through the browser tool did not reach the page in that session (tabs and buttons), while the same handlers fired from script; treat clicking as unverified until it is tried in a normal browser.
- The panel's ready state is advisory: Start is not blocked, because `generate-specs` ran with a `project.md` that still had TODO sections.

## Phase 2 results (Home, New spec, Run)

- **Landing.** The page asks Health first: everything in order opens Start, anything missing opens the checklist. Saving the Connect form with working credentials and a ready Health moves on to Start by itself. A problem only the Tech Lead can fix (`project.md` with TODO sections) is shown as a note and does not count (`Item.Soft`); a missing Jira token, OpenSpec or Claude Code does.
- **Home** lists every spec from `spec.List`: pre-spec files, drafts and spec directories, each with a plain state from a new `Step.State` of `sdkit spec next`. A spec that has gone to a PR reads its state from its own `spec.meta.json` (awaiting the Tech Lead, or approved), not from review evidence in the local work directory, which a fresh clone does not have. Open work sorts first, approved last.
- **New spec** writes the pasted text to the pre-spec directory under a name the panel validates (letters, digits, `-`, `_`; it becomes a file, a branch and a Jira link), refuses when git would not ignore that directory, and refuses to replace an existing pre-spec unless asked.
- **Run** takes an action and an id, never free text: the page cannot hand the agent instructions (`Task`). The stepper names the eight `generate-specs` steps in Turkish; the banners stay English for the tools that read them. Stop kills the whole process tree and the page offers "Listeye dön".
- The agent echoed the raw gap list into the chat on the first step; the run rules now tell it to say in a sentence what it found, never to paste lists or tool output.
- Found by looking at it: a CSS `display` rule overrode `hidden` (a red dot stayed on a ready page), the Bitbucket rule matched the CI warning's `--ci bitbucket|github|gitlab` hint, and a merged spec showed as "in review" in a fresh clone.

## Review finding cards

The skill asks about a finding as a question (`triage.md`, "walk the open findings one by one") with the finding written into the question text. The panel draws it as a card without a change to the skill: the run rules tell the agent to add a `finding` object to that question, `{id, severity, where, problem, class, diff}`, and to keep the four answers (apply, answer differently, accept the risk, reject) as the options. `NormalizeQuestions` checks and bounds it on the server (unknown severity or class dropped, a finding with no problem becomes a plain question, text clipped) so the page can trust the shape and draws every field with `textContent`. The card shows the severity in words (Engelleyici, Önemli, Küçük), the agent's own class, the file and line, the problem in one sentence and the proposed edit as a coloured diff. A chosen option and a typed note travel together as one answer string.

Seen in the browser with a stand-in agent (`sdkit ui ask` with a finding question), not with a real review at the round cap; the agent following the rule at that moment is the part still to see.

One finding of this pass: Jira being unreachable (offline, service down) was counted as something to fix on the Connect page. It is now a note that does not block, since spec writing does not need Jira; refused credentials still block.

## Open questions

- Who pays for the Claude Code access of each analyst (subscription or API key)?
- What does QA do in the panel? #39 names the role but the analyst skills do not cover QA's work yet (the kit has a `qa` role only inside development).
- How are the binary and the clone installed for someone who has never used git?
- Is the Bitbucket token (HTTPS) acceptable for push, or is SSH the standard in the organisation? (Bitbucket Cloud or Server/Data Center changes the validation calls.)
- Language of the UI itself: `ANALYST_LANGUAGE` (default `tr`) only, or switchable?
