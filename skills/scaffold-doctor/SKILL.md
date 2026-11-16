---
name: scaffold-doctor
description: Check that this repo's spec-driven-kit setup is complete and say what to fix. Use after scaffold-init, after pulling changes to the project directory, or when a guard or the verify step seems not to run.
license: MIT
metadata:
  author: spec-driven-kit
  version: "2.1"
---

1. Run `sdkit doctor` in the repo root (if `sdkit` is not found, tell the user to install it: `docs/guide.md`) and show the output.
2. For each `FAIL`, say what to change and in which file. Do not edit `project.md` yourself: the risk rubric is the user's decision.
3. `WARN` lines about a host CLI (`codex`, `opencode`, `cursor-agent`, `gemini`) only matter for `sdkit run-role --host <host>`; mention them, do not treat them as failures.
4. Copy-mode installs and non-Claude hosts: if the kit moved on (a version change: offer `sdkit upgrade` first), offer `sdkit sync`; it keeps local edits and prints a diff for conflicts.
