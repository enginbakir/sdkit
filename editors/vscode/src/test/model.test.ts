import test from 'node:test';
import assert from 'node:assert/strict';
import { actionsOf, mainItems, whereIs, SpecItem } from '../model';

const spec = (o: Partial<SpecItem>): SpecItem => ({ id: 'a', state: 's', label: 'L', waiting: false, continue: false, update: false, merge: false, tasks: false, flow: [], ...o });

test('the checklist shows main items only, problems first', () => {
  const m = mainItems({ ready: false, items: [{ level: 'todo', title: 't', main: true }, { level: 'ok', title: 'x' }, { level: 'problem', title: 'p', main: true }] });
  assert.deepEqual(m.map(i => i.title), ['p', 't']);
});

test('a spec offers only the actions its state allows', () => {
  assert.deepEqual(actionsOf(spec({ continue: true })), ['generate-specs']);
  assert.deepEqual(actionsOf(spec({ update: true, merge: true })), ['update-specs', 'merge-specs']);
  assert.deepEqual(actionsOf(spec({ waiting: true })), []);
});

test('whereIs names the current stage, or the state label when none is current', () => {
  const flow = [{ key: 'a', label: 'One', status: 'done' as const }, { key: 'b', label: 'Two', status: 'current' as const }];
  assert.equal(whereIs(spec({ flow })), '2/2: Two');
  assert.equal(whereIs(spec({ label: 'Onaylı' })), 'Onaylı');
});

import { groupHealth, groupSpecs, healthSummary } from '../model';

test('a problem only the Tech Lead can fix is not something the person must do', () => {
  const d = { ready: true, items: [
    { level: 'problem', title: 'project.md', main: true, soft: true },
    { level: 'problem', title: 'Jira token', main: true },
    { level: 'ok', title: 'Claude Code', main: true },
    { level: 'ok', title: 'git' },
  ] };
  assert.deepEqual(groupHealth(d).map(g => [g.label, g.items.map(i => i.title)]),
    [['Sizi bekleyenler', ['Jira token']], ['Tech Lead\'e ait', ['project.md']], ['Hazır', ['Claude Code']], ['Teknik ayrıntılar', ['git']]]);
  assert.equal(healthSummary(d), 'Başlamadan önce yapılacak 1 şey var');
  assert.equal(healthSummary({ ready: true, items: [{ level: 'problem', title: 'p', main: true, soft: true }] }), 'Çalışmaya hazır');
});

test('specs are grouped by who acts next, empty groups left out', () => {
  const s = (id: string, o: Partial<SpecItem>) => spec({ id, ...o });
  const g = groupSpecs([s('a', { continue: true }), s('b', { waiting: true }), s('c', {}), s('d', { continue: true })]);
  assert.deepEqual(g.map(x => [x.label, x.items.map(i => i.id)]), [['Sizi bekleyenler', ['a', 'd']], ['Tech Lead\'de', ['b']], ['Onaylı', ['c']]]);
  assert.deepEqual(groupSpecs([s('c', {})]).map(x => x.label), ['Onaylı']);
});

import { failingChecks, needsAttention } from '../model';

test('a missing agent install fails the login check as well', () => {
  const f = failingChecks({ ready: false, items: [{ level: 'problem', title: 'x', check: 'agent' }, { level: 'ok', title: 'y', check: 'jira' }] });
  assert.deepEqual([...f].sort(), ['agent', 'agent-login']);
});

test('only what the person must do opens the welcome screen, not the Tech Lead\'s share', () => {
  assert.equal(needsAttention({ ready: true, items: [{ level: 'problem', title: 'project', check: 'project', soft: true }] }), false);
  assert.equal(needsAttention({ ready: false, items: [{ level: 'todo', title: 'jira', check: 'jira' }] }), true);
});

import { blocked, fileItems, menu, sendActions, SpecDetail, validSpecId } from '../model';

test('spec names follow the kit: no space, no dot, at most 60 characters', () => {
  assert.equal(validSpecId('odeme-akisi_2'), true);
  for (const bad of ['', 'bad id', '-x', 'a.b', 'ş', 'x'.repeat(61)]) assert.equal(validSpecId(bad), false, bad);
});

import { blockers } from '../model';

test('a run is blocked by setup, Claude Code and OpenSpec, not by Jira or the Tech Lead\'s profile', () => {
  const d = { ready: false, items: [
    { level: 'problem', title: 'kurulum eksik', check: 'setup' }, { level: 'todo', title: 'jira', check: 'jira' },
    { level: 'problem', title: 'project', check: 'project', soft: true }, { level: 'problem', title: 'claude yok', check: 'agent' },
  ] };
  assert.deepEqual(blockers(d).map(b => b.check), ['setup', 'agent', 'agent-login']);
  assert.equal(blockers(d)[2].title, "Ajana giriş yapılmamış");
  assert.deepEqual(blockers({ ready: true, items: [] }), []);
});

import { staleBeforeRun, syncNotice, SyncResult } from '../model';

const sync = (state: SyncResult['state']): SyncResult => ({ state, base: 'main', branch: 'main', behind: 2, message: 'm' });

test('only news and problems the person can act on are said after an update', () => {
  assert.equal(syncNotice(sync('updated'))?.level, 'info');
  assert.equal(syncNotice(sync('dirty'))?.level, 'warn');
  assert.equal(syncNotice(sync('diverged'))?.level, 'warn');
  for (const quiet of ['current', 'offline', 'other-branch', 'no-remote'] as const) assert.equal(syncNotice(sync(quiet)), undefined, quiet);
});

test('a run is questioned only when the copy is stale for a reason the person can see', () => {
  assert.equal(staleBeforeRun(sync('dirty')), true);
  assert.equal(staleBeforeRun(sync('diverged')), true);
  assert.equal(staleBeforeRun(sync('offline')), false);
  assert.equal(staleBeforeRun(undefined), false);
});

const detail = (o: Partial<SpecDetail>): SpecDetail => ({ id: 'a', state: 'draft', label: 'L', actions: [], branch: 'main', files: [], root: '/r', ...o });

test('the menu is the kit\'s list for the stage: run actions, change actions, then the files', () => {
  const m = menu(detail({ state: 'approved', files: [{ path: 'specs/a/spec.md', group: 'Spec', editable: false }], actions: [
    { key: 'start-change', label: 'Değişiklik başlat', primary: true }, { key: 'edit-change', label: 'Elle düzenle' },
    { key: 'generate-tasks', label: 'Task üret' }, { key: 'merge-specs', label: 'Birleştir' }] }));
  assert.deepEqual(m.map(x => x.kind), ['startChange', 'editChange', 'run', 'files']); // tasks are not offered: they need a Jira key
});

test('a read-only file says why, and an approved spec suggests a change instead of a dead end', () => {
  const d = detail({ state: 'approved', actions: [{ key: 'start-change', label: 'x' }] });
  const b = blocked(d, { path: 'p', group: 'Spec', editable: false, why: 'Onaylı sürüm salt okunur. Değişiklik başlatın.' })!;
  assert.equal(b.suggestChange, true);
  assert.equal(b.needsBranch, false);
  const c = blocked(detail({ wantBranch: 'spec/a' }), { path: 'p', group: 'Spec', editable: false, why: 'Düzenlemek için önce spec dalına geçin.' })!;
  assert.equal(c.needsBranch, true);
  assert.equal(blocked(d, { path: 'p', group: 'Spec', editable: true }), undefined);
});

test('sending edits means update-manual, or carrying on the draft; the Tech Lead\'s comments are offered next to it', () => {
  assert.deepEqual(sendActions(detail({ state: 'awaiting', actions: [{ key: 'update-specs', label: 'a' }, { key: 'update-manual', label: 'b' }] })).map(a => a.key), ['update-manual', 'update-specs']);
  assert.deepEqual(sendActions(detail({ actions: [{ key: 'generate-specs', label: 'c' }] })).map(a => a.key), ['generate-specs']);
  assert.deepEqual(sendActions(detail({ actions: [] })), []);
});

test('files are listed by group, each saying whether it can be edited', () => {
  const l = fileItems(detail({ files: [{ path: 'prespec/a.md', group: 'Ön-spec', editable: true }, { path: 'specs/a/spec.md', group: 'Spec', editable: false }] }));
  assert.deepEqual(l.map(x => [x.label, x.separator ? 'group' : x.description]), [['Ön-spec', 'group'], ['a.md', 'düzenlenebilir'], ['Spec', 'group'], ['spec.md', 'salt okunur']]);
});
