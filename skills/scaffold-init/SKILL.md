---
name: scaffold-init
description: Set up a repo for the spec-driven workflow. Writes the project profile to fill in (rules, project.md, sdkit.env, settings), adds files for other hosts when asked. Run once per repo by whoever sets it up; developers who clone an already set-up repo do not need it.
license: MIT
metadata:
  author: spec-driven-kit
  version: "2.3"
---

1. Check that the `sdkit` binary is installed: run `sdkit version`. If it is missing, tell the user how to install it (Homebrew on macOS/Linux, Scoop or the release zip on Windows; see the README) and stop.
2. If `<project-dir>/project.md` already exists, stop and say the repo is set up; offer `scaffold-doctor` instead.
3. Ask which hosts the team uses (any of `claude`, `codex`, `opencode`, `cursor`, `gemini`) and, if the repo is not obviously one stack, which stack (`go`, `spring-boot`, `nextjs`, `react-native`; auto-detected otherwise). Run `sdkit init --host <comma-separated hosts> [--stack <stack>] --mode plugin` from the repository root (`--mode` only matters for Claude Code) and show its output. It never overwrites an existing file unless `--force` is passed; do not pass `--force` without asking.
4. OpenSpec is needed only in an analyst repo (`generate-specs` outputs an OpenSpec change); `sdkit init --env analyst` sets it up. For a developer or standalone repo do nothing here: `spec-driven-dev` does not use OpenSpec.
5. Help fill in `<project-dir>/project.md` with the user (tracker, test layout, commit scopes, protected code, **risk rubric**). The rubric is the user's decision; propose wording but do not invent criteria.
6. Non-Claude hosts: tell the user to run `git config core.hooksPath .githooks` in every clone; for Codex, confirm multi-agent is enabled; for Cursor and Gemini, merge the generated `.cursor/hooks.json` / `.gemini/settings.json` into existing ones.
7. Run the `scaffold-doctor` skill and report what still fails.
