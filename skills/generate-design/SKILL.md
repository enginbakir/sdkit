---
name: generate-design
description: Designer and analyst skill. Turns a brief (free text, a file, a ticket or a pre-spec) into a UI design - screens, flows, states and design tokens as SVG plus tokens.json that open in Figma - with the designer role, asks the open questions, and has it reviewed. Works on its own for a designer, and is called by generate-specs (before the draft) when the analyst says the feature has a user interface.
license: MIT
metadata:
  author: spec-driven-kit
  version: "1.1"
---

Produces the UI design of a feature. It never writes the PRD, ADRs or acceptance criteria, never approves and never publishes a spec. `<kit>`, how to ask the user and how to start a fresh subagent: `spec-driven-dev/references/hosts.md`. Read `references/design.md` at step 2. It needs no spec, no Jira and no Figma account.

## Two ways to run it

- **Standalone** (a designer, or anyone with a brief). Input: a brief, as text in the request, a file path or a ticket id. Output: `<project-dir>/work/design/<slug>/ui-design/` (git-ignored, personal), or the directory the person names. Questions go straight to the person. The result is theirs to commit, share or import into Figma. To turn it into a spec later, give the directory to `generate-specs` (below).
- **Inside `generate-specs`** (step 3). Only when the analyst answered "yes" to "does this feature have a user interface?" in step 2 (`"ui": true` in `roles.json`); never decided from the pre-spec's wording. Input: the pre-spec and its settled answers. Output: `<project-dir>/work/spec-draft/<id>/ui-design/`, published with the spec.

## Steps

Print `[sdkit] generate-design - Step <n>/4: <name> - started|done` per step, and `[sdkit] generate-design - Step 2b/4: canvas offer - done (<asked: yes|no|skipped, why>)` for the canvas offer after step 2 (and `3b` after corrections). The canvas offer is a numbered step, not an aside: do not move on without printing its line.

1. **Input.** Standalone: take the brief as given, pick a `<slug>` (short, lower-case), read `<project-dir>/project.md` if present and find the project's existing tokens or UI conventions. Run the `prespec-gaps` role on the brief (light tier, fresh subagent) when it is long or vague, and ask its decisions and facts in rounds (host question tool, at most 4 per round, safest default first and marked); do not invent requirements. If an earlier `ui-design/` exists at the target, ask whether to change it or start over. Inside `generate-specs`: check that the gaps are settled (`sdkit review settle`), `roles.json` has `"ui": true` and `sdkit prespec check <name>` passes.
2. **Design** (`references/design.md`): run the `designer` role in a fresh subagent (`sdkit run-role designer` where the host has none) with only the brief, the answers, `project.md`, the tokens and the target path. It returns `screens.md`, `screens/*.svg` and `tokens.json`.
   **2b. Canvas offer** (host with the "Design preview" row of `spec-driven-dev/references/hosts.md`, today Claude Code; otherwise print the line as `skipped, host has none`): in the main session, with the host question tool (a subagent cannot ask), ask whether to show the screens as a Design canvas, following "Optional preview canvas" below. Asked in an interactive session, every time, by default; never assumed, never skipped because it is only optional, and never reduced to a sentence in the final summary. A non-interactive run prints `skipped, non-interactive`.
3. **Ask, correct.** The designer's open questions and assumptions are decisions only a person has: ask them in rounds, put each answer in the design, re-run the designer only for what changed. Standalone: show the person the screen list and flows and let them correct before step 4. Inside `generate-specs`: settle each answer (`sdkit review settle <id>-draft ...`).
   **3b.** If these corrections changed the design and a canvas was published (or the person declined it before the design changed), offer the canvas again with the same rules, in the main session, and print the `3b` line.
4. **Review and hand over.** Fresh reviewers check the design against `sdkit review plan design`, never the designer. Inside `generate-specs` that is step 4 of that skill (the design is part of the draft's review target), and step 3 of it gives the screen ids to the generator. Standalone it is optional: `sdkit review begin <slug>-design --target <dir>`, then `plan` and `record` as `generate-specs/references/review.md` describes; report the verdict as it is, and the screen list, the path and the Figma import steps. The hand-over lists the canvas outcome as a checked item: was it offered (steps 2b and 3b), and what the person said (published with its link, declined, skipped and why); a hand-over without it is incomplete.

## Optional preview canvas

On a host with a design canvas (`spec-driven-dev/references/hosts.md`, row "Design preview"; today Claude Code) offer, at step 2b and again at 3b, to show the screens as a Design canvas. Rules:
- **Ask first, every time.** Publishing sends the brief's content to claude.ai, and a pre-spec can hold customer or internal facts. Say what will leave the repo; without a yes, do nothing. Never publish by default or in a non-interactive run.
- **The files are the only source.** The canvas is built from `screens.md`, `screens/*.svg` and `tokens.json`, and is a view of them. A change the person asks for on the canvas is written back to those files (and to `screens.md`) before the design is reviewed or published with a spec; never publish a spec whose `ui-design/` differs from the canvas the person approved.
- **Where the canvas is built.** Write the canvas source files (the Design type's `canvas.json` and `.dc.html` pages) under `<project-dir>/work/design/<slug>/canvas/` (`<slug>` is the spec `<id>` inside `generate-specs`), never in the host's scratchpad or a temp directory: the phase guard (`SDD_MODE=strict`) refuses writes outside the project's free paths while no spec-driven task is open, and `<project-dir>/` is always free. Publish with that folder as the root. Copy the design's `screens/*.svg` unchanged into `canvas/screens/` and show them with `<img>`; the copies are derived and rebuilt from `ui-design/`, never edited. The folder is git-ignored with the rest of `work/` and is not part of `ui-design/` or the spec.
- **Not a Figma export.** The Figma route stays SVG and `tokens.json`; do not tell the person the canvas opens in Figma.
- A host without the row, or a person who says no, continues with the files alone.

## Using a standalone design in a spec

At step 2 of `generate-specs` the analyst may name an existing `ui-design/` directory. Then `generate-specs` copies it to `<project-dir>/work/spec-draft/<id>/ui-design/` instead of running the designer again (the designer re-runs only for what the pre-spec adds), writes `"ui": true`, and carries on from step 3: the ACs cite the screen ids, and the design is reviewed and published with the spec.

A design change after the spec is published is an `update-specs` change, not a re-run on the base.
