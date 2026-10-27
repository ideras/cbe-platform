// Turns a page of a session's history into table rows. A focus loss and the return that ends it become one "away" row
// with its duration; everything else is one row per record. Client-supplied details are untrusted text: they are only
// ever shown as text, never as markup.

const labels = {
  login: 'Logged in', app_started: 'Application started', app_closed: 'Application closed', grace_started: 'Grace period started',
  network_gap: 'Network gap', network_connected: 'Network connected', network_reconnected: 'Network back', connection_changed: 'Connection changed',
  capture_ready: 'Screenshot test passed', capture_failed: 'Screenshot capture failed', capture_restored: 'Screenshot capture restored', capture_health: 'Screenshot status',
  submitted: 'Submitted', force_submitted: 'Force-submitted', focus_lost: 'Left the exam window', focus_regained: 'Back in the exam window', marker: 'Marker',
  locked: 'Exam locked', unlocked: 'Exam unlocked',
  calc_opened: 'Calculator opened', calc_closed: 'Calculator closed', calc_mode_changed: 'Calculator mode changed', calc_tab_changed: 'Calculator tab changed',
  calc_evaluated: 'Calculator evaluated', calc_inspected: 'IEEE 754 inspected',
};

/** The table's filter groups. */
export const groups = [
  { id: 'answers', label: 'Answers' },
  { id: 'focus', label: 'Focus' },
  { id: 'network', label: 'Network' },
  { id: 'calculator', label: 'Calculator' },
  { id: 'other', label: 'Other' },
];

export function label(type) { return labels[type] ?? type.replaceAll('_', ' '); }

/** "45 s", "2 min 30 s", "1 h 4 min". */
export function duration(ms) {
  if (ms == null) return 'unknown';
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds} s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return seconds % 60 ? `${minutes} min ${seconds % 60} s` : `${minutes} min`;
  return `${Math.floor(minutes / 60)} h ${minutes % 60} min`;
}

function group(type) {
  if (type === 'focus_lost' || type === 'focus_regained') return 'focus';
  if (type.startsWith('network_') || type === 'connection_changed') return 'network';
  if (type.startsWith('calc_')) return 'calculator';
  return 'other';
}

/** What the lock rows say: the reason of a lock, and the reason and time given back of an unlock. */
export function lockDetail(type, detail) {
  if (type === 'locked') {
    const reason = detail?.reason === 'no_contact' ? 'no contact' : 'away from the exam window';
    const source = detail?.source === 'client' ? 'the student application' : 'the server';
    return `Locked: ${reason} (seen by ${source}).`;
  }
  if (type === 'unlocked') {
    const extended = detail?.extended_ms > 0 ? ` ${duration(detail.extended_ms)} given back.` : ' No time given back.';
    return `Unlocked by the proctor: ${detail?.operator_reason || 'no reason recorded'}.${extended}`;
  }
  return '';
}

/** The programmer panel's settings of a row, which reproduce its result: "8 bits, dec, signed". */
function panel(d) {
  const signedness = d.signed === true ? ', signed' : d.signed === false ? ', unsigned' : '';
  return d.word_size ? `${d.word_size} bits, ${d.input_base}${signedness}` : '';
}

/** The programmer tab ids the student app logs, named as the student sees them; an unknown id is shown as sent. */
const programmerTabs = { keypad: 'Keypad', ieee754: 'IEEE 754', fraction: 'Radix Point' };
const tabLabel = id => (Object.hasOwn(programmerTabs, id) ? programmerTabs[id] : id ?? '?');

/** What a calculator row reads (plan Phases 4 and 6A): "sin(30) = 0.5 (deg)", "log(8) → ARITY",
 *  "radix point: 10.625 = 1010.101 (32 bits, dec, signed)", "encode 1.0 → 0x3C00 (binary16; 32 bits, dec, signed)".
 *  Client-supplied details are untrusted text and are only shown as text. */
export function calcDetail(type, detail) {
  const d = detail?.client ?? {};
  if (type === 'calc_evaluated') {
    const outcome = d.ok === true ? `= ${d.display}` : `→ ${d.code}`;
    const context = d.angle_mode ? ` (${d.angle_mode})` : d.word_size ? ` (${panel(d)})` : '';
    return `${d.view === 'fraction' ? 'radix point: ' : ''}${d.expression} ${outcome}${context}`;
  }
  if (type === 'calc_opened') return d.mode ? `mode: ${d.mode}` : '';
  if (type === 'calc_closed') return d.mode ? `open for ${duration(d.open_ms)} · ${d.mode}` : '';
  if (type === 'calc_mode_changed') return d.from || d.to ? `${d.from ?? '?'} → ${d.to ?? '?'}` : '';
  if (type === 'calc_tab_changed') return d.from || d.to ? `${tabLabel(d.from)} → ${tabLabel(d.to)}` : '';
  if (type === 'calc_inspected') {
    if (!d.format) return '';
    const settings = panel(d);
    return `${d.direction ? `${d.direction} ` : ''}${d.input} → ${d.ok ? d.bits : d.code} (${d.format}${settings ? `; ${settings}` : ''})`;
  }
  return '';
}

function answered(answers) {
  const values = Object.values(answers);
  return { filled: values.filter(text => text.trim() !== '').length, total: values.length };
}

/** Rows for one history page, in its order. `finalVersionId` marks the designated final answer version. */
export function historyRows(items, finalVersionId) {
  const returns = new Map();
  for (const item of items) {
    const lost = item.event?.event_type === 'focus_regained' ? item.event.detail?.focus_lost_event_log_id : null;
    if (Number.isSafeInteger(lost)) returns.set(lost, item);
  }
  const merged = new Set();
  const rows = [];
  for (const item of items) {
    if (item.kind === 'answer_version') {
      const version = item.answer_version;
      rows.push({ id: item.id, at: item.occurred_at, group: 'answers', kind: 'answers', title: 'Answers saved', final: version.version_id === finalVersionId,
        version, ...answered(version.answers), images: [] });
      continue;
    }
    const event = item.event;
    if (merged.has(item.id)) continue;
    const row = { id: item.id, at: item.occurred_at, group: group(event.event_type), kind: 'event', title: label(event.event_type), event, images: event.screenshots ?? [], details: [event] };
    if (event.event_type === 'focus_lost') {
      const back = returns.get(event.event_log_id);
      row.kind = 'away';
      if (back) {
        merged.add(back.id);
        row.until = back.occurred_at;
        row.duration = back.event.detail?.unfocused_duration_ms;
        row.beyond = back.event.detail?.threshold_exceeded === true;
        row.details.push(back.event);
      }
    } else if (event.event_type === 'focus_regained') {
      row.duration = event.detail?.unfocused_duration_ms;
      row.beyond = event.detail?.threshold_exceeded === true;
    } else if (event.event_type === 'locked' || event.event_type === 'unlocked') {
      row.note = lockDetail(event.event_type, event.detail);
    } else if (event.event_type.startsWith('calc_')) {
      row.note = calcDetail(event.event_type, event.detail);
    }
    rows.push(row);
  }
  return rows;
}
