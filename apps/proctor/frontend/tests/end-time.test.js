import { test } from 'node:test';
import assert from 'node:assert/strict';
import { clockValue, suggestedEndTime, addEndMinutes, endTimeLabel, endTimeETA } from '../src/end-time.js';

test('suggest an existing future end or a roughly two-hour time, rounded to five minutes', () => {
  const now = new Date(2035, 0, 1, 10, 22, 35);
  assert.equal(clockValue(now), '10:22');
  assert.equal(suggestedEndTime(null, now), '12:25');
  const existing = new Date(2035, 0, 1, 13, 15);
  assert.equal(suggestedEndTime(existing.toISOString(), now), '13:15');
  assert.equal(suggestedEndTime(new Date(2035, 0, 1, 9, 0).toISOString(), now), '12:25');
});

test('presets add to the selected time and wrap safely over midnight', () => {
  assert.equal(addEndMinutes('12:30', 15), '12:45');
  assert.equal(addEndMinutes('12:30', 30), '13:00');
  assert.equal(addEndMinutes('23:45', 60), '00:45');
  assert.equal(addEndMinutes('23:55', 15), '00:10');
  assert.equal(addEndMinutes('', 15, new Date(2035, 0, 1, 23, 55)), '00:10');
});

test('comparison labels and ETA use full dates, including tomorrow', () => {
  const now = new Date(2035, 0, 1, 23, 45).getTime();
  assert.equal(endTimeETA(new Date(2035, 0, 2, 1, 50).toISOString(), now), 'in 2 h 05 min');
  assert.equal(endTimeETA(new Date(2035, 0, 1, 23, 50).toISOString(), now), 'in 5 min');
  assert.equal(endTimeETA(new Date(now).toISOString(), now), 'time reached');
  assert.equal(endTimeETA(null, now), '');
  assert.equal(endTimeLabel(null), 'No end time');
  assert.equal(endTimeLabel('invalid', 'Choose a time'), 'Choose a time');
  assert.equal(endTimeLabel(new Date(now).toISOString()), new Date(now).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }));
});
