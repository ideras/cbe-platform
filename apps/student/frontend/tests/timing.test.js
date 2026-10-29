import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatTime, remainingTime } from '../../../../shared/exam/timing.js';

test('duration formatting clamps expired times, preserves hours and handles missing data', () => {
  for (const [seconds, expected] of [[0, '00:00:00'], [59, '00:00:59'], [60, '00:01:00'], [3661, '01:01:01'], [90061, '25:01:01'], [-2, '00:00:00'], [null, 'unknown'], [NaN, 'unknown']]) {
    assert.equal(formatTime(seconds), expected);
  }
});

test('grace uses the server clock, rounds up partial seconds and clamps at expiry', () => {
  const status = { status: 'grace', seconds_remaining: 0, grace_until: '2035-01-01T00:00:30Z' };
  assert.equal(remainingTime(status, '2035-01-01T00:00:00Z'), '00:00:30');
  assert.equal(remainingTime(status, '2035-01-01T00:00:10.500Z'), '00:00:20');
  assert.equal(remainingTime(status, '2035-01-01T00:00:31Z'), '00:00:00');
  assert.equal(remainingTime(status, '2035-01-01T00:00:10.123456789Z'), '00:00:20');
  assert.equal(remainingTime(status), 'unknown');
  assert.equal(remainingTime(status, 'invalid'), 'unknown');
  assert.equal(remainingTime({ ...status, status: 'submitted' }), '00:00:00');
  assert.equal(remainingTime({ status: 'active', seconds_remaining: 3661 }), '01:01:01');
});
