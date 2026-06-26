// Shared types mirroring the backend JSON shapes.

export type AuthMode = 'oidc' | 'password'

export interface AuthConfig {
  mode: AuthMode
  defaultEventTimezone: string
}

export interface User {
  id: string
  email: string
  firstName: string
  lastName: string
  name: string // derived display name (first + last); read-only
  allergies: string // allergies / dietary preferences (profile, not per-event)
  profileConfirmed: boolean // false until the user reviews their profile once (first-login confirm step)
  isAdmin: boolean
  createdAt: string
}

export interface UserSummary {
  id: string
  email: string
  firstName: string
  lastName: string
  name: string // derived display name (first + last); read-only
  isAdmin: boolean
  createdAt: string
}

// ProfileInput is the self-service profile edit payload (PUT /api/me): name plus
// allergies / dietary preferences.
export interface ProfileInput {
  firstName: string
  lastName: string
  allergies: string
}

export type DayType = 'travel' | 'event'

export interface EventDay {
  date: string // YYYY-MM-DD
  type: DayType
}

export interface Event {
  id: string
  slug: string
  name: string
  country: string
  city: string
  hotelName: string
  hotelAddress: string
  hotelLink: string
  timezone: string
  startDate: string // YYYY-MM-DD
  endDate: string // YYYY-MM-DD
  submissionDeadline: string // RFC3339 UTC
  submissionDeadlineLocal: string // wall-clock in event tz
  reminderDaysBefore: number
  weeklyReminders: boolean
  reminderHour: number
  dailyActivityEmail: boolean
  isPast: boolean
  days: EventDay[]
  createdAt: string
  updatedAt: string
}

// ActiveEvent is a current (non-past) event plus the caller's own RSVP state,
// returned by GET /api/active-events to drive the post-login landing card.
export interface ActiveEvent extends Event {
  hasSubmitted: boolean
  myAttending: Attending | '' // '' when the caller hasn't responded yet
}

// EventInput is the create/update payload (subset of Event plus the local
// deadline string the backend interprets in the event timezone).
export interface EventInput {
  slug: string
  name: string
  country: string
  city: string
  hotelName: string
  hotelAddress: string
  hotelLink: string
  timezone: string
  startDate: string
  endDate: string
  submissionDeadlineLocal: string
  reminderDaysBefore: number
  weeklyReminders: boolean
  reminderHour: number
  dailyActivityEmail: boolean
  days?: EventDay[]
}

export type Attending = 'yes' | 'no' | 'not_sure'
export type TravelMode = 'flight' | 'car' | 'train' | 'other'

export interface Submission {
  id: string
  eventId: string
  userId: string
  email: string
  firstName: string
  lastName: string
  attending: Attending
  notSureReason: string
  arrivalDay: string | null
  arrivalTime: string
  arrivalMode: TravelMode | null
  arrivalDetails: string
  departureDay: string | null
  departureTime: string
  departureMode: TravelMode | null
  departureDetails: string
  arrivalIndependent: boolean
  departureIndependent: boolean
  longHaul: boolean
  extraStayStart: string | null
  extraStayEnd: string | null
  allergies: string
  comments: string
  createdAt: string
  updatedAt: string
}

// SubmissionInput is the writable subset sent on create/update. The attendee's
// name and allergies are not included — they come from their profile.
export interface SubmissionInput {
  attending: Attending
  notSureReason: string
  arrivalDay: string | null
  arrivalTime: string
  arrivalMode: TravelMode | null
  arrivalDetails: string
  departureDay: string | null
  departureTime: string
  departureMode: TravelMode | null
  departureDetails: string
  arrivalIndependent: boolean
  departureIndependent: boolean
  longHaul: boolean
  extraStayStart: string | null
  extraStayEnd: string | null
  comments: string
}

export interface ActivityChange {
  field: string
  from: string
  to: string
}

export interface ActivityEntry {
  id: string
  actorEmail: string
  subjectEmail: string
  action: string
  summary: string
  detail?: { changes?: ActivityChange[] }
  afterDeadline: boolean
  createdAt: string
}

export interface RosterEntry {
  fullName: string
  email: string
}

export interface RosterUploadResult {
  inserted: number
  skipped: number
  errors: string[]
}

export type AttendingState = 'yes' | 'no' | 'not_sure' | 'no_response'

export interface DashboardRosterEntry {
  fullName: string
  email: string
  attending: AttendingState
  afterDeadlineEdit: boolean
}

export interface DashboardOffRoster {
  name: string
  email: string
  attending: Attending
}

export interface Dashboard {
  rosterTotal: number
  counts: { yes: number; no: number; notSure: number; noResponse: number }
  rosterEntries: DashboardRosterEntry[]
  offRoster: DashboardOffRoster[]
}

export interface BackendBuildInfo {
  name: string
  version: string
  gitCommit: string
  buildTime: string
}
