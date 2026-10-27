let alertListener = () => {};
export function OnFocusAlert(listener) { alertListener = listener; return () => alertListener = () => {}; }
export async function AcknowledgeRules() { record('AcknowledgeRules'); state.rules_accepted = true; }
let permissionChecks = 0;
let permissionCheckFailure = false;
export async function CheckCapturePermission() { record('CheckCapturePermission'); permissionChecks++; if (permissionCheckFailure) throw Error('Synthetic native bridge failure.'); return result(false); }
export async function ReportAlertFailure() { record('ReportAlertFailure'); state.alertFailure = true; }
let rulesUnavailable = false;
export async function Rules(address) { record('Rules');
  if (rulesUnavailable || !address) throw Error('The server did not answer.');
  return { alarm_millis: 5000, capture_millis: 5000, lock_away_millis: 300000, lock_no_contact_millis: 300000 };
}
const clock = '2035-01-02T03:04:05Z';
let closeCalls = 0;
export async function CloseCompletedWindow() { record('CloseCompletedWindow'); if (state.phase !== 'complete') throw Error('Session not complete.'); closeCalls++; }
// Every fake method records its name, so a test can assert that an action called no native method it must
// not (the calculator may only call the Calc* ones; the app's poll timer may call Snapshot at any time).
const nativeCalls = [];
function record(name) { nativeCalls.push(name); }
const state = {
  observation: 0, phase: 'login', server: 'http://127.0.0.1:8080', account: '',
  revision: 0, acknowledged_revision: 0, timing_stale: true, save_state: 'not_saved',
  save_error: '', status_error: '', submit_error: '', submitting: false, exercise: 0, advancing: false, advance_error: '',
  sound: { volume_percent: 35, volume_muted: false },
  capture: { enabled: true, required: true, ready: false, blocked: true, pending_reports: 0, failures: 0, state: 'idle', permission: { supported: true, granted: false, test_required: true, message: 'Screenshot verification is required.' } },
};
// The exam text carries markup the compiler never writes; the student application must show none of it working.
const unsafe = '<img src=x onerror="window.__unsafe=true"><a href="https://example.invalid/leave">leave the exam</a><script>window.__unsafe=true</script>';
const exam = { exam_id: 'exam', title: 'Fixture exam', navigation: 'free', content: {
  instructions: `<p>Read <strong>every</strong> exercise.</p>${unsafe}`,
  exercises: [
    { id: '1', title: 'Ejercicio 1 — Código', points: 10, body: `<p>Translate <code>&lt;img src=x&gt;</code>:</p><pre><code class="language-c">x = a &lt; b;</code></pre>${unsafe}<div class="slot" data-slot="q1"></div>`, slots: [{ id: 'q1', kind: 'code', language: 'mips' }] },
    { id: '2', title: 'Ejercicio 2 — Tabla', points: 5, body: '<p>Shared context for the items.</p>', items: [
      { id: '2a', title: 'a. Fill in the table', points: 3, body: '<table><thead><tr><th>Category</th><th align="center">Count</th></tr></thead><tbody><tr><td>ALU</td><td align="center"><span class="slot" data-slot="q2"></span></td></tr></tbody></table>', slots: [{ id: 'q2', kind: 'blank' }] },
      { id: '2b', title: 'b. Explain', points: 2, body: '<p>Show your work:</p><div class="slot" data-slot="q3"></div>', slots: [{ id: 'q3', kind: 'text', lines: 3 }] },
    ] },
    { id: '3', title: 'Ejercicio 3 — Último', body: '<p>Last one: <span class="slot" data-slot="q4"></span></p>', slots: [{ id: 'q4', kind: 'blank' }] },
  ] } };
let answers = { q1: 'restored work', q2: '', q3: '', q4: '' };
let captured = null;
let offline = false;
let failLogin = 0;
let failLoad = false;
let lostSubmit = false;
const status = { status: 'active', server_time: clock, deadline_at: '2035-01-02T03:05:05Z', grace_until: '2035-01-02T03:05:35Z', seconds_remaining: 60 };
const result = loaded => { state.observation += 1; return structuredClone({ ...state, ...(loaded ? { exam, answers } : {}) }); };
// The calculator (Phase 3): the fake cannot do real math, so a few results are canned and the errors the UI
// tests need are answered by pattern. examTools mirrors the exam's tools config the server would send.
let examTools = null;
let calcUnavailable = false;
let calcQueueFull = false;
let fakeAns = 0;
const calcEvaluations = [];
const calcResults = {
  '2+3*4': '14', 'sin(30)': '0.5', '1/3': '0.333333333333', '2pi': '6.28318530718', 'pi': '3.14159265359',
  'sqrt(4)': '2', 'cbrt(27)': '3', 'exp(0)': '1', 'abs(-3)': '3', 'floor(2.7)': '2', 'ceil(2.1)': '3',
  'round(2.5)': '3', 'mod(-7, 3)': '-1', 'log10(1000)': '3', 'ln(e)': '1', 'log(8, 2)': '3',
  'asin(0.5)': '30', 'acos(0.5)': '60', 'atan(1)': '45', 'atan2(1,1)': '45', 'sinh(1)': '1.17520119364',
  'cosh(1)': '1.54308063482', 'tanh(1)': '0.761594155956',
  // Neural mode (Phase 7): a matrix also comes as rows, so the panel can align its columns.
  'softmax([1000, 1000])': '[0.5, 0.5]', 'sigmoid(0)': '0.5', '[1,2]^2': '[1, 4]',
  'matmul([[1,2],[3,4]], [[5,6],[7,8]])': { display: '[[19, 22], [43, 50]]', rows: [['19', '22'], ['43', '50']] },
  'transpose([[1, 2.5, -3]])': { display: '[[1], [2.5], [-3]]', rows: [['1'], ['2.5'], ['-3']] },
};
const calcNames = {
  scientific: ['abs', 'acos', 'asin', 'atan', 'atan2', 'cbrt', 'ceil', 'cos', 'e', 'exp', 'factorial', 'floor', 'ln', 'log', 'log10', 'mod', 'pi', 'round', 'sin', 'sinh', 'cosh', 'tanh', 'sqrt', 'tan'],
  programmer: ['nand', 'nor', 'rol', 'ror'],
  neural: ['tanh', 'sigmoid', 'relu', 'leaky_relu', 'dsigmoid', 'dtanh', 'drelu', 'dleaky_relu', 'softmax', 'sum', 'mean', 'max', 'min',
    'argmax', 'len', 'norm', 'transpose', 'dot', 'matmul', 'mse', 'bce'],
};
const calcErrors = {
  'log(8)': 'ARITY', '1/0': 'DIVISION_BY_ZERO', 'mod(1, 0)': 'DIVISION_BY_ZERO',
  'sqrt(-1)': 'DOMAIN', 'tan(90)': 'DOMAIN', '171!': 'OVERFLOW', '5!!': 'SYNTAX', 'sin(30': 'SYNTAX',
  'bce(1, 1)': 'DOMAIN', 'dot([1,2], [1,2,3])': 'DIM_MISMATCH', '[1,2] * [1,2,3]': 'DIM_MISMATCH', 'log([1,2], 2)': 'TYPE_MISMATCH',
};
export async function CalcAllowedFunctions() {
  record('CalcAllowedFunctions');
  const calculator = examTools?.calculator;
  if (calcUnavailable || state.phase !== 'exam' || state.locked || !calculator?.enabled) return [];
  const disabled = calculator.disabled_functions ?? [];
  const modes = calculator.modes ?? ['scientific'];
  return Object.entries(calcNames).flatMap(([mode, names]) => modes.includes(mode) ? names : []).filter(name => !disabled.includes(name)).sort();
}
// The programmer panel's fakes: a few canned results keyed by input; the fake cannot do real integer math and
// the UI tests only need the wiring and the documented shapes.
const calcIntResults = {
  '0xFF + 1': { signed: '0', unsigned: '0', hex: '0x00', octal: '000', binary: '00000000', bits: '0', carry: true, overflow: false },
  '127 + 1': { signed: '-128', unsigned: '128', hex: '0x80', octal: '200', binary: '10000000', bits: '128', carry: false, overflow: true },
  '0 - 1': { signed: '-1', unsigned: '255', hex: '0xFF', octal: '377', binary: '11111111', bits: '255', carry: true, overflow: false },
  '-8 >> 1': { signed: '-4', unsigned: '252', hex: '0xFC', octal: '374', binary: '11111100', bits: '252', carry: false, overflow: false },
  'ff + 1': { signed: '256', unsigned: '256', hex: '0x0101', octal: '000405', binary: '0000000100000001', bits: '256', carry: false, overflow: false },
  // Reals (Phase 6A, D20) answer only their display.
  '10.625': { real: true, value: '10.625' },
  '1.0 / 4': { real: true, value: '0.25' },
};
const calcIntErrors = { '1.5 & 1': 'NOT_INTEGER', '1e3': 'SYNTAX' };
const calcFractionResults = {
  '10.625': { binary: '1010.101', octal: '12.5', hex: 'A.A' },
  '0.1': { binary: '0.00011001100110011001100110011001...', octal: '0.06314631463146314631463146314631...', hex: '0.19999999999999999999999999999999...' },
};
const calcInspections = [];
const calcTabChanges = [];
export async function CalcEvaluateInt(input, word, inputBase, signedMode) {
  record('CalcEvaluateInt');
  calcEvaluations.push({ input, wordSize: word, inputBase, signedMode });
  if (calcQueueFull) throw Error('The calculator is paused until your calculations reach the server. Check your connection.');
  if (calcUnavailable || state.phase !== 'exam' || state.locked || !examTools?.calculator?.enabled || !(examTools.calculator.modes ?? ['scientific']).includes('programmer')) throw Error('The calculator is not available.');
  const trimmed = input.trim();
  const canned = calcIntResults[trimmed];
  if (canned) return { ok: true, ...canned };
  if (calcIntErrors[trimmed]) return { ok: false, code: calcIntErrors[trimmed] };
  // A plain decimal literal that fits is a real result: the UI tests and the bit grid use them.
  if (/^\d+$/.test(trimmed) && BigInt(trimmed) < (1n << BigInt(word))) {
    const bits = BigInt(trimmed);
    return { ok: true, unsigned: trimmed,
      signed: (bits >= (1n << BigInt(word - 1)) ? bits - (1n << BigInt(word)) : bits).toString(10),
      hex: '0x' + bits.toString(16).toUpperCase().padStart(word / 4, '0'),
      octal: bits.toString(8).padStart(Math.ceil(word / 3), '0'),
      binary: bits.toString(2).padStart(word, '0'), bits: trimmed, carry: false, overflow: false };
  }
  if (/^\d+$/.test(trimmed)) return { ok: false, code: 'OVERFLOW' };
  if (['1 / 0', '1 << 8', '256', 'foo(1)'].includes(trimmed)) return { ok: false, code: trimmed === 'foo(1)' ? 'UNKNOWN_FUNCTION' : trimmed === '1 / 0' ? 'DIVISION_BY_ZERO' : trimmed === '1 << 8' ? 'SHIFT_RANGE' : 'OVERFLOW' };
  return { ok: false, code: 'SYNTAX' };
}
export async function CalcConvertFraction(input, word, inputBase, signedMode) {
  record('CalcConvertFraction');
  calcEvaluations.push({ input, view: 'fraction', wordSize: word, inputBase, signedMode });
  if (calcQueueFull) throw Error('The calculator is paused until your calculations reach the server. Check your connection.');
  if (calcUnavailable || state.phase !== 'exam' || state.locked || !examTools?.calculator?.enabled || !(examTools.calculator.modes ?? ['scientific']).includes('programmer')) throw Error('The calculator is not available.');
  const canned = calcFractionResults[input.trim()];
  if (canned) return { ok: true, ...canned };
  return { ok: false, code: 'SYNTAX' };
}
// Encode's canned bits; Decode's canned values (Phase 6A, D21).
const calcIEEE = {
  'binary16': { '1.0': '0x3C00', '65504': '0x7BFF', '0.1': '0x2E66', '10.625': '0x4950', '0x1e3': '0x5F8C' },
  'bfloat16': { '1.0': '0x3F80', '10.625': '0x412A' },
  'binary32': { '10.625': '0x412A0000' },
};
const calcIEEEDecoded = { 'binary16': { '0x3C00': ['0x3C00', '1'], '0x3C00 | 0x8000': ['0xBC00', '-1'] }, 'binary32': { '0x412A0000': ['0x412A0000', '10.625'] } };
const formatBits = { binary16: 16, bfloat16: 16, binary32: 32, binary64: 64 };
export async function CalcInspect(input, format, direction, word, inputBase, signedMode) {
  record('CalcInspect');
  calcInspections.push({ input, format, direction, wordSize: word, inputBase, signedMode });
  if (calcQueueFull) throw Error('The calculator is paused until your calculations reach the server. Check your connection.');
  const tools = examTools?.calculator;
  if (calcUnavailable || state.phase !== 'exam' || state.locked || !tools?.enabled || !(tools.modes ?? ['scientific']).includes('programmer') || (tools.disabled_functions ?? []).includes('ieee754')) throw Error('The calculator is not available.');
  const text = input.trim();
  if (direction === 'decode') {
    const [bits, value] = calcIEEEDecoded[format]?.[text] ?? [];
    if (bits) return { ok: true, format, bits, sign: value.startsWith('-') ? '1' : '0', exponent_field: '15', unbiased_exponent: '0', mantissa_field: '0', class: 'normal', value, rounded: false };
    if (/^0x[0-9a-f]+$/i.test(text) && BigInt(text) >= (1n << BigInt(formatBits[format]))) return { ok: false, code: 'BIT_WIDTH' };
    if (text.includes('.')) return { ok: false, code: 'NOT_INTEGER' };
    return { ok: false, code: 'SYNTAX' };
  }
  const canned = calcIEEE[format]?.[text];
  if (canned) return { ok: true, format, bits: canned, sign: '0', exponent_field: '15', unbiased_exponent: '0', mantissa_field: '0', class: 'normal', value: { '1.0': '1', '65504': '65504', '10.625': '10.625', '0x1e3': '483', '0.1': '0.0999755859375' }[text], rounded: text === '0.1' };
  return { ok: false, code: 'SYNTAX' };
}
export async function CalcTabChanged(from, to) {
  record('CalcTabChanged');
  calcTabChanges.push({ from, to });
  if (!examTools?.calculator?.enabled) throw Error('The calculator is not available.');
}
const calcOpenings = [];
export async function CalcOpened(mode) { record('CalcOpened'); calcOpenings.push(mode); if (calcUnavailable || !examTools?.calculator?.enabled) throw Error('The calculator is not available.'); }
export async function CalcClosed(mode, openMs) { record('CalcClosed'); if (!examTools?.calculator?.enabled) throw Error('The calculator is not available.'); }
export async function CalcModeChanged(from, to) { record('CalcModeChanged'); if (!examTools?.calculator?.enabled) throw Error('The calculator is not available.'); }
export async function CalcEvaluate(input, angleMode) {
  record('CalcEvaluate');
  calcEvaluations.push({ input, angleMode });
  if (calcQueueFull) throw Error('The calculator is paused until your calculations reach the server. Check your connection.');
  if (calcUnavailable || state.phase !== 'exam' || state.locked || !examTools?.calculator?.enabled) throw Error('The calculator is not available.');
  const trimmed = input.trim();
  if (trimmed.length > 200) return { ok: false, code: 'TOO_LONG' };
  const disabled = examTools.calculator.disabled_functions ?? [];
  const modes = examTools.calculator.modes ?? ['scientific'];
  // Without neural mode a bracket is a stray character (Phase 7).
  if (!modes.includes('neural') && /[[\]]/.test(trimmed)) return { ok: false, code: 'SYNTAX' };
  const call = trimmed.match(/^([a-zA-Z_][a-zA-Z0-9_]*)\s*\(/);
  if (call) {
    // Allowed is checked before arity, so a disallowed name never answers ARITY.
    const name = call[1].toLowerCase();
    const names = [...(modes.includes('scientific') ? calcNames.scientific : []), ...(modes.includes('neural') ? calcNames.neural : [])];
    if (disabled.includes(name) || !names.includes(name)) return { ok: false, code: 'UNKNOWN_FUNCTION' };
  } else if (/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(trimmed) && trimmed !== 'ans') {
    const name = trimmed.toLowerCase();
    if (!['pi', 'e'].includes(name) || disabled.includes(name)) return { ok: false, code: 'UNKNOWN_IDENT' };
  }
  if (calcErrors[trimmed]) return { ok: false, code: calcErrors[trimmed] };
  const canned = calcResults[trimmed];
  if (typeof canned === 'object') return { ok: true, ...canned };
  if (canned !== undefined) { fakeAns = Number(canned) || fakeAns; return { ok: true, display: canned }; }
  if (trimmed === 'ans') return { ok: true, display: String(fakeAns) };
  if (trimmed === 'ans+1') { fakeAns += 1; return { ok: true, display: String(fakeAns) }; }
  return { ok: false, code: 'SYNTAX' };
}
window.__studentHarness = {
  calculatorTools(value) { examTools = value; exam.tools = value; },
  calcUnavailable(value) { calcUnavailable = value; },
  calcQueueFull(value) { calcQueueFull = value; },
  nativeCallsReset() { nativeCalls.length = 0; },
  get nativeCalls() { return [...nativeCalls]; },
  get calcEvaluations() { return calcEvaluations.map(entry => ({ ...entry })); },
  get calcInspections() { return calcInspections.map(entry => ({ ...entry })); },
  get calcTabChanges() { return calcTabChanges.map(entry => ({ ...entry })); },
  get calcOpenings() { return [...calcOpenings]; },
  alert(sequence) { alertListener({ local_incident: sequence }); },
  monitoring(value) { state.monitoring = value; },
  sound(value) { state.sound = value; },
  capture(value) { state.capture = { ...state.capture, ...value }; if (value.state === 'failed') { state.capture.ready = false; state.capture.blocked = true; state.capture.block_reason = 'Screenshot retest required: editing is paused until a screenshot test succeeds. Current answers are preserved.'; } },
  get permissionChecks() { return permissionChecks; },
  permissionFailure(value) { permissionCheckFailure = value; },
  captureBehavior(value) { captureBehavior = value; },
  get closeCalls() { return closeCalls; },
  get answers() { return { ...answers }; }, get submitted() { return captured; },
  offline(value) { offline = value; state.timing_stale = value; state.status_error = value ? 'Server unavailable.' : ''; if (!value) { state.save_error = ''; acknowledge(); } },
  loginFailure(code) { failLogin = code; }, loadFailure(value) { failLoad = value; }, lostSubmit(value) { lostSubmit = value; },
  navigation(value) { exam.navigation = value; },
  lock(value) { state.locked = value ? { since: '2035-01-02T03:00:05Z', reason: value } : null; },
  unlock(addedSeconds) { state.locked = null; state.unlock_note = 'Unlocked by your instructor'; state.added_seconds = addedSeconds; },
  rulesUnavailable(value) { rulesUnavailable = value; },
  exam(value) { exam.content = value.content; exam.navigation = value.navigation; answers = Object.fromEntries(value.content.exercises.flatMap(e => [...(e.slots ?? []), ...(e.items ?? []).flatMap(i => i.slots ?? [])]).map(s => [s.id, s.starter ?? ''])); },
  get exercise() { return state.exercise; },
  holdAdvance(value) { holdAdvance = value; },
  releaseAdvance() { finishAdvance(); },
  status(value) { state.status = { ...status, ...value }; if (['submitted', 'force_submitted'].includes(value.status)) state.phase = 'complete'; },
};
function acknowledge() {
  if (offline || state.submitting) return;
  state.acknowledged_revision = state.revision;
  state.save_state = 'saved'; state.saved = { version_seq: state.revision + 1, saved_at: clock };
}
export async function Snapshot() { record('Snapshot'); return result(false); }
export async function Login(server, account, pin) { record('Login');
  if (!state.rules_accepted) throw Error('Acknowledge monitoring disclosure first.');
  if (!state.capture.ready || state.capture.blocked) throw Error('Pass the screenshot test first.');
  if (!server.startsWith('http:') || !account.trim() || !pin.trim()) throw Error('Required fields are invalid.');
  if (failLogin) throw Error(failLogin === 409 ? 'This session has already been submitted.' : 'Credentials were rejected.');
  state.server = server; state.account = account; state.phase = 'loading'; state.status = status;
  if (failLoad) throw Error('Exam loading failed.');
  state.phase = 'exam'; state.timing_stale = offline; return result(true);
}
export async function RetryLoad() { record('RetryLoad'); if (failLoad) throw Error('Exam loading failed.'); state.phase = 'exam'; state.timing_stale = offline; return result(true); }
export async function UpdateAnswers(map, revision) { record('UpdateAnswers');
  if (state.phase !== 'exam' || state.submitting || state.advancing) throw Error('Editing is paused.');
  if (revision <= state.revision) return result(false);
  answers = { ...map }; state.revision = revision; state.save_state = offline ? 'failed' : 'pending'; state.save_error = offline ? 'Server could not be reached.' : '';
  setTimeout(acknowledge, 100); return result(false);
}
export async function Submit(map, revision) { record('Submit');
  captured = { ...map }; answers = { ...map }; state.revision = revision; state.submitting = true;
  if (lostSubmit) { state.submit_error = 'Confirmation unavailable — retrying.'; return result(false); }
  state.phase = 'complete'; state.submitting = false; state.final = { status: 'submitted', version_seq: revision + 1 }; return result(false);
}
let holdAdvance = false;
function finishAdvance() {
  if (!state.advancing) return;
  state.advancing = false; state.exercise += 1; acknowledge();
}
export async function Advance(map, revision, from) { record('Advance');
  if (state.phase !== 'exam' || state.submitting || exam.navigation !== 'forward_only') throw Error('Moving to the next exercise is not available now.');
  if (from !== state.exercise || from >= exam.content.exercises.length - 1) throw Error('The exam is no longer on that exercise.');
  answers = { ...map }; state.revision = revision; state.advancing = true;
  if (!holdAdvance) finishAdvance();
  return result(false);
}
export async function Retry() { record('Retry');
  if (state.submitting) { state.phase = 'complete'; state.submitting = false; state.submit_error = ''; state.final = { status: 'submitted', version_seq: state.revision + 1 }; }
  acknowledge();
}
let captureBehavior = 'success';
export function TestScreenshot() { record('TestScreenshot');
  window.__studentHarness.captureCalls = (window.__studentHarness.captureCalls || 0) + 1;
  let rejectCapture;
  const pending = new Promise((resolve, reject) => {
    rejectCapture = reject;
    if (captureBehavior === 'hold') return;
    if (captureBehavior === 'failure' || captureBehavior === 'invalid') {
      state.capture.ready = false; state.capture.blocked = true;
      if (captureBehavior === 'failure') return reject(Error('Screen capture permission denied.'));
      return resolve({ width: 0, height: 0 });
    }
    state.capture.ready = true; state.capture.blocked = false;
    state.capture.permission = { supported: true, granted: true, test_required: true, message: 'Screenshot test passed. Automatic incident capture is armed.' };
    resolve({ width: 1920, height: 1080 });
  });
  pending.cancel = () => { state.capture.ready = false; state.capture.blocked = true; rejectCapture(Object.assign(Error('Capture cancelled.'), { name: 'CancelError' })); };
  return pending;
}
