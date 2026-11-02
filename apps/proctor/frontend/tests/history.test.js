import { test } from 'node:test';
import assert from 'node:assert/strict';
import { historyRows } from '../src/history.js';

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
