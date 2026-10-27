import { test } from 'node:test';
import assert from 'node:assert/strict';
import { AnswerQueue, saveLabel } from '../src/answer-queue.js';

test('IPC updates serialize and coalesce while preserving question identities', async () => {
  const calls = []; let release;
  const first = new Promise(resolve => release = resolve);
  const queue = new AnswerQueue(async (answers, revision) => { calls.push({ answers, revision }); if (revision === 1) await first; });
  queue.stage({ q1: 'one', q2: '' }, 1);
  queue.stage({ q1: 'one', q2: 'two' }, 2);
  queue.stage({ q1: 'new one', q2: 'two' }, 3);
  assert.equal(calls.length, 1);
  release(); await queue.flush();
  assert.deepEqual(calls, [{ answers: { q1: 'one', q2: '' }, revision: 1 }, { answers: { q1: 'new one', q2: 'two' }, revision: 3 }]);
});

test('flush resolves before submission and retries transfer errors with newest text', async () => {
  let failing = true; const calls = [];
  const queue = new AnswerQueue(async answers => { if (failing) throw Error('transfer'); calls.push(answers); });
  queue.stage({ q1: 'old' }, 1);
  await assert.rejects(queue.flush(), /transfer/);
  failing = false; queue.stage({ q1: 'latest visible' }, 2); await queue.flush();
  calls.push('submit'); assert.deepEqual(calls, [{ q1: 'latest visible' }, 'submit']);
});

test('acknowledgment cannot label newer local work saved', () => {
  const view = { acknowledged_revision: 2, save_state: 'saved', saved: { version_seq: 7, saved_at: 'server time' } };
  assert.match(saveLabel(view, 3), /Unsaved/);
  assert.match(saveLabel(view, 2), /Saved.*version 7/);
  assert.match(saveLabel({ ...view, save_error: 'failure' }, 3), /failed/);
});

test('closing stops pending transfers and prototype-like IDs remain ordinary data', async () => {
  let release; const calls = [];
  const queue = new AnswerQueue(async answers => { calls.push(answers); await new Promise(resolve => release = resolve); });
  const answers = JSON.parse('{"__proto__":"ordinary text","constructor":"also text"}');
  queue.stage(answers, 1); queue.stage({ q1: 'not sent' }, 2); queue.close(); release(); await queue.flush();
  assert.equal(calls.length, 1); assert.equal(calls[0].__proto__, 'ordinary text'); assert.equal(calls[0].constructor, 'also text');
});
