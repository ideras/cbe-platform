import { test } from 'node:test';
import assert from 'node:assert/strict';
import { effectiveRemaining, timeChip, TimeWarnings } from '../../../../shared/exam/timing.js';

test('report age and elapsed time are deducted, independent of the wall clock', () => {
  const s = { status: 'active', seconds_remaining: 60 };
  assert.equal(effectiveRemaining(s, 500, 1500), 58);
  assert.equal(effectiveRemaining(s, 1000, 90000), 0);
  assert.equal(effectiveRemaining({ status: 'grace', server_time: '2035-01-01T00:00:00Z', grace_until: '2035-01-01T00:00:30Z' }, 250, 750), 29);
  assert.equal(timeChip(s, 58).kind, 'warning');
  assert.match(timeChip(s, 58, true).text, /Timing stale/);
  assert.match(timeChip({ status: 'grace' }, 30).text, /Grace/);
  assert.equal(timeChip({ status: 'submitted' }, 0).text, 'Submitted');
});

test('warnings cross once, re-arm after extra time, and stay silent on stale reports', () => {
  const w = new TimeWarnings();
  const s = { status: 'active', deadline_at: '2035-01-01T01:00:00Z', grace_until: '2035-01-01T01:05:00Z' };
  assert.deepEqual(w.update(s, 601), []);
  assert.match(w.update(s, 600)[0].text, /10 minutes left before the grace period/);
  assert.deepEqual(w.update(s, 599), []);
  assert.deepEqual(w.update(s, 700), []);
  assert.equal(w.update(s, 600).length, 1);
  assert.deepEqual(w.update(s, 299, true), []);
  assert.equal(w.update(s, 299).length, 1);
  const end = { ...s, ends_at: s.deadline_at, grace_until: s.deadline_at };
  w.update(end, 61);
  assert.match(w.update(end, 60)[0].text, /submitted automatically/);
  const grace = { ...s, status: 'grace' };
  w.update(grace, 61);
  assert.match(w.update(grace, 60)[0].text, /1 minute of grace left/);
});
