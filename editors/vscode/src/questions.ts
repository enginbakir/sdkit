import { QuestionItem } from './protocol';

/** What the editor can do for a question; the VS Code implementation is in extension.ts, a stand-in in the tests. */
export interface Asker {
  /** pick one label (or several), or type an answer; undefined when the person dismissed it */
  pick(item: QuestionItem): Promise<string[] | undefined>;
  text(item: QuestionItem): Promise<string | undefined>;
}

/** Plan gate cards are not drawn in the first slice: the confirm/decline options are asked as a plain choice. */
export const hasOptions = (q: QuestionItem) => (q.options?.length ?? 0) > 0;

/**
 * Ask every question of one `question` line and build the answers object the skills expect:
 * `{ "<question text>": "label, label2" }`, free text allowed. Undefined means the person walked away; the run then waits
 * (the question stays open in the run view) instead of sending a guess.
 */
export async function collect(items: QuestionItem[], ask: Asker): Promise<Record<string, string> | undefined> {
  const answers: Record<string, string> = {};
  for (const item of items) {
    if (hasOptions(item)) {
      const picked = await ask.pick(item);
      if (!picked) return undefined;
      if (picked.length) answers[item.question] = picked.join(', ');
    } else {
      const t = await ask.text(item);
      if (t === undefined) return undefined;
      if (t.trim()) answers[item.question] = t.trim();
    }
  }
  return answers;
}

/** The label QuickPick shows for an option: the description is the second line. */
export const optionDetail = (o: { description?: string }) => o.description ?? '';
