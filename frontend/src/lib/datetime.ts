// Date/time helpers that render UTC instants in a given event timezone.
// All display goes through these so the UI consistently shows event-local time.

import type { DayType } from '../types'

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

// --- Calendar-date math (zone-free YYYY-MM-DD) -----------------------------
// Plain calendar dates are parsed at UTC midnight throughout, so day arithmetic
// never drifts by a day across the viewer's local timezone.

// addDays returns the YYYY-MM-DD `n` days after `ymd` (n may be negative).
// Returns the input unchanged when it isn't a valid date.
export function addDays(ymd: string, n: number): string {
  const d = new Date(`${ymd}T00:00:00Z`)
  if (isNaN(d.getTime())) return ymd
  d.setUTCDate(d.getUTCDate() + n)
  return d.toISOString().slice(0, 10)
}

// eventDayRange lists every calendar date from start to end inclusive, ordered.
// Returns [] for missing/invalid inputs or an end that precedes the start.
export function eventDayRange(start: string, end: string): string[] {
  if (!start || !end) return []
  const s = new Date(`${start}T00:00:00Z`)
  const e = new Date(`${end}T00:00:00Z`)
  if (isNaN(s.getTime()) || isNaN(e.getTime()) || e < s) return []
  const out: string[] = []
  for (const d = new Date(s); d <= e; d.setUTCDate(d.getUTCDate() + 1)) {
    out.push(d.toISOString().slice(0, 10))
  }
  return out
}

// defaultDayType seeds a day's type from its position in the range: the first
// and last days default to travel, everything between to event. A single-day
// event (count 1) is therefore travel.
export function defaultDayType(index: number, count: number): DayType {
  return index === 0 || index === count - 1 ? 'travel' : 'event'
}

// tripLength is the whole-day span of an event, inclusive of both ends (a
// start === end event is 1 day). Returns 0 on invalid input.
export function tripLength(start: string, end: string): number {
  const s = new Date(`${start}T00:00:00Z`).getTime()
  const e = new Date(`${end}T00:00:00Z`).getTime()
  if (isNaN(s) || isNaN(e)) return 0
  return Math.round((e - s) / 86_400_000) + 1
}

// formatDateRange renders a compact, editorial date range: "27–31 Jul 2026" when
// both ends share a month, a single date when start === end, otherwise two full
// dates ("28 Jul 2026 – 02 Aug 2026"). Calendar dates parsed as UTC.
export function formatDateRange(start: string, end: string): string {
  const s = new Date(`${start}T00:00:00Z`)
  const e = new Date(`${end}T00:00:00Z`)
  if (isNaN(s.getTime()) || isNaN(e.getTime())) return ''
  const dmy = (d: Date) =>
    new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', day: '2-digit', month: 'short', year: 'numeric' }).format(d)
  if (start === end) return dmy(s)
  const sameMonth = s.getUTCFullYear() === e.getUTCFullYear() && s.getUTCMonth() === e.getUTCMonth()
  if (!sameMonth) return `${dmy(s)} – ${dmy(e)}`
  const day = (d: Date) => new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', day: '2-digit' }).format(d)
  const monthYear = new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', month: 'short', year: 'numeric' }).format(s)
  return `${day(s)}–${day(e)} ${monthYear}`
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
