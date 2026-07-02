// Shared types mirroring the backend JSON shapes.

export type AuthMode = 'oidc' | 'password'

export interface AuthConfig {
  mode: AuthMode
  defaultEventTimezone: string
  peopleTeamEmail: string // address shown in the "can't attend" instructions
  signInDomain: string // email domain shown in the sign-in copy ('' → generic)
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
  archived: boolean // excluded from all event activities until reactivated
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
  imageUrl: string // '' when no image; carries a ?v=<etag> cache-buster
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

// --- Messaging (event admin "Messaging" tab) -------------------------------

// MessageTemplates are the editable invite/reminder copy. An empty string means
// "no override" — the backend renders a generated default instead. Bodies and
// subjects support {{name}} {{event}} {{city}} {{link}} {{deadline}} placeholders.
export interface MessageTemplates {
  inviteSubject: string
  inviteBody: string
  reminderSubject: string
  reminderBody: string
}

export interface MessagingChannel {
  name: string // 'email' | 'slack'
  available: boolean // implemented & selectable (email and Slack both are)
  configured: boolean // transport actually wired up (SMTP for email, bot token for Slack)
}

export interface MessagingStats {
  attendees: number
  invited: number
  nonResponders: number
}

// MessagingFailure is one recent failed send, shown to the admin so they can act
// (fix an address, retry). "sent" elsewhere means the relay accepted the message,
// not that it was delivered — asynchronous bounces are not tracked.
export interface MessagingFailure {
  recipient: string
  kind: string // invitation | manual | weekly | deadline
  channel: string // email | slack
  error: string
  createdAt: string
}

export interface MessagingStatus {
  templates: MessageTemplates // stored overrides ('' = use default)
  defaults: MessageTemplates // generated defaults, shown as editor placeholders
  stats: MessagingStats
  channels: MessagingChannel[]
  failures: MessagingFailure[] // recent failed sends, newest first
}

export interface SendMessageResult {
  channels: string[] // channels the campaign is delivering over (email + Slack when configured)
  queued: number // recipients handed to the background sender; delivery continues async
}

// One admin's per-event notification preference. notifType '' = off; 'daily' =
// the daily activity summary; 'activity' = an immediate alert on every
// submission create/edit. email/slack are the chosen delivery channel(s).
export interface AdminNotifPref {
  userId: string
  name: string
  email: string // the admin's address
  notifType: '' | 'daily' | 'activity'
  viaEmail: boolean // deliver over email
  viaSlack: boolean // deliver over Slack DM
}

// The per-event notification matrix: the IRL-team daily-summary toggle plus
// every admin's preference. channels reports which transports are wired up so
// the UI can disable an unavailable channel.
export interface EventNotifications {
  irlTeamEmail: string // configured IRL_TEAM_EMAIL ('' if unset)
  irlTeamDailySummary: boolean
  channels: MessagingChannel[]
  admins: AdminNotifPref[]
}

// NotificationsInput is the PUT payload: the IRL-team toggle plus every
// admin's chosen stream + channels (a full replace).
export interface NotificationsInput {
  irlTeamDailySummary: boolean
  admins: Array<{
    userId: string
    notifType: '' | 'daily' | 'activity'
    viaEmail: boolean
    viaSlack: boolean
  }>
}

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
  // The attendee arrives the day before and arranges their own accommodation, but
  // still wants company transport / shared-transfer consideration. Mutually
  // exclusive with extraStayStart (the company-paid night).
  extraStaySelfFunded: boolean
  allergies: string
  comments: string
  // Total personal travel cost (ticket fare / price and any other personal
  // travel spend, one figure) and its ISO-4217 currency. null / '' when not
  // provided; only meaningful for an attending=yes response.
  travelCost: number | null
  travelCostCurrency: string
  // Set once an admin has edited this response on the attendee's behalf; the
  // attendee form then becomes read-only (only admins can change it).
  locked: boolean
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
  extraStaySelfFunded: boolean
  comments: string
  travelCost: number | null
  travelCostCurrency: string
}

// --- Financial (event admin "Financial" tab) -------------------------------

// One attendee's declared travel cost in its original currency plus its value
// converted to each report target (USD/GBP/EUR). `converted` is null when live FX
// rates were unavailable.
export interface FinancialRow {
  userId: string
  name: string
  email: string
  amount: number
  currency: string
  converted: Record<string, number> | null
}

// The Financial tab payload: every payer with per-target conversions and grand
// totals. `ratesAvailable` is false when the FX API couldn't be reached — rows
// still carry their original amounts.
export interface FinancialReport {
  targets: string[]
  rows: FinancialRow[]
  totals: Record<string, number>
  ratesAvailable: boolean
  ratesAsOf: string
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
  // Classifies what was done, not who did it: 'user' = participant action (a
  // submission), 'admin' = administrative action (event config, roster,
  // reminders). An admin submitting their own attendance produces a 'user' entry.
  category: 'user' | 'admin'
  summary: string
  detail?: { changes?: ActivityChange[] }
  afterDeadline: boolean
  createdAt: string
}

// Result of a CSV attendee import: `added` are newly linked to the event,
// `skipped` covers invalid rows plus people already on the list.
export interface AttendeeImportResult {
  added: number
  skipped: number
  errors: string[]
}

export type AttendingState = 'yes' | 'no' | 'not_sure' | 'no_response'

// One row in the unified event overview. Every attendee is a company-directory
// user, so this carries the user id (for admin edits/removal) and whether they
// have ever signed in (false = provisioned by an admin import).
export interface DashboardEntry {
  userId: string
  name: string
  email: string
  attending: AttendingState
  afterDeadlineEdit: boolean
  hasLoggedIn: boolean
}

export interface Dashboard {
  total: number
  counts: { yes: number; no: number; notSure: number; noResponse: number }
  entries: DashboardEntry[]
}

export interface BuildInfo {
  name: string
  version: string
  gitCommit: string
  buildTime: string
}

// The /api/version response and the frontend's own baked-in build info share
// the same shape.
export type BackendBuildInfo = BuildInfo
