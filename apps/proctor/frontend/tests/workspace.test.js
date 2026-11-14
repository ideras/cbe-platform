import { test } from 'node:test';
import assert from 'node:assert/strict';
import { workspaceBadge, activityText, todoText, useText, kindLabel, endText, sizeText, treeRows, parseCast, playbackPlan, numberedLines, plainText } from '../src/workspace.js';

test('the roster says what the container is doing in words', () => {
  assert.deepEqual(workspaceBadge({ state: 'ready' }), { text: 'Running', tone: 'active' });
  assert.deepEqual(workspaceBadge({ state: 'frozen' }), { text: 'Frozen', tone: 'ok' });
  assert.equal(workspaceBadge({ state: 'failed' }).tone, 'lock');
  assert.equal(workspaceBadge({ state: 'strange' }).text, 'strange');
});

test('activity is told as an age, and the TODO progress as a count', () => {
  const now = '2035-01-01T00:10:00Z';
  assert.equal(activityText('2035-01-01T00:09:48Z', now), '12 s ago');
  assert.equal(activityText('2035-01-01T00:07:00Z', now), '3 min ago');
  assert.equal(activityText('2034-12-31T22:55:00Z', now), '1 h 15 min ago');
  assert.equal(activityText('2035-01-01T00:10:05Z', now), '0 s ago');
  assert.equal(activityText(null, now), 'no activity yet');
  assert.equal(activityText('garbage', now), 'unknown');
  assert.equal(todoText({ todo_done: 2, todo_total: 3 }), '2 of 3 done');
  assert.equal(useText({ cpu_percent: 12.5, memory_mb: 180 }), '12.5% CPU · 180 MB');
  assert.equal(useText({ cpu_percent: 103.4, memory_mb: null }), '103.4% CPU');
  assert.equal(useText({ cpu_percent: 0, memory_mb: 5 }), '0% CPU · 5 MB');
  assert.equal(useText({}), 'not measured');
});

test('terminals and sizes are named', () => {
  assert.equal(kindLabel('run'), 'Run');
  assert.equal(kindLabel('x'), 'x');
  assert.equal(endText({ ended_at: null }), 'still open');
  assert.equal(endText({ ended_at: 'x', exit_code: 3 }), 'ended with code 3');
  assert.equal(endText({ ended_at: 'x', exit_code: null }), 'closed');
  assert.equal(sizeText(10), '10 B');
  assert.equal(sizeText(1536), '1.5 KiB');
  assert.equal(sizeText(3 << 20), '3.0 MiB');
});

test('the tree shows open folders, folders first', () => {
  const entries = [{ path: 'src', type: 'dir' }, { path: 'src/b.c', type: 'file', size: 4 }, { path: 'src/a.c', type: 'file' }, { path: 'src/deep', type: 'dir' }, { path: 'src/deep/x.c', type: 'file' },
    { path: 'Makefile', type: 'file' }, { path: 'empty', type: 'dir' }, { path: 'link', type: 'link', link: 'src/a.c' }, { path: 'file10', type: 'file' }, { path: 'file2', type: 'file' }];
  assert.deepEqual(treeRows(entries, new Set()).map(row => row.path), ['empty', 'src', 'file2', 'file10', 'link', 'Makefile']);
  const open = treeRows(entries, new Set(['src', 'src/deep']));
  assert.deepEqual(open.map(row => `${row.level}:${row.path}`), ['1:empty', '1:src', '2:src/deep', '3:src/deep/x.c', '2:src/a.c', '2:src/b.c', '1:file2', '1:file10', '1:link', '1:Makefile']);
  assert.equal(open.find(row => row.path === 'empty').empty, true);
  assert.equal(open.find(row => row.path === 'link').link, 'src/a.c');
  assert.equal(open.find(row => row.path === 'src/b.c').size, 4);
  // A file whose folder is not listed is still shown.
  assert.deepEqual(treeRows([{ path: 'lost/a.c', type: 'file' }], new Set()).length, 0);
});

const cast = lines => lines.map(line => typeof line === 'string' ? line : JSON.stringify(line)).join('\n') + '\n';
const head = { version: 2, width: 100, height: 30, timestamp: 5, title: 'Run' };

test('a transcript is read, its input events skipped and its output kept in order', () => {
  const read = parseCast(cast([head, [0.5, 'o', 'hello '], [0.7, 'i', 'k'], [1.5, 'o', 'world\r\n']]));
  assert.deepEqual(read.header, { width: 100, height: 30, title: 'Run', timestamp: 5 });
  assert.deepEqual(read.events, [{ at: 0.5, text: 'hello ' }, { at: 1.5, text: 'world\r\n' }]);
  assert.equal(read.duration, 1.5);
  assert.deepEqual(parseCast(cast([head])).events, []);
});

test('a text that is not a transcript is refused, with the line', () => {
  for (const [name, text, message] of [['nothing', '', /no header/], ['no header', cast([[0.5, 'o', 'x']]), /not an asciinema v2/], ['version 1', cast([{ version: 1, width: 80, height: 24 }]), /not an asciinema v2/],
    ['no size', cast([{ version: 2 }]), /not an asciinema v2/], ['not an event', cast([head, 'nope']), /Line 2 .*not an event/], ['short', cast([head, [0.1, 'o']]), /Line 2/],
    ['time back', cast([head, [1, 'o', 'a'], [0.5, 'o', 'b']]), /Line 3 .*time order/], ['text that is not text', cast([head, [0.1, 'o', 7]]), /Line 2/]]) {
    assert.throws(() => parseCast(text), message, name);
  }
});

test('a replay never waits long for a student who was thinking', () => {
  const plan = playbackPlan([{ at: 0.1, text: 'a' }, { at: 0.6, text: 'b' }, { at: 600, text: 'c' }, { at: 600.2, text: 'd' }]);
  assert.deepEqual(plan.map(step => Math.round(step.delay * 1000) / 1000), [0.1, 0.5, 2, 0.2]);
  assert.deepEqual(plan.map(step => step.text), ['a', 'b', 'c', 'd']);
  assert.deepEqual(playbackPlan([{ at: 10, text: 'x' }], 1).map(step => step.delay), [1]);
  assert.deepEqual(playbackPlan([]), []);
});

test('a file is shown line by line, with its text unchanged', () => {
  assert.deepEqual(numberedLines('a\nb\n'), ['a', 'b']);
  assert.deepEqual(numberedLines('a\n\n'), ['a', '']);
  assert.deepEqual(numberedLines(''), ['']);
  assert.deepEqual(numberedLines('<script>x</script>'), ['<script>x</script>']);
});

test('a transcript as plain text has no control sequences in it', () => {
  const events = [{ at: 0, text: '\x1b[1;32mstudent@workspace\x1b[0m:\x1b[34m/work\x1b[0m$ ls\r\n' }, { at: 1, text: '\x1b]0;title\x07Makefile  src\r\n\x1b[?2004h' }, { at: 2, text: 'caf\u00e9 \u4e16\u754c\r\n' }];
  assert.equal(plainText(events), 'student@workspace:/work$ ls\nMakefile  src\ncaf\u00e9 \u4e16\u754c\n');
  assert.equal(plainText([]), '');
});
