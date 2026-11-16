# Stacks

A stack = the language/framework part of the kit: how to verify, format and detect a project, plus default rules and example
policies. Everything else (roles, workflow, guards) is stack-independent. One `STACK` per repo (`STACK=` in `sdkit.env`).

A stack has two parts: **code** in `internal/stack/<stack>.go` (one file per stack, registered from `init()`) and **data** in this directory.

```
internal/stack/<stack>.go   implements the Stack interface (below) and calls Register
stacks/<stack>/
  permissions.txt        Claude Code permission patterns (one per line) added to settings.json "allow" for this stack
  rules/*.md             default conventions copied into <project-dir>/rules/ (project-owned afterwards)
  verify.d.example/*.example   example project policies (FAIL:/WARN: protocol; installed with the .example extension)
  <config files>         e.g. golangci.yml, copied to <project-dir>/
  known-pitfalls.md      optional seed for <project-dir>/known-pitfalls.md (stack-specific bug classes); init uses
                         templates/known-pitfalls.md (header and format, no entries) when absent
```

## The Stack interface (`internal/stack/stack.go`)

| Method | Does |
|---|---|
| `Name()` | the id used by `STACK=` and `sdkit init --stack` |
| `Detect(root)` | true when `root` looks like a project of this stack; `sdkit init` probes the stacks in order (go, spring-boot, react-native, jvm, nextjs, python; first match wins, `generic` if none), so it must not give false positives |
| `Tools()` | the tools the stack needs: `Required` or optional, the executable name, an install hint. `sdkit doctor` and the `check-tools` hook report a missing one (a `warn`, a `fail` only for a required one) |
| `IsSource(path)` | is `path` one of the stack's source files (decides which changed files count) |
| `Fmt(file)` | format one file in place; used by the post-edit hook; nil when there is nothing to do or the tool is missing |
| `Steps(ctx, env, reporter)` | the stack's checks; see below |

## Steps contract
`Env` carries `Root`, `KitDir`, `MergeBase`, `Fast` (true = cheap checks only), `Changed` (all changed files), `Source` (those `IsSource` accepts) and the project settings from `sdkit.env`. The `Reporter` gives `Step`, `Fail`, `Warn`, `Exec` and `Shell`. Rules:
- verify only what changed (nearest build file / package of each changed source file), not the whole repo;
- `Fast` must be cheap: typecheck/compile only, no tests, no lint, no full build;
- a missing tool is a `Warn` (skipped), never a `Fail`, unless the check cannot run without it;
- print little: the engine appends the summary line, callers read the tail.

The shared engine (diff base, changed files, `verify.d` policies, the green-result cache) is `internal/verify`; the Node stacks (`nextjs`, `react-native`) share `internal/stack/node.go`. `verify.d` policies are `*.sh` (run through `sh`), `*.ps1`, and on Windows `*.cmd` and `*.exe`.
