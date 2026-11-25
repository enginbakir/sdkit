import { RunEvent } from './protocol';

export interface Step { n: number; name: string; state: 'pending' | 'started' | 'done' }

/** The stepper of a run, rebuilt from the step banners; steps never seen stay pending. */
export class Stepper {
  skill = '';
  total = 0;
  private steps = new Map<number, Step>();

  apply(e: RunEvent): boolean {
    if (e.kind !== 'step' || !e.step || !e.total) return false;
    this.skill = e.skill ?? this.skill;
    this.total = e.total;
    const cur = this.steps.get(e.step);
    this.steps.set(e.step, { n: e.step, name: e.name || cur?.name || `Step ${e.step}`, state: e.state === 'done' ? 'done' : 'started' });
    return true;
  }

  list(): Step[] {
    const out: Step[] = [];
    for (let n = 1; n <= this.total; n++) out.push(this.steps.get(n) ?? { n, name: `Step ${n}`, state: 'pending' });
    return out;
  }
}
