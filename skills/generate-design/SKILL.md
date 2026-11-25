---
name: generate-design
description: Analyst environment. Turns a published spec into a BPMN 2.0 process flow (lanes, steps, decisions, the screens a person meets) that gives the designer a first picture of the work before the real UI design. Offered at the end of generate-specs when the feature has a user interface; also usable alone on any spec that already exists. Never changes the spec; the result goes in its own PR.
license: MIT
metadata:
  author: spec-driven-kit
  version: "2.0"
---

Makes a process flow from a spec that already exists: not a UI design, an orientation for the person who will design it. A `flow-modeler` role writes a small model; `sdkit design` checks it against the spec and builds the BPMN file and its Markdown twin; the analyst corrects; a separate PR carries the result. The spec, its hash and its review are never touched, so `generate-specs` is never interrupted by this. `<kit>`, how to ask the user and how to start a fresh subagent: `spec-driven-dev/references/hosts.md`. The model format is in `references/process-model.md`; read it at step 2.

**Two rules that never bend.** (1) Nothing is written under `specs/<id>/`; the design lives in `designs/<id>/`. (2) The flow shows what the spec says: a step, branch or screen the spec does not state is a question for the analyst (and, if it is a real gap in the spec, a job for `update-specs`), never something the model quietly adds.

## Language

Everything written for people (questions, step names, the `doc` of each screen, the PR body) is in `ANALYST_LANGUAGE` (`sdkit.env`, default `tr`; `sdkit.local.env` wins). Ids, types, keys and file names stay English.

## When it runs

- **From `generate-specs`.** Its last step asks the analyst whether the feature needs a user interface; on yes it invokes this skill with the spec id.
- **Alone.** Any time, for any `<id>` whose spec exists (on its branch or merged). No spec, no design: step 1 stops and points at `generate-specs`.

## Steps

Print `[sdkit] generate-design - Step <n>/4: <name> - started|done` per step.

1. **Input.** `sdkit design acs <id>` must succeed: it reads `<SPECS_DIR>/<id>/` in the working tree and lists the acceptance criteria the process has to account for. If it fails, stop and say the spec is missing or on another branch (check out `spec/<id>` or the base branch). If `designs/<id>/` already exists, run `sdkit design check <id>`: a STALE message means the spec changed since; ask whether to rework the process or leave it. Note the spec version it prints.
2. **Model.** Run the `flow-modeler` role in a fresh subagent (`sdkit run-role flow-modeler` where the host has none) with the spec directory, the `sdkit design acs` output, `<project-dir>/project.md`, the language and the target `<project-dir>/work/design/<id>/process.json`. It runs `sdkit design check <id> --model <that file>` itself; run it again and send errors back to the role until it is clean.
3. **Ask, correct.** Show the analyst the lanes, the screen candidates (each `userTask` with its `doc` and ACs) and the decisions in plain text, not the JSON. The role's assumptions and open questions are decisions only a person has: ask them in rounds (host question tool, at most 4 per round, the safest default first and marked), put each answer into the model, re-run the role only for what changed and check again. Never guess. A gap that belongs in the spec itself is reported, not patched here.
4. **Build and hand over.** `sdkit design build <id>` writes `designs/<id>/` (`process.json`, `process.bpmn`, `process.md`, `design.meta.json`); `sdkit design check <id>` must pass. Tell the analyst where the files are and how to read them: `process.md` shows the flow as a diagram in the pull request, `process.bpmn` opens in bpmn.io or Camunda Modeler. Ask whether to publish. On yes: write the PR body (what this is, spec id and version, "orientation for the designer, not a UI design, not an approval", how to open the BPMN) to a file and run `sdkit design publish <id> --title "docs(design): <id> process flow" --body-file <file>`. It cuts `design/<id>` from the base branch, commits only `designs/<id>/`, pushes and opens the PR; never push or open the PR yourself. Report the PR URL, the spec version it was made from and any open questions.

After the spec changes (`update-specs`), `sdkit design check <id>` says STALE: re-run this skill on the new version; the PR's branch is reused.
