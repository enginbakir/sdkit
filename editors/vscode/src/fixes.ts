/** What a Readiness row's `fix` key starts. Pure, so the choice per platform and agent is testable without VS Code. */
export type FixPlan =
  | { kind: 'connect' }
  | { kind: 'terminal'; name: string; line: string }
  | { kind: 'link'; url: string; why: string }
  | { kind: 'settings'; query: string; why: string };

export type Agent = 'claude' | 'codex';
export interface Env { hasNpm: boolean; agent: Agent; binary: string; win32?: boolean }

const AGENTS: Record<Agent, { name: string; pkg: string; login: string; docs: string }> = {
  claude: { name: 'Claude Code', pkg: '@anthropic-ai/claude-code', login: 'claude auth login', docs: 'https://docs.claude.com/en/docs/claude-code/setup' },
  codex: { name: 'Codex', pkg: '@openai/codex', login: 'codex login', docs: 'https://developers.openai.com/codex/cli' },
};
export const agentName = (a: Agent) => AGENTS[a].name;

/** The binary as a command in a terminal: a path with a space is quoted, and PowerShell needs the call operator for a quoted path. */
export function command(binary: string, args: string, win32 = false): string {
  const q = /[\s"]/.test(binary) ? `"${binary.replace(/"/g, '\\"')}"` : binary;
  return `${win32 && q !== binary ? '& ' : ''}${q} ${args}`;
}

export function planFix(fix: string, env: Env): FixPlan | undefined {
  const a = AGENTS[env.agent];
  switch (fix) {
    case 'connect':
      return { kind: 'connect' };
    case 'binary-setting':
      return { kind: 'settings', query: 'sdkit.binaryPath', why: 'Eklentiyle gelen sdkit'in yolunu yazın (örneğin /Users/<siz>/sdkit-dev/sdkit).' };
    case 'init-project':
      // copy mode puts the kit's skills in the repo, git-ignored: a run does not depend on a Claude Code plugin being fetched
      return { kind: 'terminal', name: 'SDKit kurulumu', line: command(env.binary, `init --env analyst --host ${env.agent} --mode copy --install-openspec`, env.win32) };
    case 'install-openspec':
      // the kit does not vendor OpenSpec: it comes from npm, so without Node there is nothing to run
      return env.hasNpm
        ? { kind: 'terminal', name: 'OpenSpec kurulumu', line: 'npm install -g @fission-ai/openspec' }
        : { kind: 'link', url: 'https://nodejs.org/en/download', why: 'OpenSpec için önce Node.js kurulmalı. Kurduktan sonra yenileyin.' };
    case 'install-agent':
      return env.hasNpm
        ? { kind: 'terminal', name: `${a.name} kurulumu`, line: `npm install -g ${a.pkg}` }
        : { kind: 'link', url: a.docs, why: `${a.name} kurulum sayfası açılıyor.` };
    case 'login-agent':
      return { kind: 'terminal', name: `${a.name} girişi`, line: a.login };
  }
  return undefined;
}
