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

import { specActions, validSpecId } from '../model';

test('a spec offers actions worded for its state', () => {
  assert.deepEqual(specActions(spec({ continue: true, state: 'new' })).map(a => a.label), ['Spec üretimini başlat']);
  assert.deepEqual(specActions(spec({ continue: true, state: 'draft' })).map(a => a.label), ['Kaldığı yerden devam et']);
  assert.deepEqual(specActions(spec({ update: true, merge: true, state: 'approved' })).map(a => a.action), ['update-specs', 'merge-specs']);
  assert.equal(specActions(spec({ update: true, state: 'approved' }))[0].label, 'Değişiklik yap');
  assert.deepEqual(specActions(spec({ waiting: true, state: 'awaiting' })), []);
});

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
