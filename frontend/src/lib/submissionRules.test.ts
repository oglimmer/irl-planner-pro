import { describe, it, expect } from 'vitest'
import {
  extraNightErrors,
  fieldChecks,
  missingRequiredCount,
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
    extraStayEnd: null,
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
  // Event 27–31 Jul 2026 → the only out-of-window days the form offers.
  const BEFORE = '2026-07-26'
  const AFTER = '2026-08-01'
  const id = (s: string) => s
  const check = (overrides: Partial<StayFormState>) =>
    extraNightErrors(form(overrides), BEFORE, AFTER, id)

  it('returns nothing for non-yes answers', () => {
    expect(check({ attending: 'no', arrivalDay: BEFORE })).toEqual([])
    expect(check({ attending: 'not_sure', departureDay: AFTER })).toEqual([])
  })

  it('returns nothing for in-window travel days', () => {
    expect(check({ attending: 'yes', arrivalDay: '2026-07-27', departureDay: '2026-07-31' })).toEqual([])
  })

  it('flags an early arrival with no long-haul / extra night, naming both boxes', () => {
    const errs = check({ attending: 'yes', arrivalDay: BEFORE })
    expect(errs).toHaveLength(1)
    expect(errs[0]).toContain('long-haul traveller')
    expect(errs[0]).toContain('Extra night before')
  })

  it('flags an early arrival when long-haul is set but the night is not, naming only the night', () => {
    const errs = check({ attending: 'yes', arrivalDay: BEFORE, longHaul: true })
    expect(errs).toHaveLength(1)
    expect(errs[0]).not.toContain('long-haul traveller')
    expect(errs[0]).toContain('Extra night before')
  })

  it('accepts an early arrival once long-haul + extra night before are both set', () => {
    expect(check({ attending: 'yes', arrivalDay: BEFORE, longHaul: true, extraStayStart: BEFORE })).toEqual([])
  })

  it('flags a late departure symmetrically', () => {
    const errs = check({ attending: 'yes', departureDay: AFTER })
    expect(errs).toHaveLength(1)
    expect(errs[0]).toContain('Extra night after')
  })

  it('accepts a late departure once long-haul + extra night after are both set', () => {
    expect(check({ attending: 'yes', departureDay: AFTER, longHaul: true, extraStayEnd: AFTER })).toEqual([])
  })

  it('reports both sides when both legs are out of window and unbooked', () => {
    expect(check({ attending: 'yes', arrivalDay: BEFORE, departureDay: AFTER })).toHaveLength(2)
  })

  it('ignores an independent leg even when its day reads out of window', () => {
    // An independent leg's day is blanked on save, but guard the logic anyway.
    expect(check({ attending: 'yes', arrivalIndependent: true, arrivalDay: BEFORE })).toEqual([])
  })

  // Reverse direction: a booked night with an in-window travel day is an orphan.
  it('flags an extra night before that no early arrival backs', () => {
    const errs = check({ attending: 'yes', arrivalDay: '2026-07-27', longHaul: true, extraStayStart: BEFORE })
    expect(errs).toHaveLength(1)
    expect(errs[0]).toContain("isn't needed")
    expect(errs[0]).toContain('Extra night before')
  })

  it('flags an extra night after that no late departure backs', () => {
    const errs = check({ attending: 'yes', departureDay: '2026-07-31', longHaul: true, extraStayEnd: AFTER })
    expect(errs).toHaveLength(1)
    expect(errs[0]).toContain("isn't needed")
    expect(errs[0]).toContain('Extra night after')
  })

  it('does not flag a booked night on a self-arranged leg', () => {
    // A long-haul attendee may self-arrange arrival yet still get a company night.
    expect(
      check({ attending: 'yes', arrivalIndependent: true, longHaul: true, extraStayStart: BEFORE }),
    ).toEqual([])
  })
})
