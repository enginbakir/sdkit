import test from 'node:test';
import assert from 'node:assert/strict';
import { collect } from '../questions';
import { parseLine } from '../protocol';
import { Stepper } from '../steps';

const asker = (picks: (string[] | undefined)[], texts: (string | undefined)[] = []) => ({
  pick: async () => picks.shift(),
  text: async () => texts.shift(),
});

test('answers are keyed by the question text; several labels are joined', async () => {
  const a = await collect(
    [{ question: 'Q1', options: [{ label: 'A' }] }, { question: 'Q2', multiSelect: true, options: [{ label: 'x' }, { label: 'y' }] }],
    asker([['A'], ['x', 'y']]),
  );
  assert.deepEqual(a, { Q1: 'A', Q2: 'x, y' });
});

test('a question without options is a typed answer; empty text is left out', async () => {
  assert.deepEqual(await collect([{ question: 'Q' }], asker([], ['  hello '])), { Q: 'hello' });
  assert.deepEqual(await collect([{ question: 'Q' }], asker([], [''])), {});
});

test('dismissing any question sends nothing', async () => {
  assert.equal(await collect([{ question: 'Q1', options: [{ label: 'A' }] }, { question: 'Q2', options: [{ label: 'B' }] }], asker([['A'], undefined])), undefined);
});

test('unknown or malformed lines are dropped, a new field is not an error', () => {
  assert.equal(parseLine('not json'), undefined);
  assert.equal(parseLine('{"type":"future"}'), undefined);
  assert.equal(parseLine('{"type":"question","id":1,"questions":{"questions":"x"}}'), undefined);
  assert.equal(parseLine('{"type":"end","failed":true,"extra":1}')?.type, 'end');
});

test('the stepper shows unseen steps as pending and survives a missing name', () => {
  const s = new Stepper();
  s.apply({ seq: 1, kind: 'step', skill: 'generate-specs', step: 2, total: 3, name: 'Draft', state: 'started' });
  s.apply({ seq: 2, kind: 'step', skill: 'generate-specs', step: 2, total: 3, state: 'done' });
  assert.deepEqual(s.list().map(x => [x.n, x.name, x.state]), [[1, 'Step 1', 'pending'], [2, 'Draft', 'done'], [3, 'Step 3', 'pending']]);
});
