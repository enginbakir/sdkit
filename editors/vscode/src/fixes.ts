/** What a Readiness row's `fix` key starts. Pure, so the choice per platform is testable without VS Code. */
export type FixPlan =
  | { kind: 'connect' }
  | { kind: 'terminal'; name: string; line: string }
  | { kind: 'link'; url: string; why: string };

export interface Env { hasNpm: boolean }

export function planFix(fix: string, env: Env): FixPlan | undefined {
  switch (fix) {
    case 'connect':
      return { kind: 'connect' };
    case 'install-openspec':
      // the kit does not vendor OpenSpec: it comes from npm, so without Node there is nothing to run
      return env.hasNpm
        ? { kind: 'terminal', name: 'OpenSpec kurulumu', line: 'npm install -g @fission-ai/openspec' }
        : { kind: 'link', url: 'https://nodejs.org/en/download', why: 'OpenSpec için önce Node.js kurulmalı. Kurduktan sonra yenileyin.' };
    case 'install-claude':
      return env.hasNpm
        ? { kind: 'terminal', name: 'Claude Code kurulumu', line: 'npm install -g @anthropic-ai/claude-code' }
        : { kind: 'link', url: 'https://docs.claude.com/en/docs/claude-code/setup', why: 'Claude Code kurulum sayfası açılıyor.' };
    case 'login-claude':
      return { kind: 'terminal', name: 'Claude Code girişi', line: 'claude auth login' };
  }
  return undefined;
}
