# Tasks
- [ ] T1 schema | role: developer | size: S | writes: db/
- [ ] T2 api | role: developer | size: M | writes: api/,db/models.go | after: T1
- [ ] T3 ui | role: developer | size: M | writes: web/
- [ ] T4 docs | role: writer | writes: docs/,web/README.md
- [ ] T5 e2e | role: tester | size: L | writes: tests/e2e/ | after: T2,T3
