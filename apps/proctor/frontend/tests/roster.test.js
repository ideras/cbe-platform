import { test } from 'node:test';
import assert from 'node:assert/strict';
import { rosterCounts, filterRoster, sortRoster, needsAttention, inProgress, isSubmitted, canForce, initials, lockLabel, lowTime, networkBadge, focusBadge, lifecycleBadge, sound } from '../src/roster.js';

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
  assert.equal(lowTime(locked, observation), false);
  assert.equal(lowTime({ ...locked, seconds_remaining: 300 }, observation), true);
  assert.equal(lowTime(row('Ada', 'submitted', { seconds_remaining: 0 }), observation), false);
  assert.equal(lowTime(row('Ada', 'grace', { grace_until: '2035-01-01T00:03:00Z' }), observation), true);
  assert.equal(lifecycleBadge({ ...locked, finalization_pending: true }, {}, observation).text, 'Finalizing');
  assert.equal(lifecycleBadge(row('Ada', 'force_submitted'), {}, observation).text, 'Force-submitted');
});
