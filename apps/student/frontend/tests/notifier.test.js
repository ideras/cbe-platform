import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Notifier, describeProblem, volumeNotice } from '../src/notifier.js';

const make = options => { const changes = []; const notifier = new Notifier({ ...options, onChange: () => changes.push(notifier.visible().length) }); return { notifier, changes }; };
const texts = notifier => notifier.visible().map(toast => toast.text);

test('a condition that holds on every poll is shown once', () => {
  const { notifier, changes } = make();
  const held = [{ key: 'heartbeat', text: 'Heartbeat reporting unavailable' }];
  for (let poll = 0; poll < 20; poll++) notifier.sync(held);
  assert.deepEqual(texts(notifier), ['Heartbeat reporting unavailable']);
  assert.equal(changes.length, 1);
});

test('a dismissed condition stays quiet until it has ended; its next occurrence is shown again', () => {
  const { notifier } = make();
  const held = [{ key: 'save', text: 'Save failed', kind: 'error' }];
  notifier.sync(held);
  notifier.dismiss(notifier.visible()[0].id);
  notifier.sync(held); notifier.sync(held);
  assert.deepEqual(texts(notifier), []);
  notifier.sync([]);
  notifier.sync(held);
  assert.deepEqual(texts(notifier), ['Save failed']);
});

test('a condition that ends takes its toast away', () => {
  const { notifier } = make();
  notifier.sync([{ key: 'a', text: 'A' }, { key: 'b', text: 'B' }]);
  notifier.sync([{ key: 'b', text: 'B' }]);
  assert.deepEqual(texts(notifier), ['B']);
});

test('a condition whose words change updates its toast instead of stacking another', () => {
  const { notifier } = make();
  notifier.sync([{ key: 'dropped', text: 'Dropped events: 1' }]);
  notifier.sync([{ key: 'dropped', text: 'Dropped events: 2' }]);
  assert.deepEqual(texts(notifier), ['Dropped events: 2']);
});

test('a warning stays for its reading time, then goes and does not come back while the condition holds', () => {
  const { notifier } = make({ warningMs: 15000 });
  const held = [{ key: 'timing', text: 'Timing stale' }];
  notifier.sync(held);
  notifier.tick(14750);
  assert.equal(notifier.visible().length, 1);
  notifier.tick(250);
  assert.equal(notifier.visible().length, 0);
  notifier.sync(held);
  assert.equal(notifier.visible().length, 0);
});

test('time does not pass for a blocking toast', () => {
  const { notifier } = make({ warningMs: 1000 });
  notifier.post('capture', 'Retest required', { kind: 'error', sticky: true });
  notifier.tick(60000);
  assert.deepEqual(texts(notifier), ['Retest required']);
});

test('an occurrence is shown every time it is posted, and the same key never stacks on itself', () => {
  const { notifier } = make();
  notifier.post('action', 'Credentials were rejected.', { kind: 'error' });
  const first = notifier.visible()[0];
  notifier.dismiss(first.id);
  notifier.post('action', 'Credentials were rejected.', { kind: 'error' });
  assert.deepEqual(texts(notifier), ['Credentials were rejected.']);
  notifier.post('action', 'Credentials were rejected.', { kind: 'error' });
  assert.equal(notifier.visible().length, 1);
});

test('posting again starts the reading time over', () => {
  const { notifier } = make({ warningMs: 1000 });
  notifier.post('volume', 'Restored');
  notifier.tick(900);
  notifier.post('volume', 'Restored');
  notifier.tick(900);
  assert.equal(notifier.visible().length, 1);
});

test('the stack and the queue are bounded, and background warnings never push out a blocking error', () => {
  const { notifier } = make({ maxVisible: 3, maxPending: 4 });
  notifier.post('save', 'Save failed', { kind: 'error', sticky: true });
  for (let i = 0; i < 50; i++) notifier.post(`noise-${i}`, `Background ${i}`);
  assert.equal(notifier.visible().length, 3);
  assert.ok(texts(notifier).includes('Save failed'));
  assert.equal(notifier.items.length, 3 + 4);
  notifier.post('capture', 'Retest required', { kind: 'error', sticky: true });
  assert.ok(texts(notifier).includes('Retest required') && texts(notifier).includes('Save failed'));
});

test('only the toasts on screen use up their time; the ones waiting start when they appear', () => {
  const { notifier } = make({ maxVisible: 1, warningMs: 1000 });
  notifier.post('a', 'First'); notifier.post('b', 'Second');
  notifier.tick(1000);
  assert.deepEqual(texts(notifier), ['Second']);
  notifier.tick(500);
  assert.deepEqual(texts(notifier), ['Second']);
});

test('clear removes a toast whose cause is over; empty text posts nothing', () => {
  const { notifier } = make();
  notifier.post('action', 'Failed'); notifier.post('x', '');
  notifier.clear('action');
  assert.equal(notifier.visible().length, 0);
});

test('problems are shown without the name of the error class', () => {
  assert.equal(describeProblem(new Error('Credentials were rejected.')), 'Credentials were rejected.');
  assert.equal(describeProblem('Error: RuntimeError: Server could not be reached.'), 'Server could not be reached.');
  assert.equal(describeProblem(undefined), 'Something went wrong. Try again.');
});

test('the volume toasts say what the native side did, and a failure is never worded as a success', () => {
  const later = volumeNotice({ sequence: 2, kind: 'restored', initial: false, minimum_percent: 70 });
  assert.equal(later.text, "Please don't lower or mute the volume during the exam. The required volume has been restored to 70% so you can hear the alerts.");
  const failed = volumeNotice({ sequence: 3, kind: 'failed', initial: false, minimum_percent: 70 });
  assert.equal(failed.text, 'The app could not restore the required volume. Keep your sound on and notify your instructor.');
  assert.equal(failed.options.kind, 'error');
  const login = volumeNotice({ sequence: 1, kind: 'restored', initial: true, minimum_percent: 70 });
  assert.doesNotMatch(login.text, /don't lower/); // the student is not blamed for the volume they had before the exam
  assert.match(login.text, /70%/);
});
