# Design: `sdkit ui`, a local panel for analysts

Status: shipped in 2.3.1: Connect (Configuration), Health (Onboarding), Specs (list, spec page, hand edits, new spec, run, review-finding cards, tasks with the plan gate, update and merge entries), Reports, resume, several specs at once in worktrees. Open: QA in the panel, publishing from a worktree seen against a real Bitbucket, a read-only view for the Tech Lead. Issue: [#39](https://github.com/wecii/sdkit/issues/39) (basic web UI for non-technical roles: analyst, PM, QA).

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

## Tasks, update and merge, and the sidebar layout

- **Tasks.** An approved spec (state from its `spec.meta.json`) gets "Task üret". The setup page takes the Jira parent key, which the server asks Jira for (`/rest/api/2/issue/<key>`; unknown, forbidden and unreachable are worded apart), and the version, which is a list of the commits of `origin/<base>` that left the spec approved (`spec.ApprovedCommits`, after a best-effort `git fetch`, never a prompt). Nobody types a SHA. `POST /api/run` re-checks both on the server (the SHA must be in that list, the parent must exist) before a session starts; the page sends `action`, `id`, `parent`, `sha` and nothing else, and the wording of the task is ours (`Task`).
- **Plan gate.** The run rules tell the agent to put a `plan` object on the one confirmation question: `{summary, tasks:[{ref,title,risk,acs,dependsOn,roles}]}`. `NormalizeQuestions` bounds it (60 tasks, 30 ACs each, clipped text; an empty plan is dropped) and the page draws the AC-to-task table with one "Planı onayla ve Jira'da oluştur" button and "Henüz değil" (a typed note travels with the answer). The skill text is unchanged; its step 5 is what the card carries out.
- **Result.** The task text keeps `results.jsonl` and `report.md` under `<project-dir>/work/tasks/<id>/`; the panel reads the outcome from those files (Jira links from `JIRA_BASE_URL`), not from what the agent says. A report that lists a task `NOT CREATED` is shown as not done.
- **update-specs, merge-specs.** Buttons on the home list, by state: "Yorumları işle" for a spec awaiting the Tech Lead, "Değişiklik yap" and "Birleştir" for an approved one. They send only the action and the id. Whether there is anything to merge is the skill's own `sdkit spec drift` check (siblings live on other branches, so the panel cannot count them from the working tree); the task tells the agent to say so and stop when there is not.
- **Layout.** A sidebar replaces the top tabs and the content uses the full width: two columns on Home, Connect and the tasks setup, and a steps column next to the conversation on a run. It folds to one column under 1000 px.
- Seen in the browser against a scratch repo with a Jira stand-in and a stand-in agent (the plan question through `sdkit ui ask`): setup, parent check, version list, plan card, confirm answer, result card. Not seen: a real `generate-tasks` run writing to a real Jira.

## Spec page, hand edits and publishing

- **Status list and spec page.** Home lists every spec with a six-stage bar (pre-spec, questions and draft, review, PR, Tech Lead approval, tasks) from `Flow(state)`; "Aç" opens the page. The page repeats the bar with "şu an buradasınız" on the current stage, what was done so far (from the spec's own meta: versions sent to a PR, review verdict, changes), and only the actions of this stage (`Actions`): continue / send to PR before approval; process the Tech Lead's requests or "Elle düzelttim, devam et" while the PR is open; start a change, edit by hand, tasks, merge once approved. Stages and actions are one table each, tested.
- **Hand edits.** The page lists the Markdown files of the spec directory and of its draft (nothing else; the kit's own `spec.meta.json` and `change.json` are not listed). A path outside that list, a symlink or a missing file is refused on read and write; no file is created; nothing is written while a run is going. Before approval the files are editable on `spec/<id>` (a button runs `sdkit spec branch` when the repo is elsewhere); an approved base is read-only and only the files of an open change are editable, on that change's branch. The edit is the instruction: "Elle düzelttim, devam et" runs `update-specs` with a task that starts from `sdkit spec changed` and the git diff and keeps the analyst's text authoritative, which the skill already says (no new skill was needed).
- **Start a change.** On an approved spec the panel runs the kit's `sdkit spec change new` itself, named by who is connected and the time (`<user>-yymmdd-hhmm`, from `JIRA_USER`; the page sends no name), then either hands the run to `update-specs` (which asks the person what changes, through the ask bridge) or opens the empty change for hand edits. Git refuses when the working tree has unsaved edits and the person is told in plain words.
- **Publishing from a session.** The agent's `sdkit spec publish` pushes and opens the PR as it always did; what was missing is the credential. Every session gets `GIT_TERMINAL_PROMPT=0` and an `Authorization` header for `https://bitbucket.org/` only, made from the saved Bitbucket token (`x-token-auth` for `ATCTT` tokens, the e-mail otherwise), passed in the process environment of that session, never on a command line and never written to git's config. An SSH remote or another host gets nothing and keeps what git has. Not yet seen against a real Bitbucket push.

## Sidebar names and a run that goes on

The sidebar reads Specs (the list and the spec pages), Configuration (the Connect screen) and Onboarding (Health). A run is a process on the server, so it goes on whichever page or browser tab is open. Two things made that look like a stall and are fixed: a question waiting for the person was drawn on a page they had left, so Specs now shows a count badge and the tab title says "(1) Cevabınız bekleniyor", and a blue dot while a run is going; and a reloaded page did not know what the replayed events were a run of, so `GET /api/run` returns the running flag and the last action and id.

## Reports, and how a report finds its spec

The kit files a session report under the host's session id (`<project-dir>/reports/<session>.json`, schema `value-report/1`); it says nothing about which spec the session worked on, and the usage ledger is keyed by task slug, which the analyst flow does not set. So the link cannot be read from the kit's records. The panel starts every run for a spec, so it writes down `{session, spec, action, start, end, outcome}` in `<project-dir>/work/ui/runs.jsonl` (the session id is the `session_id` of the host's first stream-json line; a start line when it appears, an end line when the process ends, so a panel that dies mid-run still keeps the link). A report is linked to a spec only through that ledger; a session started in a terminal shows as "Panel dışı oturum" and is not matched by time or branch name.

- **Reports** (sidebar): totals per spec (sessions, tokens, estimated cost, last run; clicking opens the spec) and every session with its spec and action, outcome, tokens and cost, and the kit's own markdown report on "Raporu aç". A run whose report is not stored yet is built from the records (`sdkit report session <id>`), which the kit never stores while a session may still run.
- **Spec page**: a "Raporlar" card with that spec's runs and their sum.
- **The spec id as the task (kit change).** Every run gets `SDKIT_TASK=<spec id>`. `project.Resolve` reads it into `Project.Task` and `state.ActiveSlug` returns it first, so the hooks of the session and of its subagents file every token under `work/<spec-id>/usage.jsonl`, with no state file; unset, nothing changes. `sdkit report task <id>` is then the spec's own report across all its sessions (per role, subagents included), the spec page shows that role breakdown, and a session that wrote to a spec's ledger is linked to the spec by the kit's own record, a terminal session with `SDKIT_TASK` exported included. The panel's run ledger stays for what the run was for (action, outcome) and for sessions before this change. Tokens spent before it sit under `_misc` and are not moved.
- **Honesty about numbers.** Cost is the API-priced equivalent, not the bill. Only the kit's `partial` (unmeasured or unpriced runs) and `unavailable` states make a figure a lower bound and say so; `estimated` and `recorded` do not.
- Seen against the real `tff-specs` reports (18 sessions, all unlinked, as they should be) and a scratch repo whose ledger links two of them to a spec.

## Continue where it stopped, files under a running job

- **Resume.** "Devam et" (and the update and merge runs) continue the latest panel session of that spec and action that the host still holds (`claude --resume <id>`; the host keeps the same session id, so the run ledger and the reports stay one session). `cmd` answers "does the host still have it" by looking for `<config-dir>/projects/*/<id>.jsonl` (`Config.SessionExists`), so a session the host has dropped starts a new one instead of failing. The spec page says "Önceki oturumdan kaldığı yerden sürer" and offers "Yeni oturumla başla" (`fresh`). The resumed task adds one sentence: carry on, check `sdkit spec next` first, do not start the steps over. A tasks run is never resumed (its parent and version are chosen again). A resumed session carries its whole context, so a long one is not cheap; fresh is the way out.
- **Loading.** From the click until the host says anything the page shows a spinner with a clock ("Oturum başlatılıyor", or "Önceki oturum açılıyor; büyük oturumlarda birkaç dakika sürebilir" when resuming), then "Çalışıyor · <step>"; it hides while a question waits for the person.
- **Files while it runs.** The running screen carries the same file panel as the spec page, read-only (the server marks every file non-editable while a run is going), refreshed on each step and every few seconds, keeping the open file. The pre-spec is in the list as its own group: readable at every stage, editable only until a draft exists.

## Several specs at once

A spec was a branch in one working tree, so moving to another spec meant switching the branch under the first, and the panel allowed one run in all. Now each spec works in a git worktree of its own and runs go side by side.

- **A tree per spec.** `Provision` makes `../<repo>-wt/spec/<id>` on branch `spec/<id>` (an existing local or remote branch is used, else a new one from `origin/<base>`). git leaves out of a checkout what a session needs, so the ignored *files* of the main tree (`sdkit.local.env` with the tokens, `.claude/settings.local.json`) are linked, and the ignored *directories* (the host's skills and agents, `prespec/`) are copied once: git's `dir/` ignore rules do not match a link to a directory, which showed as untracked work. The work directory is never shared, so each spec has its own drafts, review evidence and token ledger. After `sdkit upgrade` a worktree keeps its old skill copies until it is removed and made again.
- **What stays in the main tree.** A spec that already has work there git would not carry (a draft in its work directory, untracked files under `specs/<id>`, its own branch checked out) keeps working there, and only one such spec can run at a time (the second is told the main tree is busy). Its session also belongs to that tree: the host files sessions per working directory, so the run ledger records the directory and a run is resumed only from the tree it ran in.
- **Runs.** One run per spec, at most `Config.MaxRuns` (3) in all; the slot is taken before the tree is made, so two clicks cannot both get through. Every run has its own event log (`/api/events?id=`), its own questions (`sdkit ui ask` sends `SDKIT_UI_SPEC`, the questions carry the spec) and its own stop. `/api/runs` says what is going and how many questions wait; the Specs list shows "çalışıyor" and "cevabınız bekleniyor" per spec, "Çalışmayı aç" goes back to a running one, and the sidebar and tab title count all of them.
- **Reading.** A spec's page, files, task result and ledger are read from its own tree; the list takes a spec's state from its worktree; Reports scan every tree. Cleanup is explicit: "Çalışma alanını temizle" on an approved spec removes the worktree (git refuses with unsaved work; the branch is kept).
- Seen with a stand-in host: two specs running at once in two worktrees, one asking a question that only its own page shows, answered while the other went on; the cap, the one-run-per-spec rule and the busy main tree are tests. Not seen: two real `claude` sessions in parallel against a real account (rate limits), and a real `sdkit spec publish` from a worktree.

## Look and structure, navigation, questions that must not get lost

- **Look.** A dark, minimal telemetry console in the structure of the wireframe the design started from: a sidebar (brand, search, navigation with the active page filled, whose panel and which repo and branch at the foot), a top bar with breadcrumbs and status chips, a centre with a heading, four counter tiles and the writing box at the bottom, and a right rail listing the specs as cards. One accent per meaning (green done, blue running, amber waiting, red problem), monospace for ids, paths, tokens and cost, hairline borders, no shadows. The six-stage flow is a thin progress segment per stage.
- **Navigation.** Specs in the sidebar and the first crumb always land on the list (the section switch left the inner view, a spec page or a running screen, where it was). The rail lists every spec on every Specs page, so moving from one spec to another is one click, and a spec with a waiting question opens straight on its question. The sidebar search filters the rail; the tiles filter it by state.
- **A question that does not arrive.** Found: the rules told the agent to pass the questions as `--json '<json>'`, and an apostrophe in Turkish text (`Spec'in`) breaks that quoting in the shell, so nothing reached the panel while the agent still said it had asked. Now the rules say to give the JSON on standard input as a heredoc with a quoted delimiter, and to fix and rerun on an error instead of writing the question into the chat. The server also accepts the shapes agents send (one question without the wrapper, a bare list, options as plain strings). A refused question is written to that spec's log and shown ("Ajan bir soru sormayı denedi ama panel kabul etmedi: ..."). On the page: a question is marked seen only after it is drawn, and one that cannot be drawn is shown as plain text with an answer box; the poll compares the server's waiting count with the forms on the page and fetches again when they differ; a chip in the top bar ("N soru cevabınızı bekliyor") opens the waiting run from any page; and when a run ends on a message that is a question with no form behind it, the page says so and points at "Devam et", whose resumed prompt tells the agent to ask it again with the form.
- **More ways a question went missing, found on a real run.** (1) The session's `sdkit` was the installed 2.3.0 (a panel started from a binary not called `sdkit` did not get its own first on PATH), which does not say which spec asks, so the question had no owner and no spec page showed it. Now the panel puts a link called `sdkit` to its own binary first on the session's PATH (`sdkitShim`), a question with no owner goes to the only run going or, with several, is shown on every spec page and counted in the chips, and the page is told how many are unowned. (2) The host's Bash tool ends a command after two minutes, so a `sdkit ui ask` waiting for a person was killed, the question vanished from the page and the agent re-sent it later. Sessions now get `BASH_DEFAULT_TIMEOUT_MS` of two hours and `BASH_MAX_TIMEOUT_MS` of four, and a question that is withdrawn anyway is logged ("ask-expired") and said on the page. A killed session can leave its `sdkit ui ask` process behind with its question still waiting; answering it releases it. (3) Two sessions for one spec (one per panel) both asked: the panel does not know the other panel's runs, so run one panel per repo.

## Open questions

- Who pays for the Claude Code access of each analyst (subscription or API key)?
- What does QA do in the panel? #39 names the role but the analyst skills do not cover QA's work yet (the kit has a `qa` role only inside development).
- How are the binary and the clone installed for someone who has never used git?
- Is the Bitbucket token (HTTPS) acceptable for push, or is SSH the standard in the organisation? (Bitbucket Cloud or Server/Data Center changes the validation calls.)
- Language of the UI itself: `ANALYST_LANGUAGE` (default `tr`) only, or switchable?
