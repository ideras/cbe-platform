// Parse Go timestamps in WebKit (which accepts at most millisecond precision).
export const parseTime = value => Date.parse(String(value).replace(/(\.\d{3})\d+/, '$1'));

// Durations do not wrap at 24 hours. Unknown observations stay visibly unknown.
export function formatTime(seconds) {
  if (seconds == null || !Number.isFinite(seconds)) return 'unknown';
  const whole = Math.max(0, Math.floor(seconds));
  return [Math.floor(whole / 3600), Math.floor(whole / 60) % 60, whole % 60]
    .map(value => String(value).padStart(2, '0')).join(':');
}

// Grace is measured against the server observation, never this computer's clock.
// Refreshing a snapshot updates the countdown; stale reports retain their last value.
export function remainingTime(status, serverTime = status?.server_time) {
  if (status?.status !== 'grace') return formatTime(status?.seconds_remaining);
  if (!status.grace_until || !serverTime) return 'unknown';
  // Go timestamps can contain nanoseconds; WebKit expects at most milliseconds.
  const millis = parseTime(status.grace_until) - parseTime(serverTime);
  return formatTime(Math.ceil(millis / 1000));
}

// Count down relative to a monotonic report age, never the student's wall clock.
// Zero is only a display value: only the next server report changes lifecycle.
export function effectiveRemaining(status, ageMillis = 0, elapsedMillis = 0) {
  if (!status) return null;
  if (['submitted', 'force_submitted'].includes(status.status)) return 0;
  const reported = status.status === 'grace'
    ? (parseTime(status.grace_until) - parseTime(status.server_time)) / 1000
    : status.seconds_remaining;
  if (!Number.isFinite(reported)) return null;
  return Math.max(0, reported - Math.max(0, ageMillis) / 1000 - Math.max(0, elapsedMillis) / 1000);
}

export function timeChip(status, remaining, stale = false) {
  if (['submitted', 'force_submitted'].includes(status?.status)) return { kind: 'submitted', text: 'Submitted' };
  const time = formatTime(remaining == null ? null : Math.ceil(remaining));
  if (stale) return { kind: 'stale', text: `Timing stale · ${time}` };
  if (status?.status === 'grace') return { kind: 'grace', text: `Grace ${time}` };
  return { kind: remaining != null && remaining <= 300 ? 'warning' : 'normal', text: `${remaining != null && remaining <= 300 ? 'Time low · ' : ''}${time}` };
}

// The bound label, not its timestamp, identifies a warning episode. Adding more
// than 30 seconds above a threshold re-arms it; repeating snapshots cannot.
export class TimeWarnings {
  constructor() { this.previous = null; this.bound = ''; this.fired = new Set(); }
  update(status, remaining, stale = false) {
    if (stale || remaining == null || !['active', 'grace'].includes(status?.status)) return [];
    const bound = status.status === 'grace' ? 'grace' : status.ends_at && parseTime(status.ends_at) <= parseTime(status.deadline_at) ? 'end' : 'deadline';
    if (bound !== this.bound) { this.previous = null; this.bound = bound; this.fired.clear(); }
    const result = [];
    for (const minutes of bound === 'grace' ? [1] : [10, 5, 1]) {
      const seconds = minutes * 60;
      if (remaining > seconds + 30) this.fired.delete(minutes);
      if (this.previous != null && this.previous > seconds && remaining <= seconds && !this.fired.has(minutes)) {
        this.fired.add(minutes);
        const end = status.ends_at ? new Date(parseTime(status.ends_at)).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '';
        const unit = `${minutes} minute${minutes === 1 ? '' : 's'}`;
        const hasGrace = parseTime(status.grace_until) > parseTime(status.deadline_at);
        const text = bound === 'grace' ? `${unit} of grace left: your answers are submitted automatically.`
          : bound === 'end' ? `${unit} left: the exam ends at ${end} and is submitted automatically.`
          : hasGrace ? `${unit} left before the grace period.` : `${unit} left: your answers are submitted automatically.`;
        result.push({ key: `time-${minutes}-${bound}`, text });
      }
    }
    this.previous = remaining;
    return result;
  }
}
