import { effectiveRemaining } from '../../../../shared/exam/timing.js';
import { parseTime } from './dates.js';

export const rosterFilters = [
  { id: 'all', label: 'All' }, { id: 'attention', label: 'Needs attention' },
  { id: 'progress', label: 'In progress' }, { id: 'submitted', label: 'Submitted' },
];
export const inProgress = row => !row.finalization_pending && ['active', 'grace'].includes(row.status);
export const isSubmitted = row => !row.finalization_pending && ['submitted', 'force_submitted'].includes(row.status);
// Someone tried to open the student's account while it was open on another computer. The server counts the refusals since the
// student's latest accepted login; `waiting` is a refusal not yet answered, and `released` a release that no login has used yet.
export function loginGate(row) {
  const gate = row.login_gate;
  if (!gate || !(gate.refusals > 0 || gate.released_at)) return null;
  return { refusals: gate.refusals ?? 0, lastAt: gate.last_refused_at ?? null, addr: gate.last_refused_addr || '', releasedAt: gate.released_at ?? null,
    released: Boolean(gate.released_at), waiting: gate.refusals > 0 && !gate.released_at };
}
// Warnings are independent. A student with several warnings is counted only once.
export const needsAttention = row => Boolean(row.locked || row.network_warning || row.focus_warning || row.finalization_pending || loginGate(row)?.waiting);
export const canForce = row => inProgress(row) || Boolean(row.finalization_pending);

export function rosterCounts(rows) {
  return { total: rows.length, progress: rows.filter(inProgress).length,
    attention: rows.filter(needsAttention).length, locked: rows.filter(row => row.locked).length,
    submitted: rows.filter(isSubmitted).length };
}
export function sortRoster(rows) {
  return [...rows].sort((a, b) => Number(Boolean(b.locked)) - Number(Boolean(a.locked))
    || a.student_name.localeCompare(b.student_name) || a.account_number.localeCompare(b.account_number));
}
export function filterRoster(rows, filter) {
  const predicates = { attention: needsAttention, progress: inProgress, submitted: isSubmitted };
  return Object.hasOwn(predicates, filter) ? rows.filter(predicates[filter]) : rows;
}
export function initials(name) { return name.trim().split(/\s+/).slice(0, 2).map(part => Array.from(part)[0] ?? '').join('').toUpperCase(); }
export const duration = ms => ms == null ? 'unknown' : `${(ms / 1000).toFixed(1)}s`;
export const spoken = ms => ms == null ? 'unknown' : ms < 60000 ? `${Math.round(ms / 1000)} s` : `${Math.round(ms / 60000)} min`;
// An exam's length in the units `spoken` uses for its grace, but in whole hours when there are some: 2 h, 1 h 30 min, 45 min, 90 s.
export function examLength(seconds) {
  if (seconds == null || !Number.isFinite(seconds) || seconds < 0) return 'unknown';
  const whole = Math.round(seconds);
  const parts = [[Math.floor(whole / 3600), 'h'], [Math.floor(whole / 60) % 60, 'min'], [whole % 60, 's']].filter(([amount]) => amount > 0).map(([amount, unit]) => `${amount} ${unit}`);
  return parts.length ? parts.join(' ') : '0 s';
}
export function lockRule(rules) {
  const away = rules.lock_away_after_ms, contact = rules.lock_no_contact_after_ms;
  if (away && contact) return `locks after ${spoken(away)} away or ${spoken(contact)} without contact`;
  if (away) return `locks after ${spoken(away)} away`;
  if (contact) return `locks after ${spoken(contact)} without contact`;
  return 'never locks';
}
export function sound(presence) {
  const level = presence.volume_percent == null ? '' : `${presence.volume_percent}%`;
  if (presence.volume_muted) return level ? `muted (${level})` : 'muted';
  return level || 'not reported';
}
export function lockLabel(row, policy, serverTime) {
  const reason = row.locked.reason === 'no_contact' ? 'no contact' : `away ${spoken(policy.lock_away_after_ms)}`;
  // Even lock ages use the last server observation, so stale data never appears live.
  const observed = parseTime(serverTime), since = parseTime(row.locked.since);
  if (!observed || !since) return { reason, ago: 'at an unreported time' };
  const minutes = Math.floor(Math.max(0, observed - since) / 60000);
  return { reason, ago: minutes < 1 ? 'just now' : minutes === 1 ? '1 min ago' : `${minutes} min ago` };
}
export function networkBadge(row) {
  if (row.network_warning) return { tone: 'network', text: 'Network overdue' };
  if (!row.presence.monitoring) return { tone: 'neutral', text: 'Terminal' };
  if (row.presence.connectivity === 'healthy') return { tone: 'ok', text: 'Online' };
  return { tone: 'neutral', text: row.presence.connectivity === 'no_heartbeat' ? 'No heartbeat' : row.presence.connectivity || 'Unknown' };
}
export function focusBadge(row) {
  if (row.focus_warning) return { tone: 'focus', text: 'Sustained focus loss' };
  if (row.presence.focus.state === 'focused') return { tone: 'active', text: 'Focused' };
  if (row.presence.focus.state === 'unfocused') return { tone: 'network', text: 'Away' };
  return { tone: 'neutral', text: 'Unknown' };
}
export function lifecycleBadge(row, policy, serverTime) {
  if (row.finalization_pending) return { tone: 'neutral', text: 'Finalizing' };
  if (row.locked) return { tone: 'lock', text: `Locked · ${lockLabel(row, policy, serverTime).reason}` };
  if (row.status === 'active') return { tone: 'active', text: 'In progress' };
  if (row.status === 'grace') return { tone: 'network', text: 'Grace' };
  return { tone: 'ok', text: row.status === 'force_submitted' ? 'Force-submitted' : 'Submitted' };
}
export function lowTime(row, serverTime) {
  const remaining = effectiveRemaining({ ...row, server_time: serverTime });
  return inProgress(row) && remaining != null && remaining <= 300;
}
