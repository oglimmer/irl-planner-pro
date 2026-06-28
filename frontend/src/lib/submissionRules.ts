// Client-side mirror of the conditional required-field rules the backend
// enforces in submissions.go / validateTravelLeg. Kept as a pure module (no Vue
// imports) so the matrix can be unit-tested directly and stays a single,
// reviewable source of truth next to its server counterpart.
import type { Attending, TravelMode } from '../types'

// The form's attendance answer is initially unset ('') until the user picks a
// radio, so the rules accept the widened type rather than forcing a cast.
export type AttendingChoice = Attending | ''

// The subset of the submission form the rules read. `SubmissionInput` is
// assignable to this (its `attending: Attending` narrows `Attending | ''`), so
// callers can pass their reactive form straight in.
export interface SubmissionFormState {
  attending: AttendingChoice
  notSureReason: string
  arrivalIndependent: boolean
  arrivalDay: string | null
  arrivalMode: TravelMode | null
  arrivalTime: string
  arrivalDetails: string
  departureIndependent: boolean
  departureDay: string | null
  departureMode: TravelMode | null
  departureTime: string
  departureDetails: string
}

export type FieldKey =
  | 'notSureReason'
  | 'arrivalDay'
  | 'arrivalMode'
  | 'arrivalTime'
  | 'arrivalDetails'
  | 'departureDay'
  | 'departureMode'
  | 'departureTime'
  | 'departureDetails'

export interface FieldCheck {
  // required: is this field mandatory given the current branch?
  required: boolean
  // filled: does it currently hold a real answer? (drives the inline ✓/missing marks)
  filled: boolean
}

// fieldChecks returns the required/filled state of every conditional field for
// the chosen branch. Flight legs additionally require a time and a flight number;
// a leg the attendee self-arranges (`*Independent`) drops all of its fields.
export function fieldChecks(form: SubmissionFormState): Record<FieldKey, FieldCheck> {
  const yes = form.attending === 'yes'
  const arrFlight = yes && !form.arrivalIndependent && form.arrivalMode === 'flight'
  const depFlight = yes && !form.departureIndependent && form.departureMode === 'flight'
  return {
    notSureReason: { required: form.attending === 'not_sure', filled: !!form.notSureReason.trim() },
    arrivalDay: { required: yes, filled: form.arrivalIndependent || !!form.arrivalDay },
    arrivalMode: { required: yes && !form.arrivalIndependent, filled: !!form.arrivalMode },
    arrivalTime: { required: arrFlight, filled: !!form.arrivalTime },
    arrivalDetails: { required: arrFlight, filled: !!form.arrivalDetails.trim() },
    departureDay: { required: yes, filled: form.departureIndependent || !!form.departureDay },
    departureMode: { required: yes && !form.departureIndependent, filled: !!form.departureMode },
    departureTime: { required: depFlight, filled: !!form.departureTime },
    departureDetails: { required: depFlight, filled: !!form.departureDetails.trim() },
  }
}

// missingRequiredCount counts mandatory fields the user has not yet filled — the
// number the submit handler uses to block the save and message the attendee.
export function missingRequiredCount(form: SubmissionFormState): number {
  return Object.values(fieldChecks(form)).filter((f) => f.required && !f.filled).length
}

// The extra-night consistency check additionally reads the long-haul block, which
// is not part of the required-field matrix above.
export interface StayFormState extends SubmissionFormState {
  longHaul: boolean
  extraStayStart: string | null
  extraStayEnd: string | null
}

// extraNightErrors enforces the two-way consistency between a leg's travel day and
// its long-haul "Extra night" box. They must agree:
//   • Picking the day *before* the event as your arrival (or the day *after* as
//     your departure) needs the matching night booked — long-haul + the Extra-night
//     box ticked — otherwise no accommodation covers that night.
//   • Conversely, a ticked Extra-night box with an in-window travel day is an
//     orphan the People team would never book, so it must be removed (or the
//     travel day extended to match).
// It returns a human message per offending side (empty when all good), explaining
// exactly what to fix. `beforeDate`/`afterDate` are the event start −1 / end +1 ISO
// dates (the only out-of-window days the form offers); `fmt` renders an ISO date
// for display. A self-arranged leg is skipped (its day is blank and a company night
// can still legitimately sit alongside it). Mirrors the server check in
// submissions.go / normalizeAndValidate.
export function extraNightErrors(
  form: StayFormState,
  beforeDate: string,
  afterDate: string,
  fmt: (iso: string) => string,
): string[] {
  if (form.attending !== 'yes') return []
  const errs: string[] = []

  if (!form.arrivalIndependent) {
    const arrivesEarly = form.arrivalDay != null && form.arrivalDay <= beforeDate
    const bookedBefore = form.longHaul && form.extraStayStart != null
    if (arrivesEarly && !bookedBefore) {
      const need: string[] = []
      if (!form.longHaul) need.push('mark yourself as a long-haul traveller')
      need.push(`tick “Extra night before — ${fmt(beforeDate)}”`)
      errs.push(
        `You're arriving on ${fmt(beforeDate)}, the day before the event starts — to stay that night you must ` +
          `${need.join(' and ')} in the accommodation section, or pick a later arrival day.`,
      )
    } else if (bookedBefore && !arrivesEarly) {
      errs.push(
        `You've ticked “Extra night before — ${fmt(beforeDate)}” but your arrival isn't set to that day, ` +
          `so the extra night isn't needed — untick it, or set your arrival day to ${fmt(beforeDate)}.`,
      )
    }
  }

  if (!form.departureIndependent) {
    const leavesLate = form.departureDay != null && form.departureDay >= afterDate
    const bookedAfter = form.longHaul && form.extraStayEnd != null
    if (leavesLate && !bookedAfter) {
      const need: string[] = []
      if (!form.longHaul) need.push('mark yourself as a long-haul traveller')
      need.push(`tick “Extra night after — ${fmt(afterDate)}”`)
      errs.push(
        `You're leaving on ${fmt(afterDate)}, the day after the event ends — to stay that night you must ` +
          `${need.join(' and ')} in the accommodation section, or pick an earlier departure day.`,
      )
    } else if (bookedAfter && !leavesLate) {
      errs.push(
        `You've ticked “Extra night after — ${fmt(afterDate)}” but your departure isn't set to that day, ` +
          `so the extra night isn't needed — untick it, or set your departure day to ${fmt(afterDate)}.`,
      )
    }
  }

  return errs
}
