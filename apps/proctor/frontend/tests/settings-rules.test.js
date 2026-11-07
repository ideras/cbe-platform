import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { DEFAULTS, LABELS, findProblems, limits, hints, fit, heldField, effectiveCaptures, say, changeText } from '../src/settings-rules.js';

const form = (change = {}) => ({ ...DEFAULTS, ...change });
const messages = values => findProblems(values).map(problem => problem.message);
const conflict = (values, ...fields) => findProblems(values).some(problem => problem.kind === 'conflict' && fields.every(field => problem.fields.includes(field)));
const awayConflict = values => conflict(values, 'lockAwaySeconds', 'focusSeconds');
const contactConflict = values => conflict(values, 'lockNoContactSeconds', 'overdueSeconds');
const beatConflict = values => conflict(values, 'heartbeatSeconds', 'overdueSeconds');
const studentsConflict = values => conflict(values, 'sessionCaptures');

test('the defaults are the server defaults, with no problem and a worked-out count of 60', () => {
  assert.deepEqual(findProblems(DEFAULTS), []);
  assert.equal(effectiveCaptures(DEFAULTS), 60);
  assert.deepEqual(Object.keys(DEFAULTS), Object.keys(LABELS));
});

test('the problems say what they always said', () => {
  assert.deepEqual(messages(form({ heartbeatSeconds: 2 })), ['Heartbeat every must be between 5 and 60 seconds (it is 2).']);
  assert.deepEqual(messages(form({ overdueSeconds: 12 })), ['Connection lost after must be at least two heartbeats: it is 12 s and the heartbeat is 15 s.']);
  assert.deepEqual(messages(form({ lockAwaySeconds: 9 })), [
    'Lock after away (9 s) must come after the alarm starts (5 s) plus one screenshot interval (5 s): raise the lock or lower the alarm start.']);
  assert.deepEqual(messages(form({ lockNoContactSeconds: 30 })), ['Lock after no contact (30 s) must be longer than the connection lost threshold (30 s).']);
  assert.deepEqual(messages(form({ lockAwaySeconds: 0.5 })), ['Lock after away must be a whole number of seconds (it is 0.5).']);
  assert.deepEqual(messages(form({ lockAwaySeconds: 3601 })).slice(0, 1), ['Lock after away must be off or between 1 second and 60 minutes (it is 3601 s).']);
  assert.deepEqual(messages(form({ sessionCaptures: 59 })), ['Screenshots per student (59) must be at least the per-absence count (60) and at most 2000.']);
  assert.deepEqual(messages(form({ graceSeconds: null })), ['Default grace period is empty: type the value you want.']);
});

test('a hint names exactly the least the other values allow: one second less is refused, that much is not', () => {
  // The away lock against the alarm start and the screenshot interval.
  for (const [focusSeconds, captureSeconds] of [[5, 5], [1, 3], [60, 7], [600, 60], [1, 60]]) {
    const base = form({ focusSeconds, captureSeconds });
    const least = limits(base).awayMin;
    assert.equal(least, focusSeconds + captureSeconds);
    for (let lock = 1; lock <= least + 3; lock++) {
      assert.equal(awayConflict({ ...base, lockAwaySeconds: lock }), lock < least, `lock ${lock} with alarm ${focusSeconds} and screenshot ${captureSeconds}`);
    }
  }
  // The alarm start against the away lock that is on: the most it may be.
  for (const [lockAwaySeconds, captureSeconds] of [[300, 5], [8, 5], [4, 3], [61, 60], [3600, 3]]) {
    const base = form({ lockAwaySeconds, captureSeconds, sessionCaptures: 2000 });
    const most = limits(base).startMax;
    assert.equal(most, Math.min(600, lockAwaySeconds - captureSeconds));
    for (const start of [1, most - 1, most, most + 1, 600].filter(value => value >= 1 && value <= 600)) {
      assert.equal(awayConflict({ ...base, focusSeconds: start }), start > most, `alarm ${start} with lock ${lockAwaySeconds} and screenshot ${captureSeconds}`);
    }
  }
  // The no contact lock against the time after which the connection counts as lost.
  for (const [heartbeatSeconds, overdueSeconds] of [[15, 30], [5, 10], [60, 600], [5, 11]]) {
    const base = form({ heartbeatSeconds, overdueSeconds });
    const least = limits(base).noContactMin;
    assert.equal(least, overdueSeconds + 1);
    for (let lock = 1; lock <= least + 3; lock++) assert.equal(contactConflict({ ...base, lockNoContactSeconds: lock }), lock < least, `contact lock ${lock} with lost ${overdueSeconds}`);
  }
  // The time after which the connection counts as lost, between two heartbeats and just under the no contact lock.
  for (const [heartbeatSeconds, lockNoContactSeconds] of [[15, 300], [5, 11], [30, 100], [60, 3600]]) {
    const base = form({ heartbeatSeconds, lockNoContactSeconds });
    const at = limits(base);
    assert.equal(at.lostMin, heartbeatSeconds * 2);
    assert.equal(at.lostMax, Math.min(600, lockNoContactSeconds - 1));
    for (const lost of [at.lostMin - 1, at.lostMin, at.lostMax, at.lostMax + 1].filter(value => value >= 1 && value <= 600)) {
      const values = { ...base, overdueSeconds: lost };
      assert.equal(beatConflict(values), lost < at.lostMin, `lost ${lost} with heartbeat ${heartbeatSeconds}`);
      assert.equal(contactConflict(values), lost > at.lostMax, `lost ${lost} with contact lock ${lockNoContactSeconds}`);
    }
  }
});

test('a limit that does not exist is not shown: no alarm limit without an away lock, no upper limit without a no contact lock', () => {
  assert.equal(limits(form({ lockAwaySeconds: 0 })).startMax, null);
  assert.equal(hints(form({ lockAwaySeconds: 0 })).focusSeconds, undefined);
  assert.equal(limits(form({ lockNoContactSeconds: 0 })).lostMax, null);
  // A lock that leaves no room for the alarm has no most to name; the lock's own hint and the problem say what to do.
  assert.equal(limits(form({ lockAwaySeconds: 5, captureSeconds: 5 })).startMax, null);
  // Nothing is worked out from a value that is not a number.
  const unfinished = hints(form({ focusSeconds: null, heartbeatSeconds: NaN }));
  assert.deepEqual(Object.keys(unfinished), ['focusSeconds', 'lockNoContactSeconds']);
});

test('the hints of the defaults say the limits in the words of the page', () => {
  assert.deepEqual(hints(DEFAULTS), {
    lockAwaySeconds: 'Off, or at least 10 s: the alarm starts at 5 s, plus a screenshot every 5 s.',
    focusSeconds: 'At most 4 min 55 s: the lock after away (5 min) has to come one screenshot (5 s) after it.',
    lockNoContactSeconds: 'Off, or at least 31 s: longer than the 30 s after which the connection counts as lost.',
    overdueSeconds: 'From 30 s (two heartbeats) to 4 min 59 s (just under the lock after no contact, 5 min).',
  });
  assert.equal(hints(form({ lockNoContactSeconds: 0 })).overdueSeconds, 'At least 30 s: two heartbeats.');
  // The tightest settings of all: a 4 s lock and an 11 s lock.
  const tight = form({ heartbeatSeconds: 5, overdueSeconds: 10, focusSeconds: 1, captureSeconds: 3, lockAwaySeconds: 4, lockNoContactSeconds: 11 });
  assert.deepEqual(findProblems(tight), []);
  assert.equal(limits(tight).awayMin, 4);
  assert.equal(limits(tight).noContactMin, 11);
});

test('every button on a problem solves that problem, and says in its label what it will do', () => {
  const broken = [
    form({ heartbeatSeconds: 2 }), form({ heartbeatSeconds: 100 }), form({ overdueSeconds: 12 }), form({ overdueSeconds: 700 }),
    form({ focusSeconds: 0 }), form({ focusSeconds: 900 }), form({ alertSeconds: 20 }), form({ captureSeconds: 1 }), form({ captureSeconds: 90 }),
    form({ lockAwaySeconds: 9 }), form({ lockAwaySeconds: 3 }), form({ lockAwaySeconds: 8, focusSeconds: 5, captureSeconds: 5 }),
    form({ lockAwaySeconds: 0.5 }), form({ lockAwaySeconds: -4 }), form({ lockAwaySeconds: 9999 }), form({ lockNoContactSeconds: 30 }),
    form({ lockNoContactSeconds: 12 }), form({ lockNoContactSeconds: 3601 }), form({ lockAwaySeconds: 0, incidentCaptures: 900 }),
    form({ sessionCaptures: 40 }), form({ sessionCaptures: 5000 }), form({ graceSeconds: 2000 }), form({ graceSeconds: 1.5 }),
    form({ graceSeconds: null }), form({ lockAwaySeconds: null }), form({ sessionCaptures: null }),
  ];
  let buttons = 0;
  for (const values of broken) {
    for (const problem of findProblems(values)) {
      assert.ok(problem.fixes.length > 0, `no fix for: ${problem.message}`);
      for (const fix of problem.fixes) {
        buttons++;
        const fixed = { ...values, ...fix.set };
        const again = findProblems(fixed).find(other => other.kind === problem.kind && other.fields.join() === problem.fields.join());
        assert.equal(again, undefined, `"${fix.label}" left: ${again?.message}`);
      }
    }
  }
  assert.ok(buttons >= 30, `${buttons} buttons`);
  const labels = problem => findProblems(problem)[0].fixes.map(fix => fix.label);
  assert.deepEqual(labels(form({ lockAwaySeconds: 9 })), ['Raise the lock to 10 s', 'Lower the alarm start to 4 s']);
  assert.deepEqual(labels(form({ lockAwaySeconds: 8, focusSeconds: 5, captureSeconds: 5 })), ['Raise the lock to 10 s', 'Lower the alarm start to 3 s']);
  assert.deepEqual(labels(form({ lockAwaySeconds: 6, focusSeconds: 3, captureSeconds: 5 })), ['Raise the lock to 8 s', 'Lower the alarm start to 1 s']);
  // With no room to lower the alarm start, it is the screenshot interval that can come down, if it can come down far enough.
  assert.deepEqual(labels(form({ lockAwaySeconds: 8, focusSeconds: 5, captureSeconds: 8 })), ['Raise the lock to 13 s', 'Lower the screenshot interval to 3 s']);
  assert.deepEqual(labels(form({ lockAwaySeconds: 6, focusSeconds: 5, captureSeconds: 10 })), ['Raise the lock to 15 s']);
  // The connection can come down only as far as two heartbeats.
  assert.deepEqual(labels(form({ lockNoContactSeconds: 30 })), ['Raise the lock to 31 s']);
  assert.deepEqual(labels(form({ heartbeatSeconds: 5, lockNoContactSeconds: 30 })), ['Raise the lock to 31 s', 'Lower connection lost to 29 s']);
  assert.deepEqual(labels(form({ overdueSeconds: 12 })), ['Raise connection lost to 30 s', 'Lower the heartbeat to 6 s']);
  assert.deepEqual(labels(form({ sessionCaptures: 40 })), ['Raise to 60']);
  assert.deepEqual(labels(form({ lockAwaySeconds: -4 })), ['Turn the lock off']);
  assert.deepEqual(labels(form({ lockAwaySeconds: 9999 })), ['Use 60 min']);
  assert.deepEqual(labels(form({ heartbeatSeconds: 2 })), ['Use 5 s']);
  assert.deepEqual(labels(form({ graceSeconds: null })), ['Use 30 s']);
});

test('the held field is the last one changed among those that decide a conflict', () => {
  const values = form({ lockAwaySeconds: 9, lockNoContactSeconds: 30 });
  const problems = findProblems(values);
  assert.equal(heldField(problems, ['lockNoContactSeconds', 'lockAwaySeconds']), 'lockNoContactSeconds');
  assert.equal(heldField(problems, ['graceSeconds', 'lockAwaySeconds', 'lockNoContactSeconds']), 'lockAwaySeconds');
  assert.equal(heldField(problems, ['graceSeconds', 'alertSeconds']), null);
  assert.equal(heldField([], ['lockAwaySeconds']), null);
  // The screenshots per student are decided by the lock, the alarm start and the interval too.
  const students = findProblems(form({ lockAwaySeconds: 3600, captureSeconds: 3, focusSeconds: 1 }));
  assert.equal(heldField(students, ['lockAwaySeconds']), 'lockAwaySeconds');
  assert.equal(heldField(students, ['captureSeconds']), 'captureSeconds');
  assert.equal(heldField(students, ['alertSeconds']), null);
});

const unchanged = (a, b, fields) => fields.every(field => a[field] === b[field]);

test('fitting to a lock that was shortened lowers the alarm start first, then the screenshot interval, and leaves the rest', () => {
  let plan = fit(form({ lockAwaySeconds: 8 }), 'lockAwaySeconds');
  assert.deepEqual(plan.changes.map(changeText), ['Alarm and screenshots start after 5 s → 3 s']);
  assert.equal(plan.form.captureSeconds, 5);
  assert.deepEqual(findProblems(plan.form), []);
  plan = fit(form({ lockAwaySeconds: 4 }), 'lockAwaySeconds');
  assert.deepEqual(plan.changes.map(changeText), ['Alarm and screenshots start after 5 s → 1 s', 'Screenshot every 5 s → 3 s']);
  assert.deepEqual(findProblems(plan.form), []);
  assert.equal(plan.form.lockAwaySeconds, 4);
  // Three seconds cannot be had: the alarm cannot start before the first second and a screenshot takes three.
  assert.equal(fit(form({ lockAwaySeconds: 3 }), 'lockAwaySeconds'), null);
});

test('fitting to a later alarm start or interval raises the lock after them, and the screenshots per student with it', () => {
  let plan = fit(form({ focusSeconds: 300 }), 'focusSeconds');
  assert.deepEqual(plan.changes.map(changeText), ['Lock after away for 5 min → 5 min 5 s']);
  plan = fit(form({ captureSeconds: 60, focusSeconds: 290 }), 'captureSeconds');
  assert.equal(plan.form.lockAwaySeconds, 350);
  // A long lock with quick screenshots needs room for them all.
  plan = fit(form({ lockAwaySeconds: 3600, captureSeconds: 3, focusSeconds: 1 }), 'lockAwaySeconds');
  assert.deepEqual(plan.changes.map(changeText), ['Screenshots per student 200 → 1200']);
  assert.deepEqual(findProblems(plan.form), []);
});

test('fitting to a number of screenshots per student lengthens the interval and keeps the lock where it is', () => {
  const plan = fit(form({ sessionCaptures: 40 }), 'sessionCaptures');
  assert.deepEqual(plan.changes.map(changeText), ['Screenshot every 5 s → 8 s']);
  assert.equal(plan.form.lockAwaySeconds, 300);
  assert.ok(effectiveCaptures(plan.form) <= 40);
  // Not so few that no interval of a minute or less would do.
  assert.equal(fit(form({ sessionCaptures: 3 }), 'sessionCaptures'), null);
  // With the away lock off the per-absence count comes down to it.
  const off = fit(form({ lockAwaySeconds: 0, incidentCaptures: 300, sessionCaptures: 200 }), 'sessionCaptures');
  assert.deepEqual(off.changes.map(changeText), ['Screenshots per absence 300 → 200']);
  const raised = fit(form({ lockAwaySeconds: 0, incidentCaptures: 300, sessionCaptures: 200 }), 'incidentCaptures');
  assert.deepEqual(raised.changes.map(changeText), ['Screenshots per student 200 → 300']);
});

test('fitting the connection: what comes after the held value moves up and what comes before it moves down', () => {
  // A heartbeat of 40 s needs 80 s before the connection counts as lost; the no contact lock of 5 minutes is already longer.
  let plan = fit(form({ heartbeatSeconds: 40, overdueSeconds: 30 }), 'heartbeatSeconds');
  assert.deepEqual(plan.changes.map(changeText), ['Connection lost after 30 s → 1 min 20 s']);
  // ... and when that is longer than the lock, the lock goes after it.
  plan = fit(form({ heartbeatSeconds: 60, overdueSeconds: 30, lockNoContactSeconds: 100 }), 'heartbeatSeconds');
  assert.deepEqual(plan.changes.map(changeText), ['Connection lost after 30 s → 2 min', 'Lock after no contact for 1 min 40 s → 2 min 1 s']);
  plan = fit(form({ overdueSeconds: 400 }), 'overdueSeconds');
  assert.deepEqual(plan.changes.map(changeText), ['Lock after no contact for 5 min → 6 min 41 s']);
  // A lock held at 20 s: the connection counts as lost one second sooner and the heartbeat slows to match.
  plan = fit(form({ lockNoContactSeconds: 20 }), 'lockNoContactSeconds');
  assert.deepEqual(plan.changes.map(changeText), ['Heartbeat every 15 s → 9 s', 'Connection lost after 30 s → 19 s']);
  assert.deepEqual(findProblems(plan.form), []);
  // As short as it gets: 11 s, with a 10 s threshold and a 5 s heartbeat.
  plan = fit(form({ lockNoContactSeconds: 11 }), 'lockNoContactSeconds');
  assert.deepEqual([plan.form.heartbeatSeconds, plan.form.overdueSeconds], [5, 10]);
  // Shorter than that no heartbeat in range would do.
  assert.equal(fit(form({ lockNoContactSeconds: 10 }), 'lockNoContactSeconds'), null);
});

test('fitting does nothing when nothing conflicts or a value is wrong on its own', () => {
  assert.equal(fit(DEFAULTS, 'lockAwaySeconds'), null);
  assert.equal(fit(form({ focusSeconds: 20 }), 'focusSeconds'), null);
  assert.equal(fit(form({ graceSeconds: 20 }), 'graceSeconds'), null);
  assert.equal(fit(form({ lockAwaySeconds: 8 }), 'unknown'), null);
  // The screenshot interval is out of range, so what depends on it cannot be fitted before it is fixed.
  assert.equal(fit(form({ lockAwaySeconds: 8, captureSeconds: 1 }), 'lockAwaySeconds'), null);
  // A wrong value in the other chain does not stop this one.
  assert.ok(fit(form({ lockAwaySeconds: 8, graceSeconds: 5000 }), 'lockAwaySeconds'));
  // A conflict in the other chain is left for its own turn.
  const both = form({ lockAwaySeconds: 8, lockNoContactSeconds: 30 });
  const plan = fit(both, 'lockAwaySeconds');
  assert.ok(contactConflict(plan.form));
  assert.ok(!awayConflict(plan.form));
  assert.equal(plan.form.lockNoContactSeconds, 30);
});

test('whatever is held, a fit leaves it as it was, touches only its chain, solves its conflicts and has nothing left to fit', () => {
  const CONNECTION = ['heartbeatSeconds', 'overdueSeconds', 'lockNoContactSeconds'];
  const ABSENCE = ['focusSeconds', 'captureSeconds', 'lockAwaySeconds', 'incidentCaptures', 'sessionCaptures'];
  const tries = { heartbeatSeconds: [5, 20, 40, 60], overdueSeconds: [10, 45, 200, 600], lockNoContactSeconds: [1, 11, 20, 60, 3600],
    focusSeconds: [1, 30, 200, 400, 600], captureSeconds: [3, 12, 40, 60], lockAwaySeconds: [1, 4, 8, 20, 120, 3600],
    incidentCaptures: [1, 100, 500], sessionCaptures: [1, 2, 10, 40, 59, 500, 2000] };
  let fitted = 0;
  const refused = [];
  for (const [held, values] of Object.entries(tries)) {
    const chain = CONNECTION.includes(held) ? CONNECTION : ABSENCE;
    for (const lockOff of [false, true]) {
      for (const value of values) {
        const start = { ...DEFAULTS, ...(lockOff ? { lockAwaySeconds: 0, lockNoContactSeconds: 0 } : {}), [held]: value };
        const plan = fit(start, held);
        const label = `${held}=${value}${lockOff ? ' with the locks off' : ''}`;
        if (!plan) {
          if (findProblems(start).some(problem => problem.kind === 'conflict' && problem.involves.some(field => chain.includes(field)))) refused.push(label);
          continue;
        }
        fitted++;
        assert.equal(plan.form[held], start[held], `${label}: the held field moved`);
        assert.ok(unchanged(plan.form, start, Object.keys(DEFAULTS).filter(field => !chain.includes(field))), `${label}: another chain moved`);
        assert.ok(!findProblems(plan.form).some(problem => problem.kind === 'conflict' && problem.involves.some(field => chain.includes(field))), `${label}: a conflict is left: ${messages(plan.form)}`);
        assert.deepEqual(findProblems(plan.form).map(problem => problem.message).filter(message => !messages(start).includes(message)), [], `${label}: a new problem`);
        assert.ok(plan.changes.length > 0 && plan.changes.every(change => change.from !== change.to), `${label}: changes`);
        assert.equal(fit(plan.form, held), null, `${label}: can be fitted again`);
      }
    }
  }
  // Where there was a conflict and no fit, it is because no values in range could solve it: a lock of a second has no room for the alarm
  // and a screenshot, a lock of 1 s after no contact none for a heartbeat, and one or two screenshots per student would take an interval
  // of over a minute.
  assert.deepEqual(refused, ['lockNoContactSeconds=1', 'lockNoContactSeconds=1 with the locks off', 'lockAwaySeconds=1', 'lockAwaySeconds=1 with the locks off',
    'sessionCaptures=1', 'sessionCaptures=2']);
  assert.ok(fitted >= 25, `${fitted} fitted`);
});

test('a value is said in the words of the page', () => {
  assert.equal(say('lockAwaySeconds', 0), 'off');
  assert.equal(say('lockNoContactSeconds', 90), '1 min 30 s');
  assert.equal(say('sessionCaptures', 200), '200');
  assert.equal(say('graceSeconds', 0), '0 s');
  assert.equal(changeText({ label: 'Lock after away for', field: 'lockAwaySeconds', from: 300, to: 25 }), 'Lock after away for 5 min → 25 s');
});

test('the defaults are the ones the server starts with, as domain.DefaultPolicy has them', () => {
  const source = readFileSync(new URL('../../../../internal/domain/policy.go', import.meta.url), 'utf8');
  const start = source.indexOf('func DefaultPolicy()');
  const body = source.slice(start, source.indexOf('.Normalized()', start));
  const names = { HeartbeatInterval: 'heartbeatSeconds', OverdueThreshold: 'overdueSeconds', FocusThreshold: 'focusSeconds', AlertInterval: 'alertSeconds',
    CaptureInterval: 'captureSeconds', LockAwayAfter: 'lockAwaySeconds', LockNoContactAfter: 'lockNoContactSeconds', DefaultGrace: 'graceSeconds',
    IncidentCaptures: 'incidentCaptures', SessionCaptures: 'sessionCaptures' };
  const found = {};
  for (const [, name, amount, unit] of body.matchAll(/(\w+):\s+(\d+)(?: \* time\.(Second|Minute))?,/g)) found[names[name]] = Number(amount) * (unit === 'Minute' ? 60 : 1);
  assert.deepEqual(found, { ...DEFAULTS }, 'the Settings page restores values that are not the server defaults: change DEFAULTS in settings-rules.js');
});
