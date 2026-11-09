// The two timelines of the Settings page, worked out from the form: what happens after a student leaves the exam window, and after a
// student's computer was last heard from. Positions are percentages of the line, which runs a little past its last event. A timeline
// whose values do not fit together yet is null: the page says so instead of drawing something the server would refuse.
import { spoken } from './roster.js';
import { effectiveCaptures } from './settings-rules.js';

const say = seconds => spoken(seconds * 1000);
const positive = value => typeof value === 'number' && Number.isFinite(value) && value > 0;
const lockValue = value => value === 0 || (positive(value) && Number.isInteger(value));
// More marks than this would overlap into a band, so the stretch is drawn as one dense strip instead.
export const MOST_MARKS = 48;

function scale(end) {
  const span = end * 1.04;
  return seconds => Math.min(100, Math.max(0, (seconds / span) * 100));
}

// A label near an end of the line is aligned to that end, so it stays inside the box.
const edge = at => (at < 6 ? 'start' : at > 94 ? 'end' : 'middle');
const label = (at, value, text, place, tone = '') => ({ at, value, text, place, tone, align: edge(at) });

export function awayTimeline(form) {
  const { focusSeconds: start, captureSeconds: every, lockAwaySeconds: lock, alertSeconds: repeat } = form;
  if (!positive(start) || !positive(every) || !positive(repeat) || !lockValue(lock) || (lock > 0 && lock < start + every)) return null;
  const shots = lock > 0 ? effectiveCaptures(form) : form.incidentCaptures;
  if (!positive(shots)) return null;
  const end = lock > 0 ? lock : start + shots * every;
  const at = scale(end);
  const dense = shots > MOST_MARKS;
  const marks = dense ? [] : Array.from({ length: shots }, (_, i) => start + i * every)
    .filter(time => !(lock > 0 && time > lock)).map(time => ({ kind: 'shot', at: at(time), title: `Screenshot at ${say(time)}` }));
  if (lock > 0) marks.push({ kind: 'lock', at: at(lock), title: `Locked at ${say(lock)}` });
  return {
    title: 'Student leaves at 0 s', end: lock > 0 ? `locks at ${say(lock)}` : 'away lock off',
    segments: [{ kind: 'calm', from: 0, to: at(start) }, { kind: 'alarm', from: at(start), to: at(end), dense }],
    marks,
    labels: [label(0, '0 s', 'leaves', 'down'), label(at(start), say(start), 'alarm and screenshots', 'up', 'warn'),
      ...(lock > 0 ? [label(at(lock), say(lock), 'locked', 'down', 'alert')] : [])],
    legend: [{ kind: 'shot', text: `a screenshot every ${say(every)} (${shots} per absence)` }, { kind: 'alarm', text: `the alarm every ${say(repeat)}` }],
    summary: `After a student leaves the exam window, the alarm and the screenshots start at ${say(start)}, a screenshot every ${say(every)}, `
      + (lock > 0 ? `and the exam locks at ${say(lock)}.` : 'and the exam never locks for it.'),
  };
}

export function connectionTimeline(form) {
  const { heartbeatSeconds: beat, overdueSeconds: lost, lockNoContactSeconds: lock } = form;
  if (!positive(beat) || !positive(lost) || !lockValue(lock) || lost < beat * 2 || (lock > 0 && lock <= lost)) return null;
  const end = lock > 0 ? lock : lost + beat * 2;
  const at = scale(end);
  const beats = [];
  for (let time = beat; time < end; time += beat) beats.push(time);
  const dense = beats.length > MOST_MARKS;
  const marks = dense ? [] : beats.map(time => ({ kind: 'beat', at: at(time), title: `A heartbeat was due at ${say(time)}` }));
  marks.push({ kind: 'lost', at: at(lost), title: `The connection counts as lost at ${say(lost)}` });
  if (lock > 0) marks.push({ kind: 'lock', at: at(lock), title: `Locked at ${say(lock)}` });
  return {
    title: 'Last heard from at 0 s', end: lock > 0 ? `locks at ${say(lock)}` : 'no contact lock off',
    segments: [{ kind: 'calm', from: 0, to: at(lost) }, { kind: 'lost', from: at(lost), to: at(end) }],
    marks,
    labels: [label(0, '0 s', 'last heard', 'down'), label(at(lost), say(lost), 'connection lost', 'up', 'alert'),
      ...(lock > 0 ? [label(at(lock), say(lock), 'locked', 'down', 'alert')] : [])],
    legend: [{ kind: 'beat', text: `a heartbeat due every ${say(beat)}` }],
    summary: `After a computer was last heard from, its connection counts as lost at ${say(lost)}, `
      + (lock > 0 ? `and the exam locks at ${say(lock)}.` : 'and the exam never locks for it.'),
  };
}
