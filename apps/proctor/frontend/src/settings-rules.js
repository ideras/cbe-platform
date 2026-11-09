// The rules of the Settings page, apart from the page so they can be tested: what is wrong with the values, the limits the other values
// put on a field, what fixes each problem, and how to fit the other values to the one the proctor changed last. Everything is in the
// form's units: seconds, and counts of screenshots. The server checks the same rules (domain.Policy.Problems) and has the last word.
import { spoken } from './roster.js';

// What the server starts with (domain.DefaultPolicy), which "Restore the defaults" puts back.
export const DEFAULTS = Object.freeze({
  heartbeatSeconds: 15, overdueSeconds: 30, focusSeconds: 5, alertSeconds: 2, captureSeconds: 5,
  lockAwaySeconds: 300, lockNoContactSeconds: 300, incidentCaptures: 30, sessionCaptures: 200, graceSeconds: 30,
});

export const LABELS = Object.freeze({
  heartbeatSeconds: 'Heartbeat every', overdueSeconds: 'Connection lost after', focusSeconds: 'Alarm and screenshots start after',
  alertSeconds: 'Alarm repeats every', captureSeconds: 'Screenshot every', lockAwaySeconds: 'Lock after away for',
  lockNoContactSeconds: 'Lock after no contact for', incidentCaptures: 'Screenshots per absence', sessionCaptures: 'Screenshots per student',
  graceSeconds: 'Default grace period',
});

// Each field's own range, the server's (domain.Policy.Problems), and the step of its − and + buttons. The other values narrow some of
// them further (limits, below); the buttons keep a field only within its own range, so a conflict they make is shown and fixed like a
// typed one. A lock's range is for when it is on: off is zero.
export const RANGES = Object.freeze({
  heartbeatSeconds: [5, 60], overdueSeconds: [1, 600], focusSeconds: [1, 600], alertSeconds: [1, 10], captureSeconds: [3, 60],
  lockAwaySeconds: [1, 3600], lockNoContactSeconds: [1, 3600], incidentCaptures: [1, 500], sessionCaptures: [1, 2000], graceSeconds: [1, 1800],
});
export const STEPS = Object.freeze({ sessionCaptures: 10, graceSeconds: 5 });

// The page's sections, in order, and the fields each one holds.
export const SECTIONS = Object.freeze([
  { id: 'away', title: 'Leaving the exam window', short: 'Leaving the window', icon: 'monitor', about: 'What happens when a student switches to another window',
    fields: ['focusSeconds', 'lockAwaySeconds', 'alertSeconds', 'captureSeconds'] },
  { id: 'connection', title: 'Connection', short: 'Connection', icon: 'wifi', about: 'How the server notices that a student’s computer has gone silent',
    fields: ['heartbeatSeconds', 'overdueSeconds', 'lockNoContactSeconds'] },
  { id: 'screenshots', title: 'Screenshot limits', short: 'Screenshot limits', icon: 'screen', about: 'Caps that keep the storage under control',
    fields: ['incidentCaptures', 'sessionCaptures'] },
  { id: 'time', title: 'Exam time', short: 'Exam time', icon: 'clock', about: 'The default for new exams',
    fields: ['graceSeconds'] },
]);

const COUNTS = ['incidentCaptures', 'sessionCaptures'];
const LOCKS = ['lockAwaySeconds', 'lockNoContactSeconds'];

// A value in the words of the page: "30 s", "5 min", "off" for a lock at zero, a bare number for a count.
export function say(field, value) {
  if (COUNTS.includes(field)) return String(value);
  if (LOCKS.includes(field) && value === 0) return 'off';
  return spoken(value * 1000);
}
const seconds = value => spoken(value * 1000);

// An empty field is a value nobody chose: it is never sent as a number.
const empty = value => value === null || value === undefined || Number.isNaN(value);
const finite = value => typeof value === 'number' && Number.isFinite(value);
const clamp = (value, low, high) => Math.min(high, Math.max(low, value));
// A lock that is on, in range: the other values it has to come after are checked only for these, so one wrong value is one problem.
const lockIn = value => !empty(value) && Number.isInteger(value) && value >= 1 && value <= 3600;

// The screenshots of one absence: with the away lock on they cover the whole absence up to the lock.
export function effectiveCaptures(form) {
  if (!form.lockAwaySeconds) return form.incidentCaptures;
  const span = form.lockAwaySeconds - form.focusSeconds;
  return span < 0 ? 1 : Math.floor(span / form.captureSeconds) + 1;
}

const fix = (label, set) => ({ label, set });

// Every problem of the values, each naming the fields it is about and carrying the buttons that would fix it. A `field` problem is a
// value that is wrong on its own; a `conflict` is values that cannot go together, and names in `involves` all the fields that decide it.
export function findProblems(form) {
  const found = [];
  const add = (kind, fields, message, fixes = [], involves = fields) => found.push({ kind, fields, involves, message, fixes });
  const { heartbeatSeconds: beat, overdueSeconds: lost, focusSeconds: start, alertSeconds: repeat, captureSeconds: capture,
    lockAwaySeconds: away, lockNoContactSeconds: noContact, incidentCaptures: perAbsence, sessionCaptures: perStudent, graceSeconds: grace } = form;
  const show = value => empty(value) ? 'empty' : value;

  // A value outside its range goes to the nearest limit.
  const toRange = (field, value, low, high, whole = false) => {
    const target = clamp(whole ? Math.round(value) : value, low, high);
    return [fix(`Use ${say(field, target)}`, { [field]: target })];
  };
  for (const field of Object.keys(LABELS)) {
    if (empty(form[field])) add('field', [field], `${LABELS[field]} is empty: type the value you want.`, [fix(`Use ${say(field, DEFAULTS[field])}`, { [field]: DEFAULTS[field] })]);
  }
  if (!empty(beat) && (beat < 5 || beat > 60)) add('field', ['heartbeatSeconds'], `Heartbeat every must be between 5 and 60 seconds (it is ${beat}).`, toRange('heartbeatSeconds', beat, 5, 60));
  if (!empty(beat) && !empty(lost) && lost < beat * 2) {
    const fixes = [];
    if (beat * 2 <= 600) fixes.push(fix(`Raise connection lost to ${seconds(beat * 2)}`, { overdueSeconds: beat * 2 }));
    const slower = Math.floor(lost / 2);
    if (slower >= 5 && slower <= 60) fixes.push(fix(`Lower the heartbeat to ${seconds(slower)}`, { heartbeatSeconds: slower }));
    add('conflict', ['overdueSeconds', 'heartbeatSeconds'], `Connection lost after must be at least two heartbeats: it is ${lost} s and the heartbeat is ${beat} s.`, fixes);
  }
  if (!empty(lost) && lost > 600) add('field', ['overdueSeconds'], `Connection lost after must be at most 10 minutes (it is ${lost} s).`, toRange('overdueSeconds', lost, 1, 600));
  if (!empty(start) && (start < 1 || start > 600)) add('field', ['focusSeconds'], `Alarm and screenshots start after must be between 1 second and 10 minutes (it is ${start} s).`, toRange('focusSeconds', start, 1, 600));
  if (!empty(repeat) && (repeat < 1 || repeat > 10)) add('field', ['alertSeconds'], `Alarm repeats every must be between 1 and 10 seconds (it is ${repeat} s).`, toRange('alertSeconds', repeat, 1, 10));
  if (!empty(capture) && (capture < 3 || capture > 60)) add('field', ['captureSeconds'], `Screenshot every must be between 3 and 60 seconds (it is ${capture} s).`, toRange('captureSeconds', capture, 3, 60));
  // A lock is off at zero, otherwise a whole number of seconds from one to an hour.
  for (const [field, value, label] of [['lockAwaySeconds', away, 'Lock after away'], ['lockNoContactSeconds', noContact, 'Lock after no contact']]) {
    if (empty(value)) continue;
    const target = value <= 0 ? 0 : Math.min(3600, Math.round(value));
    const fixes = [fix(target === 0 ? 'Turn the lock off' : `Use ${say(field, target)}`, { [field]: target })];
    if (!Number.isInteger(value)) add('field', [field], `${label} must be a whole number of seconds (it is ${value}).`, fixes);
    else if (value !== 0 && (value < 1 || value > 3600)) add('field', [field], `${label} must be off or between 1 second and 60 minutes (it is ${value} s).`, fixes);
  }
  if (lockIn(away) && !empty(start) && !empty(capture) && away < start + capture) {
    const fixes = [];
    const needed = Math.ceil(start + capture);
    if (needed <= 3600) fixes.push(fix(`Raise the lock to ${seconds(needed)}`, { lockAwaySeconds: needed }));
    if (away - capture >= 1 && away - capture <= 600) fixes.push(fix(`Lower the alarm start to ${seconds(away - capture)}`, { focusSeconds: away - capture }));
    else if (away - start >= 3 && away - start <= 60) fixes.push(fix(`Lower the screenshot interval to ${seconds(away - start)}`, { captureSeconds: away - start }));
    add('conflict', ['lockAwaySeconds', 'focusSeconds', 'captureSeconds'],
      `Lock after away (${away} s) must come after the alarm starts (${start} s) plus one screenshot interval (${capture} s): raise the lock or lower the alarm start.`, fixes);
  }
  if (lockIn(noContact) && !empty(lost) && noContact <= lost) {
    const fixes = [];
    const needed = Math.floor(lost) + 1;
    if (needed <= 3600) fixes.push(fix(`Raise the lock to ${seconds(needed)}`, { lockNoContactSeconds: needed }));
    const sooner = noContact - 1;
    if (sooner >= 1 && sooner <= 600 && (empty(beat) || sooner >= beat * 2)) fixes.push(fix(`Lower connection lost to ${seconds(sooner)}`, { overdueSeconds: sooner }));
    add('conflict', ['lockNoContactSeconds', 'overdueSeconds'], `Lock after no contact (${noContact} s) must be longer than the connection lost threshold (${lost} s).`, fixes);
  }
  if (!empty(perAbsence) && !away && (perAbsence < 1 || perAbsence > 500)) add('field', ['incidentCaptures'], `Screenshots per absence must be between 1 and 500 (it is ${perAbsence}).`, toRange('incidentCaptures', perAbsence, 1, 500, true));
  const worked = effectiveCaptures(form);
  if (!empty(perStudent) && (perStudent < worked || perStudent > 2000)) {
    const fixes = [];
    if (finite(worked) && worked <= 2000) {
      const target = clamp(perStudent, worked, 2000);
      fixes.push(fix(`${target > perStudent ? 'Raise' : 'Lower'} to ${target}`, { sessionCaptures: target }));
    }
    add('conflict', ['sessionCaptures'], `Screenshots per student (${perStudent}) must be at least the per-absence count (${worked}) and at most 2000.`, fixes,
      ['sessionCaptures', 'lockAwaySeconds', 'focusSeconds', 'captureSeconds', 'incidentCaptures']);
  }
  if (!empty(grace) && (grace < 1 || grace > 1800 || !Number.isInteger(grace))) add('field', ['graceSeconds'], `Default grace period must be a whole number of seconds between 1 second and 30 minutes (it is ${grace} s).`, toRange('graceSeconds', grace, 1, 1800, true));
  return found;
}

// The limits the other values put on four of the fields right now, in seconds, or null where there is none.
export function limits(form) {
  const { heartbeatSeconds: beat, overdueSeconds: lost, focusSeconds: start, captureSeconds: capture, lockAwaySeconds: away, lockNoContactSeconds: noContact } = form;
  const awayOn = lockIn(away), noContactOn = lockIn(noContact);
  const startMax = awayOn && finite(capture) && away - capture >= 1 ? Math.min(600, away - capture) : null;
  const lostMax = noContactOn ? Math.min(600, noContact - 1) : null;
  return {
    awayMin: finite(start) && finite(capture) ? Math.ceil(start + capture) : null,
    startMax,
    noContactMin: finite(lost) ? Math.floor(lost) + 1 : null,
    lostMin: finite(beat) ? beat * 2 : null,
    lostMax: lostMax !== null && finite(beat) && lostMax >= beat * 2 ? lostMax : null,
  };
}

// The same limits in words, beside the fields they are about; a field with no limit from the other values has none.
export function hints(form) {
  const { overdueSeconds: lost, focusSeconds: start, captureSeconds: capture, lockAwaySeconds: away, lockNoContactSeconds: noContact } = form;
  const at = limits(form);
  const out = {};
  if (at.awayMin !== null) out.lockAwaySeconds = `Off, or at least ${seconds(at.awayMin)}: the alarm starts at ${seconds(start)}, plus a screenshot every ${seconds(capture)}.`;
  if (at.startMax !== null) out.focusSeconds = `At most ${seconds(at.startMax)}: the lock after away (${seconds(away)}) has to come one screenshot (${seconds(capture)}) after it.`;
  if (at.noContactMin !== null) out.lockNoContactSeconds = `Off, or at least ${seconds(at.noContactMin)}: longer than the ${seconds(lost)} after which the connection counts as lost.`;
  if (at.lostMin !== null) {
    out.overdueSeconds = at.lostMax !== null
      ? `From ${seconds(at.lostMin)} (two heartbeats) to ${seconds(at.lostMax)} (just under the lock after no contact, ${seconds(noContact)}).`
      : `At least ${seconds(at.lostMin)}: two heartbeats.`;
  }
  return out;
}

// The values that decide each other, in the order in which each comes after the one before it. When one is changed, the ones after it
// move up and the ones before it move down, as little as it takes.
const CONNECTION = ['heartbeatSeconds', 'overdueSeconds', 'lockNoContactSeconds'];
const ABSENCE = ['focusSeconds', 'captureSeconds', 'lockAwaySeconds', 'incidentCaptures', 'sessionCaptures'];
// The field the proctor changed last among those that decide a conflict: the one to hold when fitting the others to it.
export const heldField = (problems, recent) => recent.find(field => problems.some(problem => problem.kind === 'conflict' && problem.involves.includes(field))) ?? null;
const chainOf = field => CONNECTION.includes(field) ? CONNECTION : ABSENCE.includes(field) ? ABSENCE : [];

function fitConnection(next, held) {
  const on = next.lockNoContactSeconds > 0;
  const heartbeatFor = lost => Math.min(next.heartbeatSeconds, Math.floor(lost / 2));
  if (held === 'lockNoContactSeconds') {
    if (!on) return;
    next.overdueSeconds = Math.min(next.overdueSeconds, next.lockNoContactSeconds - 1);
    next.heartbeatSeconds = heartbeatFor(next.overdueSeconds);
  } else if (held === 'overdueSeconds') {
    next.heartbeatSeconds = heartbeatFor(next.overdueSeconds);
    if (on) next.lockNoContactSeconds = Math.max(next.lockNoContactSeconds, Math.floor(next.overdueSeconds) + 1);
  } else {
    next.overdueSeconds = Math.max(next.overdueSeconds, next.heartbeatSeconds * 2);
    if (on) next.lockNoContactSeconds = Math.max(next.lockNoContactSeconds, Math.floor(next.overdueSeconds) + 1);
  }
}

function fitAbsence(next, held) {
  const on = next.lockAwaySeconds > 0;
  const raiseStudents = () => { next.sessionCaptures = Math.max(next.sessionCaptures, effectiveCaptures(next)); };
  if (held === 'lockAwaySeconds') {
    // The lock is held, so what comes before it gives way: the alarm start first, then the screenshot interval.
    if (on && next.focusSeconds + next.captureSeconds > next.lockAwaySeconds) {
      next.focusSeconds = Math.max(1, next.lockAwaySeconds - next.captureSeconds);
      if (next.focusSeconds + next.captureSeconds > next.lockAwaySeconds) next.captureSeconds = Math.max(3, next.lockAwaySeconds - next.focusSeconds);
    }
    raiseStudents();
  } else if (held === 'focusSeconds' || held === 'captureSeconds') {
    if (on) next.lockAwaySeconds = Math.max(next.lockAwaySeconds, Math.ceil(next.focusSeconds + next.captureSeconds));
    raiseStudents();
  } else if (held === 'incidentCaptures') {
    raiseStudents();
  } else if (on) {
    // The screenshots per student are held: fewer of them in the same absence means a longer interval, so the lock stays where it is.
    const span = next.lockAwaySeconds - next.focusSeconds;
    if (effectiveCaptures(next) > next.sessionCaptures && next.sessionCaptures >= 1 && span >= 0) {
      next.captureSeconds = Math.floor(span / next.sessionCaptures) + 1;
    }
  } else {
    next.incidentCaptures = Math.min(next.incidentCaptures, next.sessionCaptures);
  }
}

// The values that would fit the held one, or null when they cannot be fitted: nothing conflicts with it, a value in its chain is wrong
// on its own (the buttons in the list are for that), or no values in range would make them agree. Otherwise the whole form as it would
// be, and the values that differ. The held field itself is never touched.
export function fit(form, held) {
  const chain = chainOf(held);
  if (!chain.length) return null;
  const inChain = problem => problem.involves.some(field => chain.includes(field));
  const before = findProblems(form);
  if (before.some(problem => problem.kind === 'field' && problem.fields.some(field => chain.includes(field)))) return null;
  if (!before.some(problem => problem.kind === 'conflict' && inChain(problem))) return null;
  const next = { ...form };
  if (chain === CONNECTION) fitConnection(next, held); else fitAbsence(next, held);
  const known = new Set(before.map(problem => problem.message));
  const after = findProblems(next);
  if (after.some(problem => (problem.kind === 'conflict' && inChain(problem)) || !known.has(problem.message))) return null;
  const changes = Object.keys(DEFAULTS).filter(field => next[field] !== form[field]).map(field => ({ field, label: LABELS[field], from: form[field], to: next[field] }));
  return { form: next, changes };
}

// A change in the words of the page, which the labels are made to flow into: "Lock after away for 5 min → 25 s".
export function changeText(change) {
  return `${change.label} ${say(change.field, change.from)} → ${say(change.field, change.to)}`;
}

// What each field is, said under it: the limit the other values put on it when there is one (hints), otherwise what it does. A lock that
// is off says what off means.
export function descriptions(form) {
  const limit = hints(form);
  const awayOff = form.lockAwaySeconds === 0, noContactOff = form.lockNoContactSeconds === 0;
  const worked = effectiveCaptures(form);
  return {
    heartbeatSeconds: 'How often each student’s computer checks in with the server.',
    overdueSeconds: limit.overdueSeconds ?? 'How long a computer may stay silent before its connection counts as lost.',
    lockNoContactSeconds: noContactOff ? 'Off: a silent computer is flagged, but its exam never locks.'
      : limit.lockNoContactSeconds ?? 'Longer than the time after which the connection counts as lost.',
    focusSeconds: limit.focusSeconds ?? 'How long a student may be away before the alarm sounds and the screenshots start.',
    alertSeconds: 'The alarm sounds again at this interval until the student comes back.',
    captureSeconds: 'While the student is away, a screenshot of every screen at this interval.',
    lockAwaySeconds: awayOff ? 'Off: the alarm keeps going, and the exam never locks for being away.'
      : limit.lockAwaySeconds ?? 'Long enough for the alarm to start and one screenshot to be taken.',
    incidentCaptures: awayOff ? 'With the away lock off, this caps the screenshots of one absence.'
      : finite(worked) ? `Worked out from the away lock: ${worked} cover the whole absence up to the lock.` : 'Worked out from the away lock.',
    sessionCaptures: finite(worked) && worked >= 1 ? `At least ${worked}, one whole absence, and at most 2000.` : 'At most 2000.',
    graceSeconds: 'Extra time after the exam ends. Each exam can change it.',
  };
}

// The section of each field, for the marks of the section list.
export const sectionOf = field => SECTIONS.find(section => section.fields.includes(field))?.id ?? null;
