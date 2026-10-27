// Server times arrive as RFC 3339 in UTC, with up to nine fractional digits. They are shown in the dashboard computer's
// own time zone. The fraction is cut to milliseconds first: not every WebKit parses more digits.

/** The Date of a server timestamp, or null when it is missing, unparsable or Go's zero time. */
export function parseTime(value) {
  if (!value) return null;
  const date = new Date(String(value).replace(/(\.\d{3})\d+/, '$1'));
  return Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1 ? null : date;
}

/** "9:28:25 PM" in the local time zone, or the fallback. */
export function localTime(value, fallback = 'not reported') {
  return parseTime(value)?.toLocaleTimeString() ?? fallback;
}

/** "10/18/2026, 9:28:25 PM" in the local time zone, or the fallback. */
export function localDateTime(value, fallback = 'not reported') {
  return parseTime(value)?.toLocaleString() ?? fallback;
}
