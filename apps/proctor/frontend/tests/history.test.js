import { test } from 'node:test';
import assert from 'node:assert/strict';
import { historyRows, lockDetail, remoteDetail } from '../src/history.js';
import { localTime } from '../src/dates.js';

const AT = '2035-01-01T00:00:00Z';
const event = (id, type, detail) => ({ id: `event:${id}`, kind: 'event', occurred_at: AT, event: { event_log_id: id, event_type: type, detail, screenshots: [] } });

test('the history says when an account was refused, released and resumed', () => {
  const [login, resumed, afterRelease, refused, released] = historyRows([
    event(1, 'login', { resumed: false }),
    event(2, 'login', { resumed: true }),
    event(3, 'login', { resumed: true, released: true }),
    event(4, 'login_refused', { remote_addr: '10.0.9.9', retry_after_s: 18 }),
    event(5, 'login_released', { operator_reason: 'the laptop went flat' }),
  ], null);
  assert.equal(login.title, 'Logged in');
  assert.equal(login.note, undefined);
  assert.equal(resumed.note, 'Resumed the session.');
  assert.equal(afterRelease.note, 'Resumed the session after a proctor allowed the login.');
  assert.equal(refused.title, 'Login refused');
  assert.equal(refused.note, 'Another computer (10.0.9.9) tried to open this account while it was open. The session was assumed open for 18 s more.');
  assert.equal(released.title, 'Next login allowed');
  assert.equal(released.note, 'A proctor allowed the next login: the laptop went flat.');
});

test('a refusal without an address or a wait, and a release without a reason, still read sensibly', () => {
  const [refused, released] = historyRows([event(1, 'login_refused', {}), event(2, 'login_released', null)], null);
  assert.equal(refused.note, 'Another computer tried to open this account while it was open.');
  assert.equal(released.note, 'A proctor allowed the next login: no reason recorded.');
});

test('a lock says why, who saw it, and for a lock the student asked for, which request it is', () => {
  assert.equal(lockDetail('locked', { reason: 'away', source: 'server' }), 'Locked: away from the exam window (seen by the server).');
  assert.equal(lockDetail('locked', { reason: 'no_contact', source: 'server' }), 'Locked: no contact (seen by the server).');
  assert.equal(lockDetail('locked', { reason: 'network', source: 'client', request: 1, limit: 2 }),
    'Locked: the student asked to lock the exam to change network (seen by the student application). Request 1 of 2 allowed.');
  // A student past the limit stands out in the history.
  assert.equal(lockDetail('locked', { reason: 'network', source: 'client', request: 3, limit: 2 }),
    'Locked: the student asked to lock the exam to change network (seen by the student application). Request 3 of 2 allowed: over the limit.');
});

test('an absence that was a switch to a text console or another user says so', () => {
  const [window, session] = historyRows([
    event(1, 'focus_lost', { client: { source: 'native_window', local_incident: 1 } }),
    event(2, 'focus_lost', { client: { source: 'session_inactive', local_incident: 2 } }),
  ]);
  assert.equal(window.note, undefined);
  assert.equal(session.note, 'The student\'s session was not the one on screen: a text console or another user.');
  assert.equal(session.kind, 'away');
});

test('an absence says what took the session off screen, whichever report of it carries the reason', () => {
  const lost = (id, client) => event(id, 'focus_lost', { client: { local_incident: id, ...client } });
  const regained = (id, lostId, client) => event(id, 'focus_regained', { focus_lost_event_log_id: lostId, unfocused_duration_ms: 5000, client });
  const [onScreen, byLoss, byReturn, unknown, window] = historyRows([
    lost(1, { source: 'session_inactive', session_reason: 'other_console' }),
    lost(2, { source: 'session_inactive', session_reason: 'other_session' }),
    lost(3, { source: 'native_window' }), regained(4, 3, { source: 'session_active', session_reason: 'other_session' }),
    lost(5, { source: 'session_inactive', session_reason: 'something_new' }),
    lost(6, { source: 'native_window' }), regained(7, 6, { source: 'native_window' }),
  ]);
  assert.equal(onScreen.note, 'The student\'s session was not the one on screen: a text console or another user.');
  assert.equal(byLoss.note, 'The student\'s session was not the one on screen: another user\'s session was, or (on Windows) a remote connection took the session over.');
  // The window lost its focus first, so only the return says it.
  assert.equal(byReturn.note, byLoss.note);
  assert.equal(byReturn.duration, 5000);
  // A reason this page does not know is still not hidden, and an absence of the window alone says nothing.
  assert.equal(unknown.note, 'The student\'s session was not the one on screen.');
  assert.equal(window.note, undefined);
});

test('a screenshot that failed while the student\'s session was off screen says so, and any other failure says nothing', () => {
  const [offScreen, other, older] = historyRows([
    event(1, 'capture_failed', { client: { source: 'capture_health', reason: 'Screen capture failed.', session_reason: 'other_session' } }),
    event(2, 'capture_failed', { client: { source: 'capture_health', reason: 'Screen capture failed.' } }),
    event(3, 'capture_failed', null),
  ]);
  assert.equal(offScreen.title, 'Screenshot capture failed');
  assert.equal(offScreen.note, 'The student\'s session was not the one on screen, so the screens could not be captured.');
  assert.equal(other.note, undefined);
  assert.equal(older.note, undefined);
});

test('a lock the student asked for while the server was out of reach says when it began', () => {
  const since = '2026-11-03T17:51:41.014Z';
  const detail = { reason: 'network', source: 'client', request: 1, limit: 2, since };
  assert.equal(lockDetail('locked', detail, '2026-11-03T17:51:55.694Z'),
    `Locked: the student asked to lock the exam to change network (seen by the student application). Request 1 of 2 allowed. The student asked for it at ${localTime(since)}, 15 s before the server heard of it.`);
  // A lock the server heard of at once says nothing more, and neither does a record that does not say when it began.
  assert.equal(lockDetail('locked', detail, '2026-11-03T17:51:42.014Z'),
    'Locked: the student asked to lock the exam to change network (seen by the student application). Request 1 of 2 allowed.');
  assert.equal(lockDetail('locked', { ...detail, since: undefined }, '2026-11-03T17:51:55.694Z'),
    'Locked: the student asked to lock the exam to change network (seen by the student application). Request 1 of 2 allowed.');
  // Read through the table, the row takes the time of the record from the item.
  const [row] = historyRows([{ ...event(1, 'locked', detail), occurred_at: '2026-11-03T17:51:55.694Z' }]);
  assert.match(row.note, /15 s before the server heard of it\.$/);
});

test('a lock the application made for lack of contact is not said to have been asked for by the student', () => {
  const since = '2026-11-03T17:51:41.014Z';
  const detail = { reason: 'no_contact', source: 'client', since };
  const note = lockDetail('locked', detail, '2026-11-03T17:55:41.014Z');
  assert.equal(note, `Locked: no contact (seen by the student application). The application locked it at ${localTime(since)}, 4 min before the server heard of it.`);
  assert.doesNotMatch(note, /student asked/);
  // The one the server made itself has no earlier start to say.
  assert.equal(lockDetail('locked', { reason: 'no_contact', source: 'server' }, '2026-11-03T17:55:41.014Z'), 'Locked: no contact (seen by the server).');
});

test('the locks that come from the network expect screenshots, the lock for an absence does not', () => {
  const [network, away, noContact] = historyRows([
    event(1, 'locked', { reason: 'network', source: 'client', request: 1, limit: 2 }),
    event(2, 'locked', { reason: 'away', source: 'server' }),
    event(3, 'locked', { reason: 'no_contact', source: 'server' }),
  ]);
  assert.equal(network.expectsImages, true);
  assert.equal(away.expectsImages, false);
  assert.equal(noContact.expectsImages, true);
});

test('a report of remote access says what was found, and that nothing is any more', () => {
  const [found, gone, odd] = historyRows([
    event(1, 'remote_changed', { client: { source: 'remote_watch', active: [{ kind: 'remote_session', name: 'Remote Desktop', detail: 'LAPTOP-1' }, { kind: 'remote_tool', name: 'AnyDesk.exe' }] } }),
    event(2, 'remote_changed', { client: { source: 'remote_watch', active: [] } }),
    event(3, 'remote_changed', null),
  ]);
  assert.equal(found.title, 'Remote access');
  assert.equal(found.note, 'Found on the student\'s computer: Remote Desktop session from LAPTOP-1, AnyDesk.exe.');
  assert.equal(gone.note, 'Nothing found any more: no Remote Desktop session and no remote-control software.');
  assert.equal(odd.note, gone.note);
  assert.equal(remoteDetail({ client: { active: 'x' } }), gone.note);
  assert.equal(remoteDetail({ client: { active: [{ kind: 'remote_tool', name: '<b>x</b>' }] } }), 'Found on the student\'s computer: <b>x</b>.');
});
