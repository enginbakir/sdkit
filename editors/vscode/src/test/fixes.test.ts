import test from 'node:test';
import assert from 'node:assert/strict';
import { command, Env, planFix } from '../fixes';
import { describeResult, fields } from '../connect';

const env = (o: Partial<Env> = {}): Env => ({ hasNpm: true, agent: 'claude', binary: 'sdkit', ...o });

test('without npm the install fixes open a page instead of running a command', () => {
  assert.equal(planFix('install-openspec', env({ hasNpm: false }))?.kind, 'link');
  assert.deepEqual(planFix('install-openspec', env()), { kind: 'terminal', name: 'OpenSpec kurulumu', line: 'npm install -g @fission-ai/openspec' });
  assert.equal(planFix('something-new', env()), undefined);
});

test('the agent fixes follow the chosen agent', () => {
  assert.equal((planFix('install-agent', env({ agent: 'codex' })) as { line: string }).line, 'npm install -g @openai/codex');
  assert.equal((planFix('login-agent', env({ agent: 'claude' })) as { line: string }).line, 'claude auth login');
  assert.equal((planFix('login-agent', env({ agent: 'codex' })) as { line: string }).line, 'codex login');
});

test('the setup runs init in copy mode for the chosen agent, quoting a path with a space', () => {
  assert.equal((planFix('init-project', env({ agent: 'codex' })) as { line: string }).line, 'sdkit init --env analyst --host codex --mode copy --install-openspec');
  assert.equal(command('/Users/a b/sdkit', 'init'), '"/Users/a b/sdkit" init');
  assert.equal(command('C:\\Program Files\\sdkit.exe', 'init', true), '& "C:\\Program Files\\sdkit.exe" init');
});

test('a saved token is optional in the form, a missing one is required', () => {
  const c = { jiraBaseUrl: '', jiraProject: '', jiraUser: 'a@b.co', jiraTokenSet: true, bitbucketEmail: '', bitbucketTokenSet: false };
  assert.equal(fields(c).find(f => f.key === 'jiraToken')?.optional, true);
  assert.equal(fields({ ...c, jiraTokenSet: false }).find(f => f.key === 'jiraToken')?.optional, false);
  assert.equal(fields(c).find(f => f.key === 'bitbucketEmail')?.value, 'a@b.co'); // falls back to the Jira address
});

test('the saved notice is ok only when every service checked is ok', () => {
  assert.equal(describeResult({ saved: true, jira: { ok: true, who: 'Ayşe' } }).ok, true);
  assert.equal(describeResult({ saved: true, jira: { ok: true }, bitbucket: { ok: false, message: 'reddedildi' } }).ok, false);
  assert.match(describeResult({ saved: true, jira: { ok: false, message: 'ulaşılamadı' } }).text, /Jira: ulaşılamadı/);
});
