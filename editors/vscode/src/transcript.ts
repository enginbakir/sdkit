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

/** One thing the agent was seen doing: what a person can follow, never a raw command. */
export interface Activity { label: string; at: number }

export interface Page {
  id: string;
  title: string; // what the run does, in plain words
  steps: Step[];
  entries: Entry[];
  running: boolean;
  startedAt?: number;
  /** newest last; the agent can work for minutes between two messages, and this is what shows it is alive */
  activity?: Activity[];
}

/** A tool call in words for a person: what it is doing, and to what when that is a file or a subagent's task. Commands are not shown. */
export function activityLabel(name: string, text = ''): string {
  const file = text.split(/[\\/]/).pop() ?? '';
  switch (name) {
    case 'Read': return file ? `${file} okunuyor` : 'Dosya okunuyor';
    case 'Write': case 'Edit': case 'MultiEdit': return file ? `${file} yazılıyor` : 'Dosya yazılıyor';
    case 'Glob': case 'Grep': return 'Projede arama yapılıyor';
    case 'Task': return text ? `Alt ajan çalışıyor: ${text}` : 'Alt ajan çalışıyor';
    case 'Skill': return text ? `Yetenek yükleniyor: ${text}` : 'Yetenek yükleniyor';
    case 'Bash': return 'Komut çalıştırılıyor';
    default: return 'Çalışıyor';
  }
}

/** "4 dk 12 sn": how long, without a clock to compare to. */
export function duration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  return s < 60 ? `${s} sn` : `${Math.floor(s / 60)} dk ${String(s % 60).padStart(2, '0')} sn`;
}

export const MAX_ACTIVITY = 6;

/** Record a move: the same thing again only refreshes its time (forty reads in a row are one line), and the list stays short. */
export function pushActivity(p: Page, label: string, at: number): void {
  const a = (p.activity ??= []);
  const last = a[a.length - 1];
  if (last?.label === label) last.at = at;
  else a.push({ label, at });
  if (a.length > MAX_ACTIVITY) a.splice(0, a.length - MAX_ACTIVITY);
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

export function render(p: Page, now = Date.now()): string {
  const out: string[] = [`# ${p.id} · ${p.title}`, ''];
  if (p.steps.length) {
    out.push(p.steps.map(s => `${stepIcon(s)} ${s.n}. ${s.name}`).join('  \n'), '');
  }
  out.push('---', '', '## Şu an', '', status(p, now), '', '---', '', '## Konuşma', '');
  if (!p.entries.length) out.push('_Ajan başlıyor…_', '');
  for (const e of p.entries) out.push(entry(e), '');
  return out.join('\n');
}

/** The box at the top, so the one thing that matters is seen without scrolling: a question waiting, the end, or the last message. */
function status(p: Page, now: number): string {
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
  const parts = [head];
  if (p.running) parts.push(progress(p, now));
  if (last) parts.push(quote(firstLines(last.text, 4)));
  return parts.filter(Boolean).join('\n\n');
}

/** Time and the latest moves: a quiet agent is still seen working, and how long it has been quiet is plain. */
function progress(p: Page, now: number): string {
  const head: string[] = [];
  if (p.startedAt !== undefined) head.push(`⏱ Süre: ${duration(now - p.startedAt)}`);
  const a = p.activity ?? [];
  if (!a.length) head.push('⚙️ Ajan hazırlanıyor…');
  else head.push(`⚙️ Şimdi: ${a[a.length - 1].label} · ${duration(now - a[a.length - 1].at)} önce`);
  const earlier = a.slice(0, -1).reverse().map(x => `- ${x.label}`);
  return [head.join('  \n'), ...(earlier.length ? ['_Son hareketler:_\n' + earlier.join('\n')] : [])].join('\n\n');
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
