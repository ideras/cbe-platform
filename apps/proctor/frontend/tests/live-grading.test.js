import { test } from 'node:test';
import assert from 'node:assert/strict';
import { batchProgress, gradingRows, gradeStatus } from '../src/live-grading.js';

test('a run hidden behind a different grade does not count as finished', () => {
  const ids = [11, 12, 13];
  const finished = new Map([[11, 'failed']]);
  assert.deepEqual(batchProgress(ids, new Map([[5, 'done'], [12, 'queued']]), finished),
    { done: 0, failed: 1, queued: 1, running: 0, unseen: 1, total: 3, complete: false });
  assert.deepEqual(batchProgress(ids, new Map([[12, 'done'], [13, 'done'], [14, 'running']]), finished),
    { done: 2, failed: 1, queued: 0, running: 0, unseen: 0, total: 3, complete: true });
});

test('grading distinguishes submission and grade states, and orders active runs before other students', () => {
  const student = (session_id, student_name) => ({ session_id, student_name, account_number: session_id });
  const workspace = (state, confirmed = false) => ({ submissions: 1, grade: state ? { state, confirmed } : null });
  assert.equal(gradeStatus(workspace(null)), 'ungraded');
  assert.equal(gradeStatus(workspace('done')), 'advisory');
  assert.equal(gradeStatus(workspace('done', true)), 'confirmed');
  assert.equal(gradeStatus({ submissions: 0 }), 'none');
  const rows = gradingRows([
    student('a', 'Alan'), student('b', 'Barbara'), student('c', 'Carol'), student('d', 'Dennis'), student('e', 'Eve'),
  ], [{ student_name: 'Frank', account_number: 'f' }], new Map([
    ['a', workspace('queued')], ['b', workspace('done', true)], ['c', workspace('running')], ['d', workspace('queued')], ['e', workspace('failed')],
  ]));
  assert.deepEqual(rows.map(row => [row.student.account_number, row.status]),
    [['c', 'running'], ['a', 'queued'], ['d', 'queued'], ['b', 'confirmed'], ['e', 'failed'], ['f', 'none']]);
});
