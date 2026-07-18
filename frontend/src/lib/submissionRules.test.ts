import { describe, it, expect } from 'vitest'
import {
  extraNightErrors,
  fieldChecks,
  missingRequiredCount,
  travelCostLabel,
  type StayFormState,
} from './submissionRules'

// A blank form (attendance not yet chosen). Helpers below override only the
// fields a given case exercises.
function form(overrides: Partial<StayFormState> = {}): StayFormState {
  return {
    attending: '',
    notSureReason: '',
    arrivalIndependent: false,
    arrivalDay: null,
    arrivalMode: null,
    arrivalTime: '',
    arrivalDetails: '',
    departureIndependent: false,
    departureDay: null,
    departureMode: null,
    departureTime: '',
    departureDetails: '',
    longHaul: false,
    extraStayStart: null,
    extraStaySelfFunded: false,
    ...overrides,
  }
}

// A fully-valid "yes" with both legs by flight — the strictest branch.
function validFlightYes(): StayFormState {
  return form({
    attending: 'yes',
    arrivalDay: '2026-07-27',
    arrivalMode: 'flight',
    arrivalTime: '09:00',
    arrivalDetails: 'AF1234',
    departureDay: '2026-07-31',
    departureMode: 'flight',
    departureTime: '18:00',
    departureDetails: 'AF4321',
  })
}

describe('fieldChecks — branch by attendance', () => {
  it('requires nothing while attendance is unanswered', () => {
    const checks = fieldChecks(form())
    expect(Object.values(checks).some((c) => c.required)).toBe(false)
    expect(missingRequiredCount(form())).toBe(0)
  })

  it("requires only the reason for a 'not_sure' answer", () => {
    const checks = fieldChecks(form({ attending: 'not_sure' }))
    expect(checks.notSureReason.required).toBe(true)
    expect(checks.notSureReason.filled).toBe(false)
    expect(checks.arrivalDay.required).toBe(false)
    expect(checks.departureDay.required).toBe(false)
    expect(missingRequiredCount(form({ attending: 'not_sure' }))).toBe(1)
  })

  it("a filled 'not_sure' reason satisfies the only requirement", () => {
    expect(missingRequiredCount(form({ attending: 'not_sure', notSureReason: 'maybe' }))).toBe(0)
  })

  it("treats whitespace-only 'not_sure' reason as unfilled", () => {
    expect(fieldChecks(form({ attending: 'not_sure', notSureReason: '   ' })).notSureReason.filled).toBe(false)
  })

  it("requires nothing extra for a 'no' answer", () => {
    expect(missingRequiredCount(form({ attending: 'no' }))).toBe(0)
  })
})

describe('fieldChecks — attending yes', () => {
  it('requires day + mode on both legs, but time/details only for flights', () => {
    const checks = fieldChecks(form({ attending: 'yes' }))
    expect(checks.arrivalDay.required).toBe(true)
    expect(checks.arrivalMode.required).toBe(true)
    expect(checks.departureDay.required).toBe(true)
    expect(checks.departureMode.required).toBe(true)
    // Mode is still null, so neither leg is a flight yet.
    expect(checks.arrivalTime.required).toBe(false)
    expect(checks.arrivalDetails.required).toBe(false)
    expect(checks.departureTime.required).toBe(false)
    expect(checks.departureDetails.required).toBe(false)
    // day + mode × 2 legs
    expect(missingRequiredCount(form({ attending: 'yes' }))).toBe(4)
  })

  it('requires flight time + number once a leg is by flight', () => {
    const checks = fieldChecks(form({ attending: 'yes', arrivalMode: 'flight' }))
    expect(checks.arrivalTime.required).toBe(true)
    expect(checks.arrivalDetails.required).toBe(true)
  })

  it('does not require time/details for non-flight modes', () => {
    for (const mode of ['car', 'train', 'other'] as const) {
      const checks = fieldChecks(form({ attending: 'yes', arrivalMode: mode }))
      expect(checks.arrivalTime.required).toBe(false)
      expect(checks.arrivalDetails.required).toBe(false)
    }
  })

  it('a fully valid flight "yes" has no missing fields', () => {
    expect(missingRequiredCount(validFlightYes())).toBe(0)
  })

  it('flags a flight leg missing its flight number', () => {
    const f = validFlightYes()
    f.arrivalDetails = ''
    expect(fieldChecks(f).arrivalDetails.filled).toBe(false)
    expect(missingRequiredCount(f)).toBe(1)
  })
})

describe('fieldChecks — independent travel', () => {
  it('drops a leg\'s day/mode requirements when self-arranged', () => {
    const checks = fieldChecks(form({ attending: 'yes', arrivalIndependent: true }))
    // The independent leg counts as filled and its mode is no longer required.
    expect(checks.arrivalDay.required).toBe(true)
    expect(checks.arrivalDay.filled).toBe(true)
    expect(checks.arrivalMode.required).toBe(false)
    expect(checks.arrivalTime.required).toBe(false)
    expect(checks.arrivalDetails.required).toBe(false)
  })

  it('ignores a stale flight mode on an independent leg', () => {
    // Mode left as 'flight' but the leg is independent → no flight requirements.
    const checks = fieldChecks(form({ attending: 'yes', arrivalIndependent: true, arrivalMode: 'flight' }))
    expect(checks.arrivalTime.required).toBe(false)
    expect(checks.arrivalDetails.required).toBe(false)
  })

  it('both legs independent leaves only the opposite leg to fill', () => {
    // Both independent → arrival fully satisfied; departure day+mode... but
    // departure is also independent here, so nothing is missing.
    const f = form({ attending: 'yes', arrivalIndependent: true, departureIndependent: true })
    expect(missingRequiredCount(f)).toBe(0)
  })
})

describe('extraNightErrors — extra-night consistency', () => {
  // Event 27–31 Jul 2026 → the day before is the only out-of-window day the form
  // offers (late return removed, so there is no day-after).
  const BEFORE = '2026-07-26'
  const id = (s: string) => s
  const check = (overrides: Partial<StayFormState>) =>
    extraNightErrors(form(overrides), BEFORE, id)

  it('returns nothing for non-yes answers', () => {
    expect(check({ attending: 'no', arrivalDay: BEFORE })).toEqual([])
    expect(check({ attending: 'not_sure', arrivalDay: BEFORE })).toEqual([])
  })

  it('returns nothing for in-window travel days', () => {
    expect(check({ attending: 'yes', arrivalDay: '2026-07-27', departureDay: '2026-07-31' })).toEqual([])
  })

  it('flags an early arrival with no long-haul confirmation', () => {
    const errs = check({ attending: 'yes', arrivalDay: BEFORE })
    expect(errs).toHaveLength(1)
    expect(errs[0]).toContain('long-haul')
    expect(errs[0]).toContain('later arrival day')
  })

  it('accepts an early arrival once the company night is booked', () => {
    expect(check({ attending: 'yes', arrivalDay: BEFORE, longHaul: true, extraStayStart: BEFORE })).toEqual([])
  })

  it('accepts an early arrival when the attendee self-funds that night', () => {
    // No long-haul, no company night — just the self-funded flag (still wants transport).
    expect(check({ attending: 'yes', arrivalDay: BEFORE, extraStaySelfFunded: true })).toEqual([])
  })

  it('ignores the departure day entirely (late return removed)', () => {
    // The departure side has no consistency rule any more, even on the last day.
    expect(check({ attending: 'yes', departureDay: '2026-07-31' })).toEqual([])
  })

  it('ignores an independent leg even when its day reads out of window', () => {
    // An independent leg's day is blanked on save, but guard the logic anyway.
    expect(check({ attending: 'yes', arrivalIndependent: true, arrivalDay: BEFORE })).toEqual([])
  })

  // Reverse direction: a booked night with an in-window travel day is an orphan.
  it('flags a company night that no early arrival backs', () => {
    const errs = check({ attending: 'yes', arrivalDay: '2026-07-27', longHaul: true, extraStayStart: BEFORE })
    expect(errs).toHaveLength(1)
    expect(errs[0]).toContain("isn't needed")
    expect(errs[0]).toContain('company hotel night')
  })

  it('does not flag a booked night on a self-arranged leg', () => {
    // A long-haul attendee may self-arrange arrival yet still get a company night.
    expect(
      check({ attending: 'yes', arrivalIndependent: true, longHaul: true, extraStayStart: BEFORE }),
    ).toEqual([])
  })
})

describe('travelCostLabel', () => {
  const label = (o: Partial<StayFormState>) => travelCostLabel(form(o))

  it('names the mode when both legs match', () => {
    expect(label({ arrivalMode: 'flight', departureMode: 'flight' })).toBe('Flight cost')
    expect(label({ arrivalMode: 'car', departureMode: 'car' })).toBe('Car travel cost')
    expect(label({ arrivalMode: 'train', departureMode: 'train' })).toBe('Train cost')
  })

  it('lets flight win a mixed trip', () => {
    expect(label({ arrivalMode: 'flight', departureMode: 'train' })).toBe('Flight cost')
    expect(label({ arrivalMode: 'car', departureMode: 'flight' })).toBe('Flight cost')
  })

  it('falls back to generic wording for mixed non-flight or unchosen modes', () => {
    expect(label({ arrivalMode: 'car', departureMode: 'train' })).toBe('Travel cost')
    expect(label({ arrivalMode: 'other', departureMode: 'other' })).toBe('Travel cost')
    expect(label({})).toBe('Travel cost')
  })

  // An independent leg has its mode blanked on write, so it must not colour the
  // label — this is what keeps the wording in step with who gets the flight-cost
  // reminder (messaging.go ignores independent legs too).
  it('ignores a self-arranged leg', () => {
    expect(label({ arrivalIndependent: true, arrivalMode: 'flight', departureMode: 'train' }))
      .toBe('Train cost')
    expect(label({ arrivalIndependent: true, departureIndependent: true, arrivalMode: 'flight' }))
      .toBe('Travel cost')
  })
})
