import { parseTime } from './dates.js';

export function clockValue(date) {
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
}

// Start with the existing cap, or the mock's roughly two-hour suggestion. The
// native PreviewEndTime remains authoritative about dates and midnight crossings.
export function suggestedEndTime(current, now = new Date()) {
  const existing = parseTime(current);
  if (existing && existing > now) return clockValue(existing);
  const suggested = new Date(now.getTime() + 125 * 60000);
  suggested.setMinutes(Math.round(suggested.getMinutes() / 5) * 5, 0, 0);
  return clockValue(suggested);
}

export function addEndMinutes(value, minutes, now = new Date()) {
  if (!/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(value)) return clockValue(new Date(now.getTime() + minutes * 60000));
  const [hours, mins] = value.split(':').map(Number);
  const total = (hours * 60 + mins + minutes) % 1440;
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`;
}

export function endTimeLabel(value, fallback = 'No end time') {
  return parseTime(value)?.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }) ?? fallback;
}

export function endTimeETA(value, now) {
  const date = parseTime(value);
  if (!date) return '';
  const minutes = Math.ceil((date.getTime() - now) / 60000);
  if (minutes <= 0) return 'time reached';
  const hours = Math.floor(minutes / 60);
  return `in ${hours ? `${hours} h ${String(minutes % 60).padStart(2, '0')} min` : `${minutes} min`}`;
}
