import test from 'node:test';
import assert from 'node:assert/strict';
import { planFix } from '../fixes';
import { describeResult, fields } from '../connect';

test('without npm the install fixes open a page instead of running a command', () => {
  assert.equal(planFix('install-openspec', { hasNpm: false })?.kind, 'link');
  assert.deepEqual(planFix('install-openspec', { hasNpm: true }), { kind: 'terminal', name: 'OpenSpec kurulumu', line: 'npm install -g @fission-ai/openspec' });
  assert.equal(planFix('login-claude', { hasNpm: false })?.kind, 'terminal');
  assert.equal(planFix('something-new', { hasNpm: true }), undefined);
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
