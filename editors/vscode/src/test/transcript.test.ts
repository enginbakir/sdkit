import test from 'node:test';
import assert from 'node:assert/strict';
import { Page, quote, render } from '../transcript';

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
