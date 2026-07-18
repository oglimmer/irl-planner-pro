import { describe, it, expect } from 'vitest'
import {
  addDays,
  defaultDayType,
  eventDayRange,
  formatDate,
  formatDateRange,
  formatDeadline,
  formatInZone,
  tripLength,
} from './datetime'

describe('addDays', () => {
  it('adds and subtracts whole days', () => {
    expect(addDays('2026-07-27', 1)).toBe('2026-07-28')
    expect(addDays('2026-07-27', -1)).toBe('2026-07-26')
    expect(addDays('2026-07-27', 0)).toBe('2026-07-27')
  })
  it('crosses month and year boundaries', () => {
    expect(addDays('2026-07-31', 1)).toBe('2026-08-01')
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addDays('2026-03-01', -1)).toBe('2026-02-28')
  })
  it('handles a leap day', () => {
    expect(addDays('2028-02-28', 1)).toBe('2028-02-29')
  })
  it('returns the input unchanged for garbage', () => {
    expect(addDays('not-a-date', 1)).toBe('not-a-date')
  })
})

describe('eventDayRange', () => {
  it('lists an inclusive multi-day range in order', () => {
    expect(eventDayRange('2026-07-27', '2026-07-30')).toEqual([
      '2026-07-27',
      '2026-07-28',
      '2026-07-29',
      '2026-07-30',
    ])
  })
  it('returns a single day when start equals end', () => {
    expect(eventDayRange('2026-07-27', '2026-07-27')).toEqual(['2026-07-27'])
  })
  it('spans a month boundary', () => {
    expect(eventDayRange('2026-07-30', '2026-08-01')).toEqual(['2026-07-30', '2026-07-31', '2026-08-01'])
  })
  it('returns [] when end precedes start', () => {
    expect(eventDayRange('2026-07-30', '2026-07-27')).toEqual([])
  })
  it('returns [] for missing or invalid input', () => {
    expect(eventDayRange('', '2026-07-27')).toEqual([])
    expect(eventDayRange('2026-07-27', '')).toEqual([])
    expect(eventDayRange('nope', '2026-07-27')).toEqual([])
  })
})

describe('defaultDayType', () => {
  it('marks the first and last day as travel, the middle as event', () => {
    expect(defaultDayType(0, 4)).toBe('travel')
    expect(defaultDayType(3, 4)).toBe('travel')
    expect(defaultDayType(1, 4)).toBe('event')
    expect(defaultDayType(2, 4)).toBe('event')
  })
  it('marks a single-day event as travel', () => {
    expect(defaultDayType(0, 1)).toBe('travel')
  })
})

describe('tripLength', () => {
  it('counts inclusive whole days', () => {
    expect(tripLength('2026-07-27', '2026-07-31')).toBe(5)
    expect(tripLength('2026-07-27', '2026-07-27')).toBe(1)
  })
  it('spans a month boundary', () => {
    expect(tripLength('2026-07-30', '2026-08-02')).toBe(4)
  })
  it('returns 0 on invalid input', () => {
    expect(tripLength('bad', '2026-07-31')).toBe(0)
  })
})

describe('formatDateRange', () => {
  it('compacts a same-month range', () => {
    expect(formatDateRange('2026-07-27', '2026-07-31')).toBe('27–31 Jul 2026')
  })
  it('renders a single date when start equals end', () => {
    expect(formatDateRange('2026-07-27', '2026-07-27')).toBe('27 Jul 2026')
  })
  it('renders two full dates across months', () => {
    expect(formatDateRange('2026-07-30', '2026-08-02')).toBe('30 Jul 2026 – 02 Aug 2026')
  })
  it('returns empty string on invalid input', () => {
    expect(formatDateRange('bad', '2026-07-31')).toBe('')
  })
})

describe('formatDate', () => {
  it('renders a weekday-prefixed calendar date in UTC (no off-by-one)', () => {
    // Parsed at UTC midnight, so the day never shifts under the test runner's tz.
    expect(formatDate('2026-10-12')).toBe('Mon, 12 Oct 2026')
  })
  it('falls back to the raw value for garbage', () => {
    expect(formatDate('not-a-date')).toBe('not-a-date')
  })
})

describe('formatInZone', () => {
  it('renders an instant in the given zone with a zone label', () => {
    // 15:00Z in summer is 17:00 CEST (Europe/Paris, UTC+2).
    const s = formatInZone('2026-07-15T15:00:00Z', 'Europe/Paris')
    expect(s).toContain('17:00')
    expect(s).toContain('CEST')
    expect(s).toContain('Jul 2026')
  })
  it('falls back to the raw string on bad input', () => {
    expect(formatInZone('not-a-date', 'Europe/Paris')).toBe('not-a-date')
  })
})

describe('formatDeadline', () => {
  it('renders a long date + time in the company zone with an explicit label', () => {
    const s = formatDeadline('2026-07-15T15:00:00Z')
    expect(s).toContain('July 15, 2026')
    expect(s).toContain('(Europe/Paris)')
  })
  it('falls back to the raw string on bad input', () => {
    expect(formatDeadline('not-a-date')).toBe('not-a-date')
  })
})
