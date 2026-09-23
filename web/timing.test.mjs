import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import vm from 'node:vm';

const page = readFileSync(new URL('./index.html', import.meta.url), 'utf8');
const definitions = page.slice(page.indexOf('const terminal='), page.indexOf('\nlet job='));
const timing = page.slice(page.indexOf('function elapsed()'), page.indexOf('\nfunction hasDuration'));

function evaluate(job, start) {
  assert.ok(definitions.startsWith('const terminal='));
  assert.ok(timing.startsWith('function elapsed()'));
  return vm.runInNewContext(`${definitions}\n${timing}\n({elapsed:elapsed(),label:wallLabel()})`, {
    Date,
    Number,
    job,
    start,
  });
}

test('completed review uses server start and finish even with stale browser timestamps', () => {
  const started = Date.parse('2026-09-23T00:00:00Z');
  const result = evaluate({
    status: 'completed',
    started_at: new Date(started).toISOString(),
    finished_at: new Date(started + 120_000).toISOString(),
    updated_at: new Date(started + 2_300).toISOString(),
  }, started + 200_000);

  assert.equal(result.elapsed, 120_000);
  assert.equal(result.label, '2m 0s');
});

test('terminal review without finish time remains settling', () => {
  const result = evaluate({
    status: 'completed',
    started_at: '2026-09-23T00:00:00Z',
    updated_at: '2026-09-23T00:00:02.300Z',
  }, 0);

  assert.equal(result.elapsed, null);
  assert.equal(result.label, '结算中');
});
