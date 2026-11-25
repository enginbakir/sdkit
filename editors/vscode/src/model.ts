// The documents `sdkit ext health|specs` print (internal/ext/read.go, internal/ui). Pure helpers, so they are testable without VS Code.

export interface HealthItem { check?: string; level: 'problem' | 'todo' | 'note' | 'ok' | string; title: string; help?: string; fix?: string; raw?: string; soft?: boolean; main?: boolean }
export interface HealthDoc { ready: boolean; items: HealthItem[] }
export interface FlowStep { key: string; label: string; status: 'done' | 'current' | 'todo' }
export interface SpecItem { id: string; state: string; label: string; waiting: boolean; continue: boolean; update: boolean; merge: boolean; tasks: boolean; flow: FlowStep[] }

/** What the checklist shows first: the items the person must act on (the kit marks them `main`), problems before todos. */
export function mainItems(d: HealthDoc): HealthItem[] {
  const rank = (i: HealthItem) => (i.level === 'problem' ? 0 : i.level === 'todo' ? 1 : 2);
  return d.items.filter(i => i.main).sort((a, b) => rank(a) - rank(b));
}

/** The run action a spec's state offers, in the order a person would take them; none when only someone else can act. */
export function actionsOf(s: SpecItem): string[] {
  const a: string[] = [];
  if (s.continue) a.push('generate-specs');
  if (s.update) a.push('update-specs');
  if (s.merge) a.push('merge-specs');
  return a;
}

/** "step 3 of 6: Review", for the tooltip of a spec. */
export function whereIs(s: SpecItem): string {
  const i = s.flow.findIndex(f => f.status === 'current');
  return i < 0 ? s.label : `${i + 1}/${s.flow.length}: ${s.flow[i].label}`;
}

export interface Group<T> { label: string; items: T[]; collapsed?: boolean }

/**
 * The readiness checklist in the order a person acts on it. A `soft` problem is the Tech Lead's to fix and does not stop the work,
 * so it is shown apart and never as an error; everything the kit marks as not `main` is detail.
 */
export function groupHealth(d: HealthDoc): Group<HealthItem>[] {
  const main = mainItems(d);
  const mine = main.filter(i => (i.level === 'problem' || i.level === 'todo') && !i.soft);
  const techLead = main.filter(i => i.soft);
  const good = main.filter(i => !mine.includes(i) && !techLead.includes(i));
  const rest = d.items.filter(i => !i.main);
  const out: Group<HealthItem>[] = [];
  if (mine.length) out.push({ label: 'Sizi bekleyenler', items: mine });
  if (techLead.length) out.push({ label: 'Tech Lead\'e ait', items: techLead });
  if (good.length) out.push({ label: 'Hazır', items: good });
  if (rest.length) out.push({ label: 'Teknik ayrıntılar', items: rest, collapsed: true });
  return out;
}

/** The one line above the groups. */
export function healthSummary(d: HealthDoc): string {
  const n = groupHealth(d).find(g => g.label === 'Sizi bekleyenler')?.items.length ?? 0;
  return n ? `Başlamadan önce yapılacak ${n} şey var` : d.ready ? 'Çalışmaya hazır' : 'Hazır değil: ayrıntılara bakın';
}

/** Specs grouped by who has to act next; the kit already sorts open work first, and that order is kept inside a group. */
export function groupSpecs(specs: SpecItem[]): Group<SpecItem>[] {
  const g = [
    { label: 'Sizi bekleyenler', items: specs.filter(s => s.continue) },
    { label: 'Tech Lead\'de', items: specs.filter(s => s.waiting && !s.continue) },
    { label: 'Onaylı', items: specs.filter(s => !s.continue && !s.waiting) },
  ];
  return g.filter(x => x.items.length);
}

/** Every check the welcome screen shows, in its order. The names are `Item.Check` of the kit (internal/ui/health.go). */
export const CHECKS = ['setup', 'claude', 'claude-login', 'openspec', 'jira', 'bitbucket', 'project'] as const;
export type Check = (typeof CHECKS)[number];

/**
 * The checks that fail: an item with the check's name that is a problem or a todo. Without Claude Code there is nothing to log in
 * to, so a missing install fails the login too (the kit reports only the install then).
 */
export function failingChecks(d: HealthDoc): Set<string> {
  const f = new Set(d.items.filter(i => i.check && (i.level === 'problem' || i.level === 'todo')).map(i => i.check as string));
  if (f.has('claude')) f.add('claude-login');
  return f;
}

/** Something the person (not only the Tech Lead) has to do: worth showing the welcome screen for. */
export function needsAttention(d: HealthDoc): boolean {
  return d.items.some(i => (i.level === 'problem' || i.level === 'todo') && !i.soft);
}

export interface SpecAction { action: 'generate-specs' | 'update-specs' | 'merge-specs'; label: string }

/** What the person can start on a spec, worded for its state; nothing while only someone else can act (the Tech Lead's review). */
export function specActions(s: SpecItem): SpecAction[] {
  const a: SpecAction[] = [];
  if (s.continue) a.push({ action: 'generate-specs', label: s.state === 'new' ? 'Spec üretimini başlat' : 'Kaldığı yerden devam et' });
  if (s.update) a.push({ action: 'update-specs', label: s.state === 'approved' ? 'Değişiklik yap' : 'Tech Lead yorumlarını işle' });
  if (s.merge) a.push({ action: 'merge-specs', label: 'Değişiklikleri birleştir' });
  return a;
}

/** The pre-spec name becomes a file, a branch and a Jira link, so the kit allows letters, digits, - and _ only. Mirrors ui.ValidID. */
export const validSpecId = (id: string) => /^[A-Za-z0-9][A-Za-z0-9_-]{0,59}$/.test(id);

export const PRESPEC_TEMPLATE = `# Ön-spec

## Ne istiyoruz?
(Bir iki cümleyle isteği anlatın.)

## Neden?
(Kimin hangi sorunu çözülüyor?)

## Kapsam
- (Neler dahil?)

## Kapsam dışı
- (Neler bu işe dahil değil?)

## Notlar
(Bildiğiniz kurallar, ekran görüntüleri, benzer ekranlar.)
`;

/** The checks without which a run cannot work: the agent would not find the skills or the tools (it then offers to write by hand, and the kit's gates do not run). */
export const BLOCKING: readonly Check[] = ['setup', 'claude', 'claude-login', 'openspec'];

export function blockers(d: HealthDoc): { check: string; title: string }[] {
  const failing = failingChecks(d);
  return BLOCKING.filter(c => failing.has(c)).map(c => ({
    check: c,
    title: d.items.find(i => i.check === c && (i.level === 'problem' || i.level === 'todo'))?.title
      ?? { 'claude-login': "Claude Code'a giriş yapılmamış", claude: 'Claude Code kurulu değil' }[c as string] ?? c,
  }));
}
