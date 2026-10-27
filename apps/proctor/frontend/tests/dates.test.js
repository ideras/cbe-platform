import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseTime, localTime, localDateTime } from '../src/dates.js';

test('server times with nanoseconds are read to the millisecond and shown in local time', () => {
  const date = parseTime('2026-10-19T03:28:25.668515282Z');
  assert.equal(date.toISOString(), '2026-10-19T03:28:25.668Z');
  assert.equal(localTime('2026-10-19T03:28:25.668515282Z'), date.toLocaleTimeString());
  assert.equal(localDateTime('2026-10-19T03:28:25Z'), new Date('2026-10-19T03:28:25Z').toLocaleString());
});

test('missing, invalid and Go zero times are not shown as dates', () => {
  for (const value of [null, undefined, '', 'yesterday', '0001-01-01T00:00:00Z']) {
    assert.equal(parseTime(value), null);
    assert.equal(localTime(value), 'not reported');
    assert.equal(localDateTime(value, 'unavailable'), 'unavailable');
  }
});
