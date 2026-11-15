// What the proctor's windows show of a workspace exam (docs/workspace/student-ide.md): the badges of the roster, the tree of a
// snapshot, and the transcripts of the terminals. The students' files and what their programs print are untrusted text: nothing here
// ever turns them into markup.

const states = {
  ready: { text: 'Running', tone: 'active' }, provisioning: { text: 'Starting', tone: 'neutral' }, new: { text: 'Starting', tone: 'neutral' },
  frozen: { text: 'Frozen', tone: 'ok' }, removed: { text: 'Removed', tone: 'neutral' }, failed: { text: 'Failed', tone: 'lock' },
};

/** The container's state as the roster says it. */
export function workspaceBadge(student) {
  return states[student.state] ?? { text: student.state, tone: 'neutral' };
}

/** "12 s ago", "3 min ago", "1 h 5 min ago", or "no activity yet". */
export function activityText(at, now) {
  if (!at) return 'no activity yet';
  const seconds = Math.max(0, Math.round((Date.parse(now) - Date.parse(at)) / 1000));
  if (Number.isNaN(seconds)) return 'unknown';
  if (seconds < 60) return `${seconds} s ago`;
  const minutes = Math.floor(seconds / 60);
  return minutes < 60 ? `${minutes} min ago` : `${Math.floor(minutes / 60)} h ${minutes % 60} min ago`;
}

/** The TODO progress, always with whose word it is. */
export function todoText(student) {
  return `${student.todo_done} of ${student.todo_total} done`;
}

export const todoNote = 'self-reported';

/** "12.5% CPU · 180 MB", or what is known of it. */
export function useText(student) {
  const parts = [];
  if (student.cpu_percent != null) parts.push(`${Math.round(student.cpu_percent * 10) / 10}% CPU`);
  if (student.memory_mb != null) parts.push(`${student.memory_mb} MB`);
  return parts.length ? parts.join(' · ') : 'not measured';
}

const kinds = { shell: 'Terminal', run: 'Run', check: 'Check', program: 'Program' };
export const kindLabel = kind => kinds[kind] ?? kind;

/** "ended with code 3", "still open", "ended (how is unknown)". */
export function endText(terminal) {
  if (!terminal.ended_at) return 'still open';
  return terminal.exit_code == null ? 'closed' : `ended with code ${terminal.exit_code}`;
}

export const sizeText = bytes => bytes < 1024 ? `${bytes} B` : bytes < 1 << 20 ? `${(bytes / 1024).toFixed(1)} KiB` : `${(bytes / (1 << 20)).toFixed(1)} MiB`;

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/** The rows of the tree for the folders that are open: folders first, then names in the order a person expects. A file whose folder
 *  is not in the list is shown anyway, as if the folder were there. */
export function treeRows(entries, expanded) {
  const children = new Map([['', []]]);
  const kind = new Map();
  for (const entry of entries) {
    kind.set(entry.path, entry.type);
    const parent = entry.path.includes('/') ? entry.path.slice(0, entry.path.lastIndexOf('/')) : '';
    if (!children.has(parent)) children.set(parent, []);
    children.get(parent).push(entry);
    if (entry.type === 'dir' && !children.has(entry.path)) children.set(entry.path, []);
  }
  const rows = [];
  const visit = (folder, level) => {
    const list = [...(children.get(folder) ?? [])].sort((a, b) => (a.type === 'dir') === (b.type === 'dir') ? collator.compare(a.path, b.path) : a.type === 'dir' ? -1 : 1);
    for (const entry of list) {
      const name = entry.path.slice(entry.path.lastIndexOf('/') + 1);
      const open = entry.type === 'dir' && expanded.has(entry.path);
      rows.push({ path: entry.path, name, level, type: entry.type, size: entry.size ?? 0, link: entry.link ?? '', open, empty: entry.type === 'dir' && !(children.get(entry.path)?.length) });
      if (open) visit(entry.path, level + 1);
    }
  };
  visit('', 1);
  return rows;
}

/** Reads an asciinema v2 transcript: its header and its output events in time order. It refuses a text that is not one. */
export function parseCast(text) {
  const lines = String(text).split('\n').filter(line => line !== '');
  let header;
  try { header = JSON.parse(lines[0]); } catch { throw new Error('The transcript has no header.'); }
  if (header?.version !== 2 || !(header.width >= 1) || !(header.height >= 1)) throw new Error('The transcript is not an asciinema v2 file.');
  const events = [];
  let last = 0;
  for (const [index, line] of lines.slice(1).entries()) {
    let event;
    try { event = JSON.parse(line); } catch { throw new Error(`Line ${index + 2} of the transcript is not an event.`); }
    if (!Array.isArray(event) || event.length !== 3 || typeof event[0] !== 'number' || typeof event[1] !== 'string' || typeof event[2] !== 'string' || event[0] < last) {
      throw new Error(`Line ${index + 2} of the transcript is not an event in time order.`);
    }
    last = event[0];
    if (event[1] === 'o') events.push({ at: event[0], text: event[2] });
  }
  return { header: { width: header.width, height: header.height, title: header.title ?? '', timestamp: header.timestamp ?? 0 }, events, duration: last };
}

/** The pauses of a replay: the time between two events as it was, but never more than maxGap seconds, so a student who sat thinking for
 *  ten minutes does not make the proctor wait ten minutes. */
export function playbackPlan(events, maxGap = 2) {
  let previous = 0;
  return events.map(event => {
    const gap = Math.min(Math.max(0, event.at - previous), maxGap);
    previous = event.at;
    return { delay: gap, text: event.text };
  });
}

/** A file's text with the line numbers a reader looks for, as lines; the text itself is never changed. */
export function numberedLines(content) {
  const lines = content.split('\n');
  if (lines.length > 1 && lines.at(-1) === '') lines.pop();
  return lines;
}

/** What a transcript says without the terminal's control sequences: colors, cursor moves and the like are taken out, and what
 *  is left is the text a person reads (for a screen reader, and for a copy). Carriage returns before a line feed are dropped. */
export function plainText(events) {
  // eslint-disable-next-line no-control-regex
  return events.map(event => event.text).join('').replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, '').replace(/\x1b\[[0-9;?<=>]*[ -/]*[@-~]/g, '').replace(/\x1b[()][0-9A-Za-z]/g, '').replace(/\r\n/g, '\n');
}

// ---- Grading (Phase 6) ----------------------------------------------------------------------------------------------------------
// A grade is the result of the exam's hidden tests run on a submission. It is advisory until the proctor confirms it, and the windows
// say so wherever a grade shows. What a grade program printed is untrusted text, shown as text.

export const gradeNote = 'advisory until you confirm it';

const failureWords = {
  malformed: 'its output was not what a grade program must print',
  timeout: 'it did not end in time',
  crash: 'it crashed, or ended with an error',
  oom: 'it ran out of memory',
  runner: 'the runner could not run it',
  interrupted: 'the server stopped before it ended',
};
export const failureLabel = code => failureWords[code] ?? 'it failed';

/** "16 of 40 points", "16 points" when the TODOs have no points. */
export function pointsText(points, max) {
  const shown = value => (Math.round(value * 100) / 100).toString();
  if (points == null) return 'no points';
  return max == null ? `${shown(points)} points` : `${shown(points)} of ${shown(max)} points`;
}

/** What a grade (the roster's, or a run's) says in words: { text, note, tone }. */
export function gradeLine(grade) {
  if (!grade) return null;
  switch (grade.state) {
    case 'queued': return { text: 'Waiting to be graded', note: '', tone: 'neutral' };
    case 'running': return { text: 'Being graded…', note: '', tone: 'active' };
    case 'failed': return { text: `Grading failed: ${failureLabel(grade.failure)}`, note: '', tone: 'lock' };
    default: return { text: pointsText(grade.points, grade.max_points), note: grade.confirmed ? 'confirmed' : gradeNote, tone: grade.confirmed ? 'ok' : 'neutral' };
  }
}

/** What the roster knows of the class's grading, for the line above the table and for what the "Grade" button will do. */
export function gradingSummary(students) {
  const out = { waiting: 0, running: 0, done: 0, confirmed: 0, failed: 0, ungraded: 0, submitters: 0, gradable: 0 };
  for (const student of students ?? []) {
    const grade = student.grade;
    if (student.submissions > 0) out.submitters += 1;
    if (!grade) { if (student.submissions > 0) out.ungraded += 1; }
    else if (grade.state === 'queued') out.waiting += 1;
    else if (grade.state === 'running') out.running += 1;
    else if (grade.state === 'failed') out.failed += 1;
    else { out.done += 1; if (grade.confirmed) out.confirmed += 1; }
    // The students a request to grade the class would queue: those who submitted and are not being graded.
    if (student.submissions > 0 && !(grade && (grade.state === 'queued' || grade.state === 'running'))) out.gradable += 1;
  }
  return out;
}

// An exam's own image (docs/workspace/images.md): what the exam list and the image dialog say of it. The names are the server's states.
const imageStates = {
  not_built: { text: 'Image not built', tone: 'warn', title: 'This exam has its own image. Build it before you publish the exam.' },
  queued: { text: 'Build waiting', tone: 'active', title: 'The build of this exam’s image is waiting for its turn.' },
  building: { text: 'Building the image…', tone: 'active', title: 'The image is being built.' },
  ready: { text: 'Image ready', tone: 'ok', title: 'The exam’s own image is built and verified.' },
  failed: { text: 'Build failed', tone: 'lock', title: 'The last build of this exam’s image failed.' },
  missing: { text: 'Image missing', tone: 'lock', title: 'The runner does not have this exam’s image any more. Build it again.' },
};

/** What to say of an exam's own image in the exam list; null for an exam that has none (a question exam, or a package with no image folder). */
export function imageChip(image) {
  if (!image || image.state === 'none') return null;
  const chip = imageStates[image.state] ?? { text: image.state, tone: 'neutral', title: '' };
  // A failed build does not take away an image that was built before it.
  if (image.state === 'failed' && image.ready) return { ...chip, text: 'Last build failed', tone: 'warn', title: 'The last build failed, but an earlier build of the same files is still the exam’s image.' };
  return chip;
}

/** Why an exam cannot be published as far as its image goes ("" when it can). The server checks again when asked to publish. */
export function publishBlock(entry) {
  const image = entry?.image;
  if (!image || image.state === 'none' || entry.kind !== 'workspace') return '';
  if (image.state === 'queued' || image.state === 'building') return 'The exam’s image is being built: wait for the build to end before publishing.';
  if (!image.ready) return image.state === 'failed' ? 'The last build of the exam’s image failed: fix the Containerfile and build it again before publishing.' : 'Build the exam’s image before publishing.';
  return '';
}

/** "sha256:baac8cde6b90…" for a digest that is too long to read. */
export function shortDigest(digest) {
  if (!digest) return '';
  const [kind, rest] = digest.includes(':') ? digest.split(':', 2) : ['', digest];
  return `${kind ? kind + ':' : ''}${rest.slice(0, 12)}${rest.length > 12 ? '…' : ''}`;
}

const buildFailures = {
  containerfile: 'The Containerfile is not accepted', base: 'The course image on this server is not the one the file names', build: 'A step of the build failed',
  timeout: 'The build took too long', verify: 'The image does not keep the course image’s contract', runner: 'The runner could not build it', interrupted: 'The server stopped during the build',
};
export const buildFailureLabel = code => buildFailures[code] ?? 'The build failed';

/** "1:05" or "12 s": how long a build has been going or took. */
export function buildTime(ms) {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds === 0 && ms > 0) return '< 1 s';
  return seconds < 60 ? `${seconds} s` : `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}

/** What one build in the list says, in a few words. */
export function buildLine(build) {
  if (!build) return '';
  const states = { queued: 'waiting', running: 'building…', ok: build.cached ? 'ready (already built)' : 'ready', failed: 'failed' };
  return `#${build.build_id} · ${states[build.state] ?? build.state}${build.rebuild ? ' · from scratch' : ''}`;
}
