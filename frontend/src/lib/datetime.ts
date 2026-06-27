// Date/time helpers that render UTC instants in a given event timezone.
// All display goes through these so the UI consistently shows event-local time.

// formatInZone renders an RFC3339/ISO UTC instant in the given IANA timezone,
// e.g. "12 Oct 2026, 17:00 CEST". Falls back to the raw string on bad input.
export function formatInZone(isoUtc: string, timeZone: string): string {
  const d = new Date(isoUtc)
  if (isNaN(d.getTime())) return isoUtc
  try {
    return new Intl.DateTimeFormat('en-GB', {
      timeZone,
      day: '2-digit',
      month: 'short',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      timeZoneName: 'short',
    }).format(d)
  } catch {
    return d.toISOString()
  }
}

// DISPLAY_TIMEZONE is the fixed company zone for showing the RSVP deadline. The
// company is international, so the deadline is shown in one consistent reference
// zone (HQ time) rather than each viewer's local time. Mirrors the backend's
// companyTimezone so email and UI agree.
export const DISPLAY_TIMEZONE = 'Europe/Paris'

// formatDeadline renders a UTC instant as a long US date + 12-hour US time in the
// company timezone, with an explicit zone label — e.g.
// "July 3, 2026 at 2:00 AM (Europe/Paris)". Matches the backend's formatDeadline.
export function formatDeadline(isoUtc: string): string {
  const d = new Date(isoUtc)
  if (isNaN(d.getTime())) return isoUtc
  try {
    const s = new Intl.DateTimeFormat('en-US', {
      timeZone: DISPLAY_TIMEZONE,
      dateStyle: 'long',
      timeStyle: 'short',
    }).format(d)
    return `${s} (${DISPLAY_TIMEZONE})`
  } catch {
    return d.toISOString()
  }
}

// formatDate renders a YYYY-MM-DD calendar date as "Mon 12 Oct 2026". Because it
// is a plain calendar date (no zone), it is parsed as UTC midnight to avoid
// off-by-one shifts.
export function formatDate(ymd: string): string {
  const d = new Date(`${ymd}T00:00:00Z`)
  if (isNaN(d.getTime())) return ymd
  return new Intl.DateTimeFormat('en-GB', {
    timeZone: 'UTC',
    weekday: 'short',
    day: '2-digit',
    month: 'short',
    year: 'numeric',
  }).format(d)
}

// A curated short list of timezones for the event picker, plus any value already
// set (so editing an event with an unusual tz still shows it selected).
export const COMMON_TIMEZONES = [
  'Europe/Paris',
  'Europe/London',
  'Europe/Berlin',
  'Europe/Madrid',
  'Europe/Lisbon',
  'Europe/Athens',
  'America/New_York',
  'America/Los_Angeles',
  'Asia/Dubai',
  'Asia/Singapore',
  'UTC',
]
