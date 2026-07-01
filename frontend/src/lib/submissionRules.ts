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
  // Arrives the day before and self-funds that night (but still wants company
  // transport). An alternative to the company-paid extra night before.
  extraStaySelfFunded: boolean
}

// extraNightErrors enforces the consistency between the arrival day and how the
// night before it is covered:
//   • Picking the day *before* the event as your arrival needs that night covered.
//     The employee form now offers only the long-haul confirmation (company-paid
//     hotel); a self-funded flag still counts as covered here for parity with the
//     server / admin editor, which can still set it. Otherwise it's rejected.
//   • Conversely, a booked company night with an in-window arrival is an orphan the
//     People team would never book, so it must be removed (or the arrival day
//     extended to match). A stray self-funded flag is not an error (the form clears
//     it), so it isn't reported here.
// It returns a human message when the arrival side is off (empty when all good),
// explaining exactly what to fix. `beforeDate` is the event start −1 ISO date (the
// only out-of-window day the form offers); `fmt` renders an ISO date for display. A
// self-arranged arrival is skipped (its day is blank and a company night can still
// legitimately sit alongside it). The departure side has no mirror: late return is
// no longer offered, so the departure day can never fall after the event. Mirrors
// the server check in submissions.go / normalizeAndValidate.
export function extraNightErrors(
  form: StayFormState,
  beforeDate: string,
  fmt: (iso: string) => string,
): string[] {
  if (form.attending !== 'yes') return []
  const errs: string[] = []

  if (!form.arrivalIndependent) {
    const arrivesEarly = form.arrivalDay != null && form.arrivalDay <= beforeDate
    const bookedBefore = form.longHaul && form.extraStayStart != null
    const covered = bookedBefore || form.extraStaySelfFunded
    if (arrivesEarly && !covered) {
      errs.push(
        `You're arriving on ${fmt(beforeDate)}, the day before the event starts — confirm you're a ` +
          `long-haul traveller who needs the extra night at the hotel, or pick a later arrival day.`,
      )
    } else if (bookedBefore && !arrivesEarly) {
      errs.push(
        `You've booked a company hotel night for ${fmt(beforeDate)} but your arrival isn't set to that day, ` +
          `so the extra night isn't needed — change the night-before choice, or set your arrival day to ${fmt(beforeDate)}.`,
      )
    }
  }

  return errs
}
