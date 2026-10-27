const EXAM = '11111111-1111-4111-8111-111111111111';
const PRACTICE = '33333333-3333-4333-8333-333333333333';
const DRAFT = '77777777-7777-4777-8777-777777777777';
const SESSION = '22222222-2222-4222-8222-222222222222';
const MATH = '44444444-4444-4444-8444-444444444444';
const HISTORY = '55555555-5555-4555-8555-555555555555';
const AT = '2035-01-01T00:00:00Z';
const PNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aI0sAAAAASUVORK5CYII=';
let observation = 0;
let connected = false;
let selectedExam = '';
let publishedExam = EXAM;
let connecting = false;
let connectError = '';
let connectFails = '';
let heldConnect = null;
let releaseConnect;
let unreachable = false;
let knownServers = ['http://127.0.0.1:8080'];
let classes = [
  { class_id: MATH, name: 'Mathematics 10A', student_count: 28, created_at: '2035-01-01T00:00:00Z' },
  { class_id: HISTORY, name: 'History 11B', student_count: 24, created_at: '2035-01-02T00:00:00Z' },
];
let rosterOverride = {};
let rosterFails = '';
let deferRoster = false;
let releaseRoster;
let nextClassFile = null;
let createFails = '';
let renameFails = '';
let deleteFails = '';
let replaceFails = '';
let nextRosterChange = null;
let pruned = 2;
let workingStudents = 1;
let nextExamFile = null;
let launchCredential = true;
let rejected = false;
let notice = '';
let credentialFile = '';
let nextTokenFile = null;
let tokenFileFails = '';
let importFails = false;
let publishFails = '';
let deleteExamFails = '';
let catalogDown = false;
// The proctor's Settings: the monitoring values stored on the server.
let settings = { heartbeat_interval_ms: 15000, overdue_threshold_ms: 30000, focus_threshold_ms: 5000, alert_interval_ms: 2000,
  capture_interval_ms: 5000, lock_away_after_ms: 300000, lock_no_contact_after_ms: 300000, incident_captures: 60, session_captures: 200,
  default_grace_ms: 30000, updated_at: '2035-01-01T00:00:00Z', average_screenshot_bytes: 524288 };
let settingsFails = false;
let settingsStale = false;
// Downloading submissions: the Go side counts the sessions and writes the files; these stand in for it.
let downloadPreviewOverride = {};
let downloadNeedsReplace = false;
let downloadFails = '';
let downloadFailureFor = null;
let openFolderFails = '';
let downloadFolder = '/home/proctor/Downloads';
let chooseFolderFails = '';
let downloadHeld = false;
let releaseDownload = null;
let lastDownloadProgress = { running: false, done: 0, total: 0, current: '' };
// An exam is imported into a class and stays there: all three belong to Mathematics 10A, and History 11B has none.
let exams = [
  { exam_id: EXAM, class_id: MATH, title: 'Databases midterm', exercise_count: 12, navigation: 'free', duration_seconds: 5400, grace_seconds: 30, min_volume_percent: 80, created_at: '2035-01-01T00:00:00Z', opens_at: null, closes_at: null, total_sessions: 90 },
  { exam_id: PRACTICE, class_id: MATH, title: 'Practice exam', exercise_count: 3, navigation: 'forward_only', duration_seconds: 600, grace_seconds: 30, min_volume_percent: 0, created_at: '2034-12-01T00:00:00Z', opens_at: null, closes_at: '2034-12-02T00:00:00Z', total_sessions: 14 },
  { exam_id: DRAFT, class_id: MATH, title: 'Draft quiz', exercise_count: 2, navigation: 'free', duration_seconds: 900, grace_seconds: 30, min_volume_percent: 80, created_at: '2034-11-01T00:00:00Z', opens_at: null, closes_at: null, total_sessions: 0, tools: { version: 1, calculator: { enabled: true, modes: ['scientific', 'programmer'], angle_mode: 'rad', disabled_functions: ['ieee754'] } } },
];
let stale = false;
let imageMissing = window.__fakeImagesMissing === true; // set before the page loads, for thumbnails that load at once
let saveCancelled = false;
let failForce = false;
let deferForce = false;
let releaseForce;
let deferHistory = false;
let releaseHistory;
let forceStatus = 'force_submitted';
let partialPurge = false;
let rows = Array.from({ length: 90 }, (_, i) => ({
  session_id: i === 0 ? SESSION : `${String(i).padStart(8, '0')}-0000-4000-8000-000000000000`, exam_id: EXAM,
  student_name: `Student ${String(i).padStart(3, '0')}`, account_number: String(i + 1).padStart(4, '0'), roster_active: true,
  status: 'active', persisted_status: 'active', seconds_remaining: 89, finalization_pending: false, final_version: null,
  network_warning: i === 0, focus_warning: i === 0,
  presence: { hostname: `desk-${i}`, remote_addr: `10.0.0.${i + 1}`, connectivity: i === 0 ? 'overdue' : 'healthy', heartbeat_age_ms: i === 0 ? 35000 : 1000, missed_intervals: i === 0 ? 2 : 0, ever_overdue: i < 2, monitoring: true, volume_percent: i === 2 ? null : i === 0 ? 35 : 70, volume_muted: i === 2 ? null : i === 0, focus: { state: i === 0 ? 'unknown' : 'focused', unfocused_duration_ms: null } }, last_known_unfocused_duration_ms: i === 0 ? 29000 : null,
}));
let missing = Array.from({ length: 30 }, (_, i) => ({ student_name: `Missing ${String(i).padStart(3, '0')}`, account_number: `M${i}` }));
const final = { final_version_id: 12, version_seq: 3, reason: 'explicit_submit', submitted_at: AT };
// The new fakes (the preview dialog and its calculator) record their native method names, so a test can
// assert what an action called.
const nativeCalls = [];
const record = name => nativeCalls.push(name);
const calls = { force: [], purge: [], image: [], timeline: [], select: [], connect: [], windows: [], saved: [], chooseCredential: 0, chooseExam: 0, chosenFor: [], importExam: 0, imported: [], discardImport: 0, deleteExam: [], chooseClassFile: 0, createClass: [], discardClassFile: 0, renameClass: [], deleteClass: [], chooseRosterFile: [], replaceRoster: [], discardRosterFile: 0, disconnect: 0, classRoster: [], publish: [], unpublish: [], download: [], prepareDownload: [], chooseDownloadFolder: [], openDownloadFolder: 0, quit: 0 };
const FAMOUS = ['Ada Lovelace', 'Grace Hopper', 'Alan Turing', 'Edsger Dijkstra'];
function rosterOf(id) {
  if (rosterOverride[id]) return rosterOverride[id];
  const entry = classes.find(item => item.class_id === id);
  const shown = Math.min(entry.student_count, 300);
  return { class_id: id, name: entry.name, total: entry.student_count, students: Array.from({ length: shown }, (_, i) => ({ account_number: String(i + 1).padStart(4, '0'), student_name: i < FAMOUS.length ? FAMOUS[i] : `Student ${String(i + 1).padStart(3, '0')}` })) };
}
const className = id => classes.find(item => item.class_id === id)?.name ?? '';
const publishedEntry = () => exams.find(item => item.exam_id === publishedExam);
function classList() {
  return classes.map(entry => {
    const own = exams.filter(item => item.class_id === entry.class_id);
    const live = own.find(item => item.exam_id === publishedExam);
    return { ...entry, exam_count: own.length, published_exam: live ? { exam_id: live.exam_id, title: live.title } : null };
  });
}
function catalog() {
  const live = publishedEntry();
  return { server_time: AT, current_exam_id: publishedExam, current_class_id: live ? live.class_id : '', current_class_name: live ? className(live.class_id) : '',
    exams: exams.map(entry => ({ ...entry, class_name: className(entry.class_id), published: entry.exam_id === publishedExam, active_sessions: entry.exam_id === publishedExam ? workingStudents : 0, locked_sessions: entry.exam_id === publishedExam ? rows.filter(row => row.locked).length : 0 })) };
}
function snapshot() {
  const loaded = connected && !rejected && selectedExam !== '';
  const ok = connected && !rejected;
  return { observation: ++observation, server: 'http://127.0.0.1:8080', known_servers: knownServers, exam_id: selectedExam, credential_available: launchCredential || credentialFile !== '', credential_file: credentialFile, notice,
    connected, connecting, connect_error: connectError, unreachable: connected && unreachable, loaded, loading: false, busy: false, stale: stale || (connected && rejected) || (connected && unreachable),
    error: rejected ? 'Proctor authorization was rejected. Reconnect with the correct credential.' : catalogDown ? 'Invalid exam catalog; previous observations retained.' : stale ? 'Server unreachable. Previous observations retained.' : '', last_success: connected ? AT : null,
    catalog: ok && !catalogDown ? catalog() : { server_time: AT, current_exam_id: '', current_class_id: '', current_class_name: '', exams: [] }, catalog_loaded: ok && !catalogDown,
    classes: ok ? classList() : [], classes_loaded: ok,
    roster: { exam_id: selectedExam, server_time: AT, sessions: loaded ? structuredClone(rows) : [], students_without_session: loaded ? structuredClone(missing) : [],
      policy: { heartbeat_interval_ms: 15000, focus_threshold_ms: 5000, overdue_threshold_ms: 30000, alert_interval_ms: 2000, capture_interval_ms: 5000, lock_away_after_ms: 300000, lock_no_contact_after_ms: 300000 } } };
}
export async function Snapshot() { return snapshot(); }
export async function Connect(server) {
  calls.connect.push({ server });
  if (!launchCredential && credentialFile === '') throw new Error('Choose the proctor token file first, or launch with an admin-token file.');
  connecting = true; connectError = '';
  if (heldConnect) await new Promise(resolve => releaseConnect = resolve);
  connecting = false;
  if (connectFails) { connectError = connectFails; throw new Error(connectFails); }
  selectedExam = publishedExam; connected = true; return snapshot();
}
export async function ChooseCredential() {
  calls.chooseCredential++;
  if (tokenFileFails) throw new Error(tokenFileFails);
  if (!nextTokenFile) return { cancelled: true, file: '' };
  credentialFile = nextTokenFile;
  return { cancelled: false, file: nextTokenFile };
}
export async function SelectExam(id) { calls.select.push(id); selectedExam = id; return snapshot(); }
// A compiled exam as the dashboard's Go side returns it; the markup it holds must stay inert in the preview.
const sampleContent = { instructions: '<p>Read <strong>everything</strong>.</p><img src=x onerror="window.__unsafe=true">', exercises: [
  { id: '1', title: 'Ejercicio 1 — Tabla', points: 10, items: [{ id: '1a', title: 'a. Conteo', points: 10, body: '<table><tbody><tr><td>ALU</td><td><span class="slot" data-slot="1a.alu"></span></td></tr></tbody></table>', slots: [{ id: '1a.alu', kind: 'blank' }] }] },
  { id: '2', title: 'Ejercicio 2 — Código', body: '<p>Translate.</p><div class="slot" data-slot="2.code"></div>', slots: [{ id: '2.code', kind: 'code', language: 'mips', starter: 'main:\n' }] },
] };
export async function ChooseExam(classId) {
  calls.chooseExam++; calls.chosenFor.push(classId);
  if (!nextExamFile) return { cancelled: true, file: '', valid: false, problems: [] };
  return { cancelled: false, problems: [], ...nextExamFile };
}
export async function ImportExam(classId, pin, forwardOnly) {
  calls.importExam++; calls.imported.push({ classId, pin, forwardOnly });
  if (importFails) throw new Error('The server rejected the document: title is required.');
  const file = nextExamFile; nextExamFile = null;
  exams = [{ exam_id: '55555555-5555-4555-8555-555555555555', class_id: classId, title: file.title, exercise_count: file.exercise_count, navigation: forwardOnly ? 'forward_only' : 'free', duration_seconds: file.duration_seconds, grace_seconds: file.grace_seconds, min_volume_percent: file.min_volume_percent, created_at: AT, opens_at: file.opens_at ?? null, closes_at: file.closes_at ?? null, total_sessions: 0 }, ...exams];
  return { exam_id: '55555555-5555-4555-8555-555555555555' };
}
export async function DiscardImport() { calls.discardImport++; nextExamFile = null; }
export async function ChooseClassFile() {
  calls.chooseClassFile++;
  if (!nextClassFile) return { cancelled: true, file: '', suggested_name: '', valid: false, problems: [], students: 0, warning_count: 0, warnings: [] };
  return { cancelled: false, problems: [], warning_count: 0, warnings: [], suggested_name: nextClassFile.file.replace(/\.csv$/i, ''), ...nextClassFile };
}
export async function DiscardClassFile() { calls.discardClassFile++; nextClassFile = null; }
export async function CreateClass(name) {
  calls.createClass.push(name);
  if (createFails) throw new Error(createFails);
  const file = nextClassFile; nextClassFile = null;
  const entry = { class_id: '66666666-6666-4666-8666-666666666666', name, student_count: file.students, created_at: AT };
  classes = [...classes, entry];
  return { ...entry, exam_count: 0, published_exam: null };
}
export async function ClassRoster(id) {
  calls.classRoster.push(id);
  if (deferRoster) await new Promise(resolve => releaseRoster = resolve);
  if (rosterFails) throw new Error(rosterFails);
  return rosterOf(id);
}
export async function RenameClass(id, name) {
  calls.renameClass.push({ id, name });
  if (renameFails) throw new Error(renameFails);
  const entry = classes.find(item => item.class_id === id);
  entry.name = name;
  return classList().find(item => item.class_id === id);
}
export async function DeleteClass(id, confirmed) {
  calls.deleteClass.push({ id, confirmed });
  const entry = classes.find(item => item.class_id === id);
  if (deleteFails) throw new Error(deleteFails);
  if (exams.some(item => item.class_id === id)) throw new Error('The class still has exams. Delete its exams first. Exams students have taken cannot be deleted.');
  classes = classes.filter(item => item.class_id !== id);
  return { class_id: id, name: entry.name, students: entry.student_count, pruned };
}
export async function ChooseRosterFile(id) {
  calls.chooseRosterFile.push(id);
  if (!nextRosterChange) return { cancelled: true, file: '', valid: false, problems: [], students: 0, added: 0, removed: 0, warning_count: 0, warnings: [], applied: false, admitting: false, pruned: 0 };
  return { cancelled: false, problems: [], warning_count: 0, warnings: [], applied: false, admitting: publishedEntry()?.class_id === id, pruned: 0, added: 0, removed: 0, ...nextRosterChange };
}
export async function DiscardRosterFile() { calls.discardRosterFile++; nextRosterChange = null; }
export async function ReplaceClassRoster(id, confirmed) {
  calls.replaceRoster.push({ id, confirmed });
  if (replaceFails) throw new Error(replaceFails);
  const file = nextRosterChange; nextRosterChange = null;
  const entry = classes.find(item => item.class_id === id);
  entry.student_count = file.students; delete rosterOverride[id];
  return { students: file.students, added: file.added ?? 0, removed: file.removed ?? 0, warning_count: 0, warnings: [], applied: true, admitting: false, pruned: file.pruned ?? 0 };
}
export async function Disconnect() {
  calls.disconnect++;
  connected = false; connecting = false; selectedExam = '';
  return snapshot();
}
export async function Quit() { calls.quit++; }
export async function Publish(examId, expectedExam, allowActive, confirmed) {
  calls.publish.push({ examId, expectedExam, allowActive, confirmed });
  if (publishFails) throw new Error(publishFails);
  if (workingStudents > 0 && expectedExam && !allowActive) throw new Error('The server refused the change: students are still working in the current exam.');
  const replaced = publishedEntry();
  const target = exams.find(item => item.exam_id === examId);
  publishedExam = examId; selectedExam = examId;
  return { exam_id: examId, class_id: target.class_id, replaced_exam_id: replaced?.exam_id ?? '', replaced_class_id: replaced?.class_id ?? '', active_sessions: allowActive ? workingStudents : 0 };
}
export async function Unpublish(expectedExam, allowActive, confirmed) {
  calls.unpublish.push({ expectedExam, allowActive, confirmed });
  const replaced = publishedEntry(); publishedExam = '';
  return { exam_id: '', class_id: '', replaced_exam_id: replaced?.exam_id ?? '', replaced_class_id: replaced?.class_id ?? '', active_sessions: allowActive ? workingStudents : 0 };
}
export async function DeleteExam(examId, confirmed) {
  calls.deleteExam.push({ examId, confirmed });
  if (deleteExamFails) throw new Error(deleteExamFails);
  const entry = exams.find(item => item.exam_id === examId);
  if (examId === publishedExam || entry.total_sessions > 0) throw new Error('The exam is published or students have taken it, so it cannot be deleted.');
  exams = exams.filter(item => item.exam_id !== examId);
  return { exam_id: examId, title: entry.title };
}
export async function Refresh() {}
let closeListener = () => {};
export function OnCloseRequested(listener) { closeListener = listener; return () => { closeListener = () => {}; }; }
// Windows: the fake records what Go would open. Pages for the other windows are loaded directly by the tests.
export async function OpenMonitoring() { if (!selectedExam) throw new Error('Choose an exam to monitor first.'); calls.windows.push({ name: 'monitoring', url: '/#/monitoring' }); }
export async function OpenReview(id) { calls.windows.push({ name: `review-${id}`, url: `/#/review/${id}` }); }
export async function OpenImage(ids, times, index) { calls.windows.push({ name: `image-${ids[0]}`, url: `/#/image?set=${ids.join('%2C')}&at=${index}` }); }
export async function SaveImage(id) { calls.saved.push(id); if (imageMissing) throw new Error('Image missing or purged.'); return saveCancelled ? '' : `/home/proctor/screenshot-${id}.png`; }
export async function Timeline(id, cursor) {
  calls.timeline.push({ id, cursor });
  const oldExam = selectedExam;
  if (deferHistory) await new Promise(resolve => releaseHistory = resolve);
  return { server_time: AT, session: { ...structuredClone(rows[0]), exam_id: oldExam, final_version: final }, items: cursor ? [{ id: 'answer_version:12', kind: 'answer_version', occurred_at: AT, answer_version: { version_id: 12, version_seq: 3, answers: { q1: 'Final answer 🧪' }, is_final: true } }] : [
    { id: 'answer_version:1', kind: 'answer_version', occurred_at: AT, answer_version: { version_id: 1, version_seq: 1, answers: { q1: '<img src=x onerror="window.injected=true">student text' }, is_final: false } },
    { id: 'event:31', kind: 'event', occurred_at: AT, event: { event_log_id: 31, event_type: 'focus_lost', detail: { client: { source: '<script>untrusted</script>' } }, screenshots: [7, 8, 9].map(id => ({ screenshot_id: id, event_log_id: 31, captured_at: AT, width: 1, height: 1, byte_size: 68 })) } },
    { id: 'event:33', kind: 'event', occurred_at: '2035-01-01T00:02:30Z', event: { event_log_id: 33, event_type: 'focus_regained', detail: { focus_lost_event_log_id: 31, unfocused_duration_ms: 150000, threshold_exceeded: true }, screenshots: [] } },
    { id: 'event:32', kind: 'event', occurred_at: AT, event: { event_log_id: 32, event_type: 'network_gap', detail: { overdue_after: AT }, screenshots: [] } },
    { id: 'event:40', kind: 'event', occurred_at: AT, event: { event_log_id: 40, event_type: 'calc_opened', detail: { client: { client_time: AT, mode: 'scientific' } }, screenshots: [] } },
    { id: 'event:41', kind: 'event', occurred_at: AT, event: { event_log_id: 41, event_type: 'calc_evaluated', detail: { client: { client_time: AT, mode: 'scientific', expression: 'sin(30)', ok: true, display: '0.5', angle_mode: 'deg' } }, screenshots: [] } },
    { id: 'event:42', kind: 'event', occurred_at: AT, event: { event_log_id: 42, event_type: 'calc_evaluated', detail: { client: { client_time: AT, mode: 'scientific', expression: 'log(8)', ok: false, code: 'ARITY', angle_mode: 'deg' } }, screenshots: [] } },
    { id: 'event:43', kind: 'event', occurred_at: AT, event: { event_log_id: 43, event_type: 'calc_closed', detail: { client: { client_time: AT, mode: 'scientific', open_ms: 30000 } }, screenshots: [] } },
    { id: 'event:44', kind: 'event', occurred_at: AT, event: { event_log_id: 44, event_type: 'calc_tab_changed', detail: { client: { client_time: AT, from: 'keypad', to: 'ieee754' } }, screenshots: [] } },
    { id: 'event:45', kind: 'event', occurred_at: AT, event: { event_log_id: 45, event_type: 'calc_inspected', detail: { client: { client_time: AT, format: 'binary16', direction: 'decode', input: '0x3C00', ok: true, bits: '0x3C00', word_size: 32, input_base: 'dec', signed: true } }, screenshots: [] } },
    { id: 'event:46', kind: 'event', occurred_at: AT, event: { event_log_id: 46, event_type: 'calc_evaluated', detail: { client: { client_time: AT, mode: 'programmer', expression: '0xFF / 2', ok: true, display: '127', word_size: 8, input_base: 'hex', signed: false } }, screenshots: [] } },
    { id: 'event:47', kind: 'event', occurred_at: AT, event: { event_log_id: 47, event_type: 'calc_tab_changed', detail: { client: { client_time: AT, from: 'ieee754', to: 'fraction' } }, screenshots: [] } },
    { id: 'event:48', kind: 'event', occurred_at: AT, event: { event_log_id: 48, event_type: 'calc_evaluated', detail: { client: { client_time: AT, mode: 'programmer', view: 'fraction', expression: '10.625', ok: true, display: '1010.101', word_size: 32, input_base: 'dec', signed: true } }, screenshots: [] } },
  ], next_cursor: cursor ? null : 'opaque cursor +/=' };
}
export async function Image(id, thumbnail) { calls.image.push({ id, thumbnail }); if (imageMissing) throw new Error('Image missing or purged.'); return { id, data: PNG }; }
export async function ForceSubmit(id, reason, confirmed) {
  calls.force.push({ id, reason, confirmed });
  if (deferForce) await new Promise(resolve => releaseForce = resolve);
  if (failForce) throw new Error('Proctor authorization was rejected.');
  rows[0] = { ...rows[0], status: forceStatus, final_version: { ...final, reason: forceStatus === 'submitted' ? 'explicit_submit' : 'force_submit' } };
  return { status: forceStatus, final_version_id: 12, version_seq: 3, submitted_at: AT };
}
export async function Settings() { if (settingsFails) throw new Error('Proctor authorization was rejected.'); return structuredClone(settings); }
export async function SaveSettings(values, expectedUpdatedAt) {
  calls.settings = { values, expectedUpdatedAt };
  if (settingsFails) throw new Error('Proctor authorization was rejected.');
  if (settingsStale) { settingsStale = false; throw new Error('Another proctor saved the settings first. The values were reloaded; check them and save again.'); }
  settings = { ...values, updated_at: '2035-01-01T00:05:00Z', average_screenshot_bytes: settings.average_screenshot_bytes };
  return structuredClone(settings);
}
export async function Unlock(sessionID, reason, giveBackTime, confirmed) {
  calls.unlock = { sessionID, reason, giveBackTime, confirmed };
  const row = rows.find(item => item.session_id === sessionID);
  if (!row || !row.locked) throw new Error('The session is not locked.');
  const locked = row.locked;
  row.locked = null;
  return { session_id: sessionID, deadline_at: '2035-01-01T01:00:00Z', grace_until: '2035-01-01T01:00:30Z', seconds_added: giveBackTime ? 420 : 0, locked_ms: locked.since ? 420000 : 0 };
}
// The downloads of one exam. The Go side counts the sessions and writes the files; these stand in for it.
function downloadPreview(examId) {
  const entry = exams.find(item => item.exam_id === examId);
  const folderName = `${(className(entry?.class_id) || 'Class').replace(/\s+/g, '_')}-${(entry?.title || 'Exam').replace(/\s+/g, '_')}`;
  return { exam_id: examId, title: entry?.title ?? '', class_name: className(entry?.class_id), folder_name: folderName,
    finished: 28, working: 2, finishing: 0, never_logged_in: 3, source_available: true, ...downloadPreviewOverride };
}
export async function PrepareDownload(examId) {
  calls.prepareDownload.push(examId);
  return downloadPreview(examId);
}
export async function ChooseDownloadFolder() {
  calls.chooseDownloadFolder.push(downloadFolder);
  if (chooseFolderFails) throw new Error(chooseFolderFails);
  return downloadFolder;
}
export async function DownloadProgress() { return lastDownloadProgress; }
export async function DownloadSubmissions(examId, folderName, replace, confirmed) {
  calls.download.push({ examId, folderName, replace, confirmed });
  if (downloadFails) throw new Error(downloadFails);
  const preview = downloadPreview(examId);
  const folder = `${downloadFolder}/${folderName}`;
  const skipped = [];
  if (downloadNeedsReplace && !replace) {
    return { needs_confirmation: true, folder, folder_name: folderName, replace_count: 28, files: [], skipped: [], failed: [], warnings: [] };
  }
  lastDownloadProgress = { running: true, done: 0, total: preview.finished, current: 'Student 001' };
  if (downloadHeld) await new Promise(resolve => { releaseDownload = resolve; });
  const failed = downloadFailureFor ? [{ student_name: downloadFailureFor, account_number: '0099', error: 'Requested record unavailable.' }] : [];
  const written = Math.max(preview.finished - failed.length, 0);
  const files = Array.from({ length: written }, (_, i) => `Student_${String(i + 1).padStart(3, '0')}.md`);
  for (let i = 0; i < preview.working; i++) skipped.push({ student_name: `Student ${String(29 + i).padStart(3, '0')}`, account_number: String(29 + i).padStart(4, '0'), status: 'working' });
  for (let i = 0; i < preview.finishing; i++) skipped.push({ student_name: `Student ${String(40 + i).padStart(3, '0')}`, account_number: String(40 + i).padStart(4, '0'), status: 'finishing' });
  lastDownloadProgress = { running: false, done: preview.finished, total: preview.finished, current: '' };
  return { needs_confirmation: false, folder, folder_name: folderName, replace_count: 0, files, skipped, failed, warnings: [] };
}
export async function OpenDownloadFolder() { calls.openDownloadFolder++; if (openFolderFails) throw new Error(openFolderFails); }
export async function PurgeScreenshots(exam, confirmed) { calls.purge.push({ exam, confirmed }); imageMissing = true; return { exam_id: exam, deleted: 1, complete: !partialPurge, failed_screenshot_ids: partialPurge ? [7] : [] }; }
// The monitoring, review and image windows open only once the main window is connected; a page loaded as one of them
// starts connected and monitoring the published exam.
if (/^#\/(monitoring|review|image)/.test(location.hash)) { connected = true; selectedExam = publishedExam; }
window.ProctorHarness = { marker: 'PROCTOR_UI_FIXTURE', calls, nativeCalls,
  launchCredential(value) { launchCredential = value; }, rejected(value) { rejected = value; }, notice(value) { notice = value; }, tokenFile(value) { nextTokenFile = value; }, tokenFileFails(value) { tokenFileFails = value; },
  connectFails(value) { connectFails = value; }, holdConnect() { heldConnect = true; }, releaseConnect() { heldConnect = null; releaseConnect?.(); }, unreachable(value) { unreachable = value; }, knownServers(value) { knownServers = value; },
  // The application started with a server and a token known: it is connected (or still connecting, or failed) before anyone clicks.
  requestClose() { closeListener(); },
  startConnected() { connected = true; selectedExam = publishedExam; }, saveCancelled(value) { saveCancelled = value; }, startConnecting() { connecting = true; }, startFailed(message) { connectError = message; },
  nothingPublished() { publishedExam = ''; selectedExam = ''; }, noClasses() { classes = []; exams = []; publishedExam = ''; selectedExam = ''; },
  // An exam of History 11B, to publish across classes; nobody has taken it, so it can be deleted.
  historyExam() { exams = [...exams, { exam_id: '88888888-8888-4888-8888-888888888888', class_id: HISTORY, title: 'History final', exercise_count: 5, navigation: 'free', duration_seconds: 7200, grace_seconds: 30, min_volume_percent: 80, created_at: '2035-01-02T00:00:00Z', opens_at: null, closes_at: null, total_sessions: 0 }]; },
  classRoster(id, names, total = null) { const entry = classes.find(item => item.class_id === id); rosterOverride[id] = { class_id: id, name: entry.name, total: total ?? names.length, students: names.map(([account, name]) => ({ account_number: account, student_name: name })) }; },
  rosterFails(value) { rosterFails = value; }, holdRoster() { deferRoster = true; }, releaseRoster() { deferRoster = false; releaseRoster?.(); },
  classFile(value) { nextClassFile = value; }, createFails(value) { createFails = value; },
  renameFails(value) { renameFails = value; }, deleteFails(value) { deleteFails = value; }, replaceFails(value) { replaceFails = value; }, rosterChange(value) { nextRosterChange = value; }, pruned(value) { pruned = value; },
  working(value) { workingStudents = value; }, examFile(value) { nextExamFile = { content: sampleContent, ...value }; }, importFails(value) { importFails = value; }, publishFails(value) { publishFails = value; }, deleteExamFails(value) { deleteExamFails = value; }, catalogDown(value) { catalogDown = value; }, stale(value) { stale = value; }, imagesMissing(value) { imageMissing = value; },
  failForce(value) { failForce = value; }, holdForce() { deferForce = true; }, releaseForce() { releaseForce?.(); }, originalSubmit() { forceStatus = 'submitted'; }, partialPurge() { partialPurge = true; },
  downloadPreview(value) { downloadPreviewOverride = value; }, downloadNeedsReplace(value = true) { downloadNeedsReplace = value; }, downloadFails(value) { downloadFails = value; }, downloadFailure(value) { downloadFailureFor = value; }, openFolderFails(value) { openFolderFails = value; },
  downloadFolder(value) { downloadFolder = value; }, chooseFolderFails(value) { chooseFolderFails = value; }, holdDownload() { downloadHeld = true; }, releaseDownload() { downloadHeld = false; releaseDownload?.(); releaseDownload = null; }, downloadProgress(value) { lastDownloadProgress = value; },
  holdHistory() { deferHistory = true; }, releaseHistory() { releaseHistory?.(); }, untrustedName() { rows[0].student_name = '<script>student name</script>'; },
  finished(index) { rows[index] = { ...rows[index], status: 'submitted', presence: { ...rows[index].presence, monitoring: false, connectivity: 'terminal' } }; },
  // A locked student: the monitoring window shows the lock first and offers the unlock dialog.
  lock(index = 0, reason = 'away') { rows[index] = { ...rows[index], locked: { since: '2035-01-01T00:00:00Z', reason } }; },
  unlock(index = 0) { rows[index] = { ...rows[index], locked: null }; },
  settings(value) { settings = { ...settings, ...value }; },
  settingsFails(value) { settingsFails = value; },
  settingsStale() { settingsStale = true; } };

// The exam preview dialog's fakes (owner decision 2026-10-23): the previewed calculator evaluates with canned
// results; the wiring is what the tests check. PreviewExam('no-calc') previews an exam without a calculator.
let previewExam = null;
export async function PreviewExam(examID) {
  record('PreviewExam');
  previewExam = examID;
  if (examID === 'missing') throw Error('not found');
  const entry = exams.find(item => item.exam_id === examID) ?? {};
  return {
    exam_id: examID, title: entry.title ?? 'Preview exam', navigation: entry.navigation ?? 'free',
    content: structuredClone(sampleContent),
    calculator: entry.tools?.calculator?.enabled ? { ...entry.tools.calculator } : null,
  };
}
export async function DiscardPreview() { record('DiscardPreview'); previewExam = null; }
export async function CalcAllowedFunctions() {
  record('CalcAllowedFunctions');
  if (!previewExam || previewExam === 'no-calc') return [];
  return ['abs', 'acos', 'asin', 'atan', 'atan2', 'cbrt', 'ceil', 'cos', 'e', 'exp', 'factorial', 'floor', 'ln', 'log', 'log10', 'mod', 'nand', 'nor', 'pi', 'rol', 'round', 'ror', 'sin', 'sinh', 'cosh', 'tanh', 'sqrt', 'tan'].sort();
}
export async function CalcEvaluate(input, angleMode) {
  record('CalcEvaluate');
  if (!previewExam || previewExam === 'no-calc') throw Error('The calculator is not available.');
  if (input.trim() === 'log(8)') return { ok: false, code: 'ARITY' };
  const canned = { 'sin(30)': '0.5', '2+3*4': '14' }[input.trim()];
  if (canned) return { ok: true, display: canned };
  return { ok: false, code: 'SYNTAX' };
}
export async function CalcEvaluateInt(input, word, inputBase, signedMode) {
  record('CalcEvaluateInt');
  if (!previewExam || previewExam === 'no-calc') throw Error('The calculator is not available.');
  if (/^\d+$/.test(input.trim()) && BigInt(input.trim()) < (1n << BigInt(word))) {
    const bits = BigInt(input.trim());
    return { ok: true, unsigned: input.trim(),
      signed: (bits >= (1n << BigInt(word - 1)) ? bits - (1n << BigInt(word)) : bits).toString(10),
      hex: '0x' + bits.toString(16).toUpperCase().padStart(word / 4, '0'),
      octal: bits.toString(8).padStart(Math.ceil(word / 3), '0'),
      binary: bits.toString(2).padStart(word, '0'), bits: input.trim(), carry: false, overflow: false };
  }
  return { ok: false, code: 'SYNTAX' };
}
export async function CalcInspect(input, format, direction) {
  record('CalcInspect');
  if (!previewExam || previewExam === 'no-calc') throw Error('The calculator is not available.');
  const text = input.trim();
  const canned = direction === 'decode' ? { 'binary16': { '0x3C00': '0x3C00' } }[format]?.[text] : { 'binary16': { '1.0': '0x3C00' }, 'bfloat16': { '1.0': '0x3F80' } }[format]?.[text];
  if (canned) return { ok: true, format, bits: canned, sign: '0', exponent_field: '15', unbiased_exponent: '0', mantissa_field: '0', class: 'normal', value: '1', rounded: false };
  return { ok: false, code: 'SYNTAX' };
}
// The session-logging calls are the student host's; the preview has none, but the shared component may call
// them optionally, so the stand-in offers no-ops.
export async function CalcOpened() { record('CalcOpened'); }
export async function CalcClosed() { record('CalcClosed'); }
export async function CalcModeChanged() { record('CalcModeChanged'); }
export async function CalcTabChanged() { record('CalcTabChanged'); }
export async function CalcConvertFraction(input) {
  record('CalcConvertFraction');
  if (!previewExam || previewExam === 'no-calc') throw Error('The calculator is not available.');
  const canned = { '10.625': { binary: '1010.101', octal: '12.5', hex: 'A.A' } }[input.trim()];
  if (canned) return { ok: true, ...canned };
  return { ok: false, code: 'SYNTAX' };
}
