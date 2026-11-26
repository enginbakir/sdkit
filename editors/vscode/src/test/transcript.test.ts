import test from 'node:test';
import assert from 'node:assert/strict';
import { activityLabel, duration, Page, pushActivity, quote, render } from '../transcript';

const page = (o: Partial<Page>): Page => ({ id: 'LOGIN', title: 'Spec üretimi', steps: [], entries: [], running: true, ...o });
const nowBox = (md: string) => md.split('## Şu an')[1].split('---')[0];

test('a waiting question is what the top box says, even after later messages', () => {
  const md = render(page({ entries: [
    { kind: 'agent', text: 'Açık noktalar var.' },
    { kind: 'question', id: 1, items: [{ question: 'Kim kontrol etsin?', options: [{ label: 'İstemci' }] }] },
  ] }));
  assert.match(nowBox(md), /Cevabınız bekleniyor/);
  assert.match(md, /cevap bekleniyor/);
});

test('an answered question shows the answer and no longer waits', () => {
  const md = render(page({ entries: [
    { kind: 'question', id: 1, items: [{ question: 'Q1' }, { question: 'Q2' }], answers: { Q1: 'İstemci' } },
    { kind: 'agent', text: 'Tamam.' },
  ] }));
  assert.doesNotMatch(nowBox(md), /bekleniyor/);
  assert.match(md, /Siz:\*\* İstemci/);
  assert.match(md, /boş bıraktınız/);
  assert.match(nowBox(md), /> Tamam\./);
});

test('a dismissed question tells the person where to find it again', () => {
  const md = render(page({ entries: [{ kind: 'question', id: 1, items: [{ question: 'Q' }], dismissed: true }] }));
  assert.match(nowBox(md), /durum çubuğunda/);
});

test('the end replaces the running state', () => {
  assert.match(nowBox(render(page({ running: false, entries: [{ kind: 'end', failed: true, text: 'push reddedildi' }] }))), /tamamlanamadı\.\*\* push reddedildi/);
  assert.match(nowBox(render(page({ running: false, entries: [{ kind: 'end', failed: false }] }))), /Çalışma bitti/);
});

test('agent text cannot inject HTML or a heading of its own', () => {
  assert.equal(quote('# başlık\n<script>x</script>'), '> # başlık\n> &lt;script>x&lt;/script>');
  const md = render(page({ entries: [{ kind: 'agent', text: '<img src=x onerror=alert(1)>' }] }));
  assert.doesNotMatch(md, /<img/);
});

test('steps are listed with their state, the current one named in the top box', () => {
  const md = render(page({ steps: [{ n: 1, name: 'Girdi', state: 'done' }, { n: 2, name: 'Taslak', state: 'started' }, { n: 3, name: 'Adım 3', state: 'pending' }] }));
  assert.match(md, /✅ 1\. Girdi {2}\n🔄 2\. Taslak {2}\n⚪ 3\. Adım 3/);
  assert.match(nowBox(md), /Ajan çalışıyor: Taslak/);
});

test('a quiet agent still shows what it is doing and for how long', () => {
  const md = render(page({ startedAt: 0, activity: [{ label: 'design.md okunuyor', at: 50_000 }, { label: 'Alt ajan çalışıyor: Review round 1', at: 70_000 }] }), 130_000);
  assert.match(nowBox(md), /Süre: 2 dk 10 sn/);
  assert.match(nowBox(md), /Şimdi: Alt ajan çalışıyor: Review round 1 · 1 dk 00 sn önce/);
  assert.match(nowBox(md), /- design\.md okunuyor/);
});

test('progress is shown only while the agent runs, and never a raw command', () => {
  assert.doesNotMatch(nowBox(render(page({ running: false, startedAt: 0, activity: [{ label: 'x', at: 0 }], entries: [{ kind: 'end', failed: false }] }), 5000)), /Süre/);
  assert.equal(activityLabel('Bash', 'rm -rf /tmp/secret'), 'Komut çalıştırılıyor');
  assert.equal(activityLabel('Write', '/a/b/specs/x/design.md'), 'design.md yazılıyor');
  assert.equal(activityLabel('Task', 'Review the draft'), 'Alt ajan çalışıyor: Review the draft');
});

test('durations read plainly', () => {
  assert.equal(duration(4_000), '4 sn');
  assert.equal(duration(125_000), '2 dk 05 sn');
});

test('the same move again only refreshes its time, and only the latest few are kept', () => {
  const p = page({});
  pushActivity(p, 'a okunuyor', 1); pushActivity(p, 'a okunuyor', 5);
  assert.deepEqual(p.activity, [{ label: 'a okunuyor', at: 5 }]);
  for (let i = 0; i < 10; i++) pushActivity(p, `x${i}`, i);
  assert.equal(p.activity!.length, 6);
  assert.equal(p.activity![5].label, 'x9');
});
