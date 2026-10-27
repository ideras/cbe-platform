import { test } from 'node:test';
import assert from 'node:assert/strict';
import { UnfocusAlert, describeSoundFailure, ALERT_VOLUME } from '../src/unfocus-alert.js';

function fakeAudio(play) {
  const audio = { volume: 0.2, currentTime: 9, events: [], async play() { audio.events.push(`play@${audio.volume}`); return play?.(audio); }, pause() { audio.events.push('pause'); } };
  return audio;
}

test('priming plays then immediately pauses, nudges the volume inside the gesture and never sets it to 0', async () => {
  const volumes = [];
  const audio = fakeAudio();
  Object.defineProperty(audio, 'volume', { get: () => volumes.at(-1) ?? 0.2, set: v => volumes.push(v) });
  const alert = new UnfocusAlert(audio, () => assert.fail('unexpected failure'));
  volumes.length = 0;
  await alert.prime();
  assert.deepEqual(audio.events, [`play@${ALERT_VOLUME}`, 'pause']);
  assert.deepEqual(volumes, [ALERT_VOLUME - 0.01, ALERT_VOLUME]);
});

test('priming starts playback before it pauses, so both happen inside the click', () => {
  const audio = fakeAudio();
  const alert = new UnfocusAlert(audio, () => {});
  alert.prime(); // deliberately not awaited: the browser only honors what happens synchronously in the handler
  assert.deepEqual(audio.events, [`play@${ALERT_VOLUME}`, 'pause']);
});

test('the pause that ends a prime is not an error, but a real failure is reported to the caller', async () => {
  const aborted = fakeAudio(() => { throw Object.assign(new Error('interrupted by pause'), { name: 'AbortError' }); });
  await new UnfocusAlert(aborted, () => {}).prime();
  const unsupported = fakeAudio(() => { throw Object.assign(new Error('x'), { name: 'NotSupportedError' }); });
  await assert.rejects(new UnfocusAlert(unsupported, () => {}).prime(), error => { assert.match(describeSoundFailure(error), /could not open the alert sound \(NotSupportedError\)/); return true; });
});

test('every beep of a focus loss plays once, at the alert volume, and an old one never plays again', async () => {
  const audio = fakeAudio();
  const alert = new UnfocusAlert(audio, () => assert.fail('unexpected failure'));
  await alert.prime();
  for (const beep of [1, 1, 2, 3, 2]) await alert.play({ local_incident: 1, beep });
  await alert.play({ local_incident: 2, beep: 1 }); await alert.play({ local_incident: 1, beep: 4 });
  assert.deepEqual(audio.events, [`play@${ALERT_VOLUME}`, 'pause', ...Array(4).fill(`play@${ALERT_VOLUME}`)]);
  alert.close(); assert.equal(audio.events.at(-1), 'pause');
});

test('muted or denied audio is reported once per focus loss however often it beeps', async () => {
  const failures = [];
  const alert = new UnfocusAlert({ currentTime: 0, play: async () => { throw Error('denied'); }, pause: () => {} }, sequence => failures.push(sequence));
  for (const beep of [1, 2, 3]) await alert.play({ local_incident: 8, beep });
  await alert.play({ local_incident: 9, beep: 1 });
  assert.deepEqual(failures, [8, 9]);
});

test('failure descriptions name the cause and never blame permissions for unrelated errors', () => {
  assert.match(describeSoundFailure({ name: 'NotAllowedError' }), /blocked/);
  assert.match(describeSoundFailure({ name: 'NotSupportedError' }), /NotSupportedError/);
  assert.match(describeSoundFailure({ name: 'Weird' }), /Weird/);
  assert.doesNotMatch(describeSoundFailure(undefined), /permission/i);
});
