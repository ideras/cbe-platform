import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULTS, RANGES, SECTIONS, LABELS, descriptions, sectionOf } from '../src/settings-rules.js';
import { awayTimeline, connectionTimeline, MOST_MARKS } from '../src/settings-timeline.js';

const form = (change = {}) => ({ ...DEFAULTS, ...change });

test('every field is in one section, with a range that holds its default', () => {
  const placed = SECTIONS.flatMap(section => section.fields);
  assert.deepEqual([...placed].sort(), Object.keys(LABELS).sort());
  for (const field of placed) {
    assert.equal(sectionOf(field), SECTIONS.find(section => section.fields.includes(field)).id);
    const [low, high] = RANGES[field];
    assert.ok(low <= DEFAULTS[field] && DEFAULTS[field] <= high, `${field}: ${DEFAULTS[field]} outside ${low}–${high}`);
  }
});

test('every field has a description, and a lock that is off says what off means', () => {
  for (const [field, text] of Object.entries(descriptions(DEFAULTS))) assert.ok(text, field);
  const off = descriptions(form({ lockAwaySeconds: 0, lockNoContactSeconds: 0 }));
  assert.match(off.lockAwaySeconds, /^Off: /);
  assert.match(off.lockNoContactSeconds, /^Off: /);
  assert.equal(off.incidentCaptures, 'With the away lock off, this caps the screenshots of one absence.');
});

test('the absence timeline puts the alarm, the screenshots and the lock where the values say', () => {
  const line = awayTimeline(form({ lockAwaySeconds: 30 }));
  // Screenshots at 5, 10, … 30 s: six of them, the last one at the lock.
  assert.equal(line.marks.filter(mark => mark.kind === 'shot').length, 6);
  assert.equal(line.marks.at(-1).kind, 'lock');
  assert.ok(Math.abs(line.marks.at(-1).at - 100 / 1.04) < 1e-9);
  assert.equal(line.end, 'locks at 30 s');
  assert.equal(line.summary, 'After a student leaves the exam window, the alarm and the screenshots start at 5 s, a screenshot every 5 s, and the exam locks at 30 s.');
  // Off, the line runs over the screenshots of one absence and nothing locks.
  const off = awayTimeline(form({ lockAwaySeconds: 0, incidentCaptures: 4 }));
  assert.equal(off.end, 'away lock off');
  assert.equal(off.marks.filter(mark => mark.kind === 'lock').length, 0);
  assert.equal(off.marks.length, 4);
  // Too many screenshots to draw one by one make a striped stretch instead.
  const dense = awayTimeline(DEFAULTS);
  assert.ok(60 > MOST_MARKS && dense.segments[1].dense && dense.marks.length === 1);
  // Values that do not fit together are not drawn.
  assert.equal(awayTimeline(form({ lockAwaySeconds: 8 })), null);
  assert.equal(awayTimeline(form({ focusSeconds: null })), null);
});

test('the connection timeline marks the heartbeats, the lost connection and the lock', () => {
  const line = connectionTimeline(form({ lockNoContactSeconds: 60 }));
  assert.deepEqual(line.marks.map(mark => mark.kind), ['beat', 'beat', 'beat', 'lost', 'lock']);
  assert.equal(line.summary, 'After a computer was last heard from, its connection counts as lost at 30 s, and the exam locks at 1 min.');
  assert.equal(connectionTimeline(form({ lockNoContactSeconds: 0 })).end, 'no contact lock off');
  assert.equal(connectionTimeline(form({ lockNoContactSeconds: 30 })), null);
  assert.equal(connectionTimeline(form({ overdueSeconds: 20 })), null);
});
