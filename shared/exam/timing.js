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
  const parse = value => Date.parse(String(value).replace(/(\.\d{3})\d+/, '$1'));
  const millis = parse(status.grace_until) - parse(serverTime);
  return formatTime(Math.ceil(millis / 1000));
}
