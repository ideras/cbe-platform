import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spokenSpan, addedText } from '../src/spans.js';

test('a span is told to the student in seconds, minutes, or both, never rounded to a minute', () => {
  for (const [millis, expected] of [
    [1000, '1 second'], [30000, '30 seconds'], [59000, '59 seconds'],
    [60000, '1 minute'], [61000, '1 minute 1 second'], [90000, '1 minute 30 seconds'], [120000, '2 minutes'],
    [300000, '5 minutes'], [420000, '7 minutes'], [3600000, '60 minutes'], [3661000, '61 minutes 1 second'],
    // To the nearest second, which may be the next minute.
    [1499, '1 second'], [59600, '1 minute'],
  ]) assert.equal(spokenSpan(millis), expected, String(millis));
});

test('no span is told when there is none to tell', () => {
  for (const none of [0, -1000, null, undefined, NaN]) assert.equal(spokenSpan(none), '');
});

test('the time the instructor gave back is told in the same words, with the verb that agrees', () => {
  for (const [seconds, expected] of [
    [1, '1 second was added'], [45, '45 seconds were added'], [60, '1 minute was added'], [90, '1 minute 30 seconds were added'],
    [420, '7 minutes were added'], [61, '1 minute 1 second were added'],
  ]) assert.equal(addedText(seconds), expected, String(seconds));
});
