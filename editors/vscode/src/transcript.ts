import { QuestionItem, RunEvent } from './protocol';
import { Step } from './steps';

/**
 * The run page in the middle of the window: a Markdown document that VS Code's own preview draws and redraws as the run goes. Pure,
 * so what the person reads is tested without VS Code. The preview is view-only; questions are answered in the QuickPick above it.
 */
export type Entry =
  | { kind: 'agent'; text: string }
  | { kind: 'question'; id: number; items: QuestionItem[]; answers?: Record<string, string>; dismissed?: boolean }
  | { kind: 'note'; text: string }
  | { kind: 'end'; failed: boolean; text?: string };

export interface Page {
  id: string;
  title: string; // what the run does, in plain words
  steps: Step[];
  entries: Entry[];
  running: boolean;
}

const ACTION_TITLES: Record<string, string> = {
  'generate-specs': 'Spec üretimi',
  'update-specs': 'Spec güncelleme',
  'merge-specs': 'Değişiklikleri birleştirme',
};
export const actionTitle = (a: string) => ACTION_TITLES[a] ?? a;

const stepIcon = (s: Step) => (s.state === 'done' ? '✅' : s.state === 'started' ? '🔄' : '⚪');

/** Text from outside (the agent, the question) must not break the page's own structure: no raw HTML, no heading of its own. */
export function quote(text: string): string {
  return text
    .replace(/</g, '&lt;')
    .split('\n')
    .map(l => '> ' + l)
    .join('\n');
}

export function render(p: Page): string {
  const out: string[] = [`# ${p.id} · ${p.title}`, ''];
  if (p.steps.length) {
    out.push(p.steps.map(s => `${stepIcon(s)} ${s.n}. ${s.name}`).join('  \n'), '');
  }
  out.push('---', '', '## Şu an', '', now(p), '', '---', '', '## Konuşma', '');
  if (!p.entries.length) out.push('_Ajan başlıyor…_', '');
  for (const e of p.entries) out.push(entry(e), '');
  return out.join('\n');
}

/** The box at the top, so the one thing that matters is seen without scrolling: a question waiting, the end, or the last message. */
function now(p: Page): string {
  const waiting = [...p.entries].reverse().find(e => e.kind === 'question' && !e.answers) as Extract<Entry, { kind: 'question' }> | undefined;
  if (waiting) {
    const hint = waiting.dismissed ? 'Seçim kutusu kapandı: alttaki durum çubuğunda **SDKit: cevabınız bekleniyor** yazısına tıklayın.' : 'Pencerenin üstündeki seçim kutusundan cevaplayın.';
    return `❓ **Cevabınız bekleniyor.** ${hint}`;
  }
  const end = p.entries.find(e => e.kind === 'end') as Extract<Entry, { kind: 'end' }> | undefined;
  if (end) return end.failed ? `❌ **Çalışma tamamlanamadı.** ${end.text ?? ''}`.trim() : '✅ **Çalışma bitti.**';
  const last = [...p.entries].reverse().find(e => e.kind === 'agent') as Extract<Entry, { kind: 'agent' }> | undefined;
  const cur = p.steps.find(s => s.state === 'started');
  const head = p.running ? `🔄 **Ajan çalışıyor${cur ? `: ${cur.name}` : ''}.**` : '⏸ **Durdu.**';
  return last ? `${head}\n\n${quote(firstLines(last.text, 4))}` : head;
}

function firstLines(t: string, n: number): string {
  const l = t.trim().split('\n');
  return l.length > n ? l.slice(0, n).join('\n') + '\n…' : t.trim();
}

function entry(e: Entry): string {
  switch (e.kind) {
    case 'agent':
      return `**🤖 Ajan**\n\n${quote(e.text.trim())}`;
    case 'note':
      return `_${e.text.replace(/[_<]/g, ' ')}_`;
    case 'end':
      return e.failed ? `**❌ Tamamlanamadı.** ${e.text ?? ''}`.trim() : '**✅ Bitti.**';
    case 'question': {
      const lines = [`**❓ ${e.items.length > 1 ? `${e.items.length} soru` : 'Soru'}**`, ''];
      for (const q of e.items) {
        lines.push(`- **${q.question.replace(/</g, '&lt;')}**`);
        if (q.finding) {
          const f = q.finding;
          lines.push(`  - Bulgu ${f.id ?? ''} · ${f.severity ?? ''} · ${f.where ?? ''}`.trimEnd(), `  - ${f.problem ?? ''}`);
        }
        const a = e.answers?.[q.question];
        lines.push(e.answers ? `  - 🙋 **Siz:** ${a ? a.replace(/</g, '&lt;') : '_(boş bıraktınız)_'}` : '  - _cevap bekleniyor_');
      }
      return lines.join('\n');
    }
  }
}

/** The address of a run's page; one page per spec, so starting the same spec again reuses its tab. */
export const pagePath = (id: string) => `/${id}.md`;
