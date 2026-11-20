# The UI design

Layout, under `<project-dir>/work/spec-draft/<id>/ui-design/` (published as `specs/<id>/ui-design/`) or, standalone, `<project-dir>/work/design/<slug>/ui-design/`:

```
ui-design/
  screens.md                       screens, states, flows, assumptions, open questions
  screens/SCR-<feature>-<n>[-<state>].svg   one mockup per screen and state worth seeing
  tokens.json                      colors, typography, spacing, radii (stable key order)
```

## Rules

- **Ids.** `SCR-<feature>-<n>` per screen, stable like `AC-` ids: never renumbered or reused, a removed screen stays and is marked `withdrawn`. A state of a screen is a suffix of the mockup name (`SCR-login-1-error.svg`), not a new id.
- **Every pre-spec behaviour with a visible result has a screen or a state of one**; a screen exists because the pre-spec or a settled answer asks for it. Non-UI behaviour (a job, an API) is not designed here.
- **States.** Default, empty, loading, error and success, those that apply. A flow reaches no dead end: every screen names where each action leads, including the failure path.
- **Format for Figma.** SVG with plain shapes and text, named layers (`id` = the element's name in `screens.md`), no external fonts, images or scripts. Figma opens it with drag-and-drop or paste and the layers stay editable. `tokens.json` imports through Figma's variables or a token plugin. The kit cannot create a Figma file; a person imports the files and writes the link at the top of `screens.md` (`Figma: <url or none>`). Nothing in the kit requires Figma or an account.
- **Tokens.** Reuse the project's own tokens when the handoff names them; add only what a screen needs. JSON written with a stable key order (the file is hashed with the spec).
- **Accessibility basics.** Reading and focus order, text alternative for non-text elements, contrast of every text/background token pair.
- **Language.** Prose in `ANALYST_LANGUAGE`; ids, file names, token keys and layer ids in English.
- **Size.** As small as the pre-spec allows. Do not draw screens "for completeness".

## Traceability

Only with a spec. `generate-specs` step 3 writes each `AC-<feature>-<n>` that has a visible result with the `SCR-` ids it covers (`AC-login-3 (SCR-login-1-error)`); `design.md`'s `## Roles` section lists `ux` and `ui` against the screens they own. `sdkit review lint` does not check screen ids today; the design reviewers check that every screen is covered by at least one AC and every cited id exists.

## Review

`sdkit review plan design` gives the reviewers and standards (`templates/review.d/design.env`, overridable in `<project-dir>/review.d/`). The design is part of the draft target of review id `<id>-draft`, so a change to it makes earlier evidence stale like any other file, and the fidelity round on `specs/<id>/` (`generate-specs` step 7) covers it too.
