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
