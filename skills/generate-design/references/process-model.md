# The process model

One JSON file, `<project-dir>/work/design/<id>/process.json`, written by the `flow-modeler`; `sdkit design build` turns it into `designs/<id>/process.bpmn` (with diagram coordinates) and `process.md`. Keys are in this order; unknown keys are rejected.

```json
{
  "spec": "login",
  "title": "Giriş akışı",
  "lanes": [{"id": "user", "name": "Kullanıcı"}, {"id": "sys", "name": "Sistem"}],
  "nodes": [
    {"id": "start", "type": "start", "lane": "user", "name": "Giriş sayfasını açar"},
    {"id": "form", "type": "userTask", "lane": "user", "name": "Formu doldurur",
     "doc": "Giriş formu: e-posta, parola, giriş butonu. Hatalı girişte buraya döner.", "acs": ["AC-login-1"]},
    {"id": "check", "type": "serviceTask", "lane": "sys", "name": "Bilgileri doğrular"},
    {"id": "ok", "type": "xor", "lane": "sys", "name": "Geçerli mi?"},
    {"id": "home", "type": "end", "lane": "user", "name": "Ana sayfa açılır"}
  ],
  "flows": [{"from": "start", "to": "form"}, {"from": "form", "to": "check"}, {"from": "check", "to": "ok"},
            {"from": "ok", "to": "home", "label": "evet"}, {"from": "ok", "to": "form", "label": "hayır"}],
  "noUI": ["AC-login-7"]
}
```

## Rules (`sdkit design check` enforces them)

- `spec` is the spec id. Ids (lanes, nodes) are letters, digits and `_`, starting with a letter; they do not start with `Lane_`, `Flow_`, `Participant_`, `Process_`, `Collaboration_`, `Diagram_`, `Plane_`, `Definitions_` and do not end in `_di` (the BPMN uses those).
- Types: `start`, `end`, `userTask`, `serviceTask`, `xor` (a decision), `and` (parallel work). Every node is in a defined lane and has a name.
- A **`userTask` is a screen candidate**: it needs `doc` (what the screen is for, what it shows, what the person can do) and at least one AC.
- Flows connect defined nodes, no self-loop, no duplicate. A `start` has no incoming flow, an `end` no outgoing one; every other node has both; every node is reachable from a start. A flow that leaves an `xor` carries a `label`. A loop back to an earlier step is allowed.
- Traceability: every AC `sdkit design acs` lists is on a node's `acs` or in `noUI`, never both; no AC the spec does not define or has withdrawn is cited.
- 8-20 nodes is the aim; a warning above 40.

## What the output is for

`process.bpmn` is BPMN 2.0 with lanes and a laid-out diagram (open it in bpmn.io or Camunda Modeler). `process.md` holds the same flow as a Mermaid diagram, which GitHub draws in the pull request, and a table of the screen candidates with their ACs. `design.meta.json` records the spec version and hash the design was built from, so a later spec change shows up as STALE. The files are generated and checked byte for byte: change the model and run `sdkit design build`, never edit them.

This is an orientation for the designer. It says which screens the work needs and how a person moves between them; it does not say what they look like.
