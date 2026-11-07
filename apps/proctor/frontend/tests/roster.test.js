import { test } from 'node:test';
import assert from 'node:assert/strict';
import { rosterCounts, filterRoster, sortRoster, needsAttention, inProgress, isSubmitted, canForce, initials, lockLabel, lowTime, networkBadge, focusBadge, lifecycleBadge, sound, examLength, loginGate, remoteFindings } from '../src/roster.js';

const row = (name, status = 'active', extra = {}) => ({ student_name: name, account_number: name, status,
  presence: { monitoring: status === 'active', connectivity: 'healthy', focus: { state: 'focused' } }, ...extra });

test('summary and filters use the same server states; overlapping warnings count once', () => {
  const rows = [row('Ada', 'active', { network_warning: true, focus_warning: true }),
    row('Grace', 'grace', { locked: { reason: 'away' } }), row('Alan', 'active'),
    row('Dennis', 'submitted'), row('Edsger', 'force_submitted'),
    row('Barbara', 'submitted', { finalization_pending: true })];
  assert.deepEqual(rosterCounts(rows), { total: 6, progress: 3, attention: 3, locked: 1, submitted: 2 });
  assert.deepEqual(filterRoster(rows, 'attention').map(r => r.student_name), ['Ada', 'Grace', 'Barbara']);
  assert.equal(filterRoster(rows, 'progress').length, 3); // Locks do not stop running time.
  assert.equal(filterRoster(rows, 'submitted').length, 2);
  assert.equal(filterRoster(rows, 'all'), rows);
  assert.equal(filterRoster(rows, 'constructor'), rows);
  assert.equal(isSubmitted(rows[5]), false);
  assert.equal(inProgress(rows[5]), false);
  assert.equal(needsAttention(rows[5]), true);
  assert.equal(canForce(rows[5]), true);
  assert.deepEqual(rosterCounts([]), { total: 0, progress: 0, attention: 0, locked: 0, submitted: 0 });
});

test('locks sort first and names sort within each group, without mutating reports', () => {
  const rows = [row('Zoe'), row('Dennis', 'active', { locked: {} }), row('Ada'), row('Barbara', 'active', { locked: {} })];
  assert.deepEqual(sortRoster(rows).map(r => r.student_name), ['Barbara', 'Dennis', 'Ada', 'Zoe']);
  assert.equal(rows[0].student_name, 'Zoe');
  assert.equal(initials(' Grace Hopper '), 'GH');
  assert.equal(initials('Ada'), 'A');
  assert.equal(initials(''), '');
});

test('badge labels retain unknown state and independent network/focus warnings', () => {
  const healthy = row('Ada');
  assert.deepEqual(networkBadge(healthy), { tone: 'ok', text: 'Online' });
  assert.equal(focusBadge(healthy).text, 'Focused');
  const unknown = row('Ada', 'active', { network_warning: true, presence: { monitoring: true, connectivity: 'overdue', focus: { state: 'unknown' } } });
  assert.equal(networkBadge(unknown).tone, 'network');
  assert.equal(focusBadge(unknown).text, 'Unknown');
  unknown.focus_warning = true;
  assert.equal(focusBadge(unknown).tone, 'focus');
  assert.equal(networkBadge(row('Ada', 'submitted')).text, 'Terminal');
  assert.equal(sound({ volume_percent: 0 }), '0%');
  assert.equal(sound({ volume_percent: 35, volume_muted: true }), 'muted (35%)');
  assert.equal(sound({}), 'not reported');
});

test('remaining time and lock age never use a local ticking clock', () => {
  const observation = '2035-01-01T00:02:00Z';
  const locked = row('Ada', 'active', { locked: { since: '2035-01-01T00:00:00Z', reason: 'away' }, seconds_remaining: 400 });
  assert.deepEqual(lockLabel(locked, { lock_away_after_ms: 300000 }, observation), { reason: 'away 5 min', ago: '2 min ago' });
  const changing = row('Grace', 'active', { locked: { since: locked.locked.since, reason: 'network' } });
  assert.deepEqual(lockLabel(changing, { lock_away_after_ms: 300000 }, observation), { reason: 'changing network', ago: '2 min ago' });
  assert.equal(lowTime(locked, observation), false);
  assert.equal(lowTime({ ...locked, seconds_remaining: 300 }, observation), true);
  assert.equal(lowTime(row('Ada', 'submitted', { seconds_remaining: 0 }), observation), false);
  assert.equal(lowTime(row('Ada', 'grace', { grace_until: '2035-01-01T00:03:00Z' }), observation), true);
  assert.equal(lifecycleBadge({ ...locked, finalization_pending: true }, {}, observation).text, 'Finalizing');
  assert.equal(lifecycleBadge(row('Ada', 'force_submitted'), {}, observation).text, 'Force-submitted');
});

test('an exam length is told in hours, minutes and seconds, leaving out what is zero', () => {
  assert.equal(examLength(7200), '2 h');
  assert.equal(examLength(5400), '1 h 30 min');
  assert.equal(examLength(1800), '30 min');
  assert.equal(examLength(60), '1 min');
  assert.equal(examLength(90), '1 min 30 s');
  assert.equal(examLength(45), '45 s');
  assert.equal(examLength(3661), '1 h 1 min 1 s');
  assert.equal(examLength(100 * 3600), '100 h'); // no wrap at a day
  assert.equal(examLength(0), '0 s');
});

test('an exam length that is not known says so instead of showing a number', () => {
  for (const value of [null, undefined, NaN, Infinity, -5]) assert.equal(examLength(value), 'unknown');
});

test('a login refused while the account was open is reported until a release is used, and needs attention until it is answered', () => {
  const row = (gate, extra = {}) => ({ status: 'active', finalization_pending: false, locked: null, network_warning: false, focus_warning: false, login_gate: gate, ...extra });
  assert.equal(loginGate(row(null)), null);
  assert.equal(loginGate(row(undefined)), null);
  assert.equal(loginGate(row({ refusals: 0, released_at: null })), null);
  const refused = loginGate(row({ refusals: 2, last_refused_at: '2035-01-01T00:00:50Z', last_refused_addr: '10.0.9.9', released_at: null }));
  assert.deepEqual(refused, { refusals: 2, lastAt: '2035-01-01T00:00:50Z', addr: '10.0.9.9', releasedAt: null, released: false, waiting: true });
  const released = loginGate(row({ refusals: 2, last_refused_at: '2035-01-01T00:00:50Z', last_refused_addr: '10.0.9.9', released_at: '2035-01-01T00:01:00Z' }));
  assert.equal(released.released, true);
  assert.equal(released.waiting, false);
  // A release can wait for a login even though the refusals were counted before it.
  assert.equal(loginGate(row({ refusals: 0, released_at: '2035-01-01T00:01:00Z' })).released, true);
  // A refusal makes the row need attention, like a lock; once a proctor has released it, it no longer does.
  assert.equal(needsAttention(row({ refusals: 1, last_refused_at: 'x', released_at: null })), true);
  assert.equal(needsAttention(row({ refusals: 1, last_refused_at: 'x', released_at: 'y' })), false);
  assert.equal(needsAttention(row(null)), false);
  assert.equal(rosterCounts([row({ refusals: 1, last_refused_at: 'x', released_at: null }), row(null)]).attention, 1);
});

test('what the application found on the student\'s computer is listed in its own words, and makes the row need attention', () => {
  const found = [{ kind: 'remote_session', name: 'Remote Desktop', detail: 'LAPTOP-1' }, { kind: 'remote_tool', name: 'AnyDesk.exe' }];
  const watched = row('Ada', 'active', { presence: { monitoring: true, connectivity: 'healthy', focus: { state: 'focused' }, remote: found } });
  assert.deepEqual(remoteFindings(watched), ['Remote Desktop from LAPTOP-1', 'AnyDesk.exe']);
  assert.equal(needsAttention(watched), true);
  assert.equal(rosterCounts([watched, row('Alan')]).attention, 1);
  // A session that does not say where it comes from is still named; nothing found, or an older server, says nothing.
  assert.deepEqual(remoteFindings(row('x', 'active', { presence: { remote: [{ kind: 'remote_session', name: 'Remote Desktop' }] } })), ['Remote Desktop session']);
  for (const nothing of [row('Alan'), row('Dennis', 'submitted', { presence: { remote: [] } }), { presence: { remote: 'not a list' } }, { presence: { remote: [null, { kind: 'remote_tool' }, { name: 7 }] } }, {}]) {
    assert.deepEqual(remoteFindings(nothing), []);
    assert.equal(needsAttention({ ...nothing, status: 'active' }), false);
  }
});
