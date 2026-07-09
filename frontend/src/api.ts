import type {
  ActiveEvent,
  ActivityEntry,
  AuthConfig,
  BackendBuildInfo,
  AttendeeImportResult,
  Dashboard,
  Event,
  EventInput,
  FinancialReport,
  EventNotifications,
  MessageTemplates,
  NotificationsInput,
  MessagingStatus,
  ProfileInput,
  SendMessageResult,
  Submission,
  SubmissionInput,
  User,
  UserSummary,
} from './types'

function token(): string | null {
  return localStorage.getItem('token')
}

// isJwtExpired decodes a JWT's payload and reports whether its `exp` claim is
// already in the past. Anything it can't confidently read as expired returns
// false so the server stays the source of truth in ambiguous cases.
export function isJwtExpired(tok: string): boolean {
  const parts = tok.split('.')
  if (parts.length !== 3) return false
  try {
    const json = atob(parts[1].replace(/-/g, '+').replace(/_/g, '/'))
    const claims = JSON.parse(json) as { exp?: number }
    if (typeof claims.exp !== 'number') return false
    return claims.exp * 1000 <= Date.now()
  } catch {
    return false
  }
}

// ApiError carries the HTTP status alongside the message so callers can react
// to what kind of failure occurred (404 vs 500 pages, 401 → login, etc.).
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export function errMsg(e: unknown, fallback = 'something went wrong'): string {
  if (e instanceof Error) return e.message || fallback
  if (typeof e === 'string') return e
  return fallback
}

export function errStatus(e: unknown): number | undefined {
  return e instanceof ApiError ? e.status : undefined
}

// authHeaders builds request headers with the bearer token attached. It bails
// before any network call once the token is past its exp — the server would only
// answer 401 anyway. Shared by the JSON `request` path and the multipart/blob
// helpers below so the token handling lives in exactly one place.
function authHeaders(init?: HeadersInit): Headers {
  const headers = new Headers(init)
  const t = token()
  if (t) {
    if (isJwtExpired(t)) throw new ApiError(401, 'session expired')
    headers.set('Authorization', `Bearer ${t}`)
  }
  return headers
}

// throwForStatus turns a non-2xx response into an ApiError, preferring the JSON
// `error` field the backend sends and falling back to the status text.
async function throwForStatus(res: Response): Promise<void> {
  if (res.ok) return
  let msg = res.statusText
  try {
    const data = await res.json()
    if (data && data.error) msg = data.error
  } catch {
    // non-JSON body — keep the status text
  }
  throw new ApiError(res.status, msg)
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers = authHeaders(opts.headers)
  if (opts.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(path, { ...opts, headers })
  await throwForStatus(res)
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  version: () => request<BackendBuildInfo>('/api/version'),
  authConfig: () => request<AuthConfig>('/api/auth/config'),
  me: () => request<User>('/api/me'),
  // Self-service profile edit (display name + allergies).
  updateMe: (data: ProfileInput) =>
    request<User>('/api/me', { method: 'PUT', body: JSON.stringify(data) }),

  // Dev-only password-mode login (AUTH_MODE=password).
  devLogin: (email: string, firstName: string, lastName: string, allergies: string) =>
    request<{ token: string; user: User }>('/api/auth/dev-login', {
      method: 'POST',
      body: JSON.stringify({ email, firstName, lastName, allergies }),
    }),

  // Admin user management.
  listUsers: () => request<UserSummary[]>('/api/users'),
  promoteUser: (id: string) =>
    request<void>(`/api/users/${id}/promote`, { method: 'POST' }),
  demoteUser: (id: string) =>
    request<void>(`/api/users/${id}/demote`, { method: 'POST' }),
  archiveUser: (id: string) =>
    request<void>(`/api/users/${id}/archive`, { method: 'POST' }),
  unarchiveUser: (id: string) =>
    request<void>(`/api/users/${id}/unarchive`, { method: 'POST' }),
  sendTestNotification: (channel: 'email' | 'slack') =>
    request<{ status: string; to: string }>(
      `/api/notifications/test/${channel}`,
      { method: 'POST' },
    ),

  // Current (non-past) events for the post-login landing, annotated with the
  // caller's RSVP state. Every signed-in user is invited (company offsite tool).
  activeEvents: () => request<ActiveEvent[]>('/api/active-events'),

  // Attendee-facing event read (the shareable URL).
  getEventBySlug: (slug: string) => request<Event>(`/api/events/${slug}`),
  // Returns null when the caller hasn't submitted yet (404 → null).
  getMySubmission: async (slug: string): Promise<Submission | null> => {
    try {
      return await request<Submission>(`/api/events/${slug}/submission`)
    } catch (e) {
      if (errStatus(e) === 404) return null
      throw e
    }
  },
  putMySubmission: (slug: string, data: SubmissionInput) =>
    request<Submission>(`/api/events/${slug}/submission`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  myActivity: (slug: string) =>
    request<ActivityEntry[]>(`/api/events/${slug}/activity`),

  // Admin event management.
  listEvents: (scope: 'current' | 'past' | 'all' = 'current') =>
    request<Event[]>(`/api/admin/events?scope=${scope}`),
  getEvent: (id: string) => request<Event>(`/api/admin/events/${id}`),
  createEvent: (data: EventInput) =>
    request<Event>('/api/admin/events', { method: 'POST', body: JSON.stringify(data) }),
  updateEvent: (id: string, data: EventInput) =>
    request<Event>(`/api/admin/events/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  // detailed=true expands campaign sends into per-recipient/channel delivery
  // entries (channel + status set on each), for the admin timeline view.
  eventActivity: (id: string, detailed = false) =>
    request<ActivityEntry[]>(
      `/api/admin/events/${id}/activity${detailed ? '?detailed=true' : ''}`,
    ),
  // lock=true saves and locks the response (attendee can no longer self-edit);
  // lock=false is a plain save that leaves it attendee-editable. The lock is
  // sticky server-side, so a plain save never unlocks an already-locked response.
  adminUpdateSubmission: (id: string, userId: string, data: SubmissionInput, lock: boolean) =>
    request<Submission>(`/api/admin/events/${id}/submissions/${userId}?lock=${lock}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),

  // Attendees (event membership) + dashboard + export.
  // importAttendees uploads a name,email CSV; it provisions directory users and
  // adds them to the event (additive — never removes anyone).
  importAttendees: async (id: string, file: File): Promise<AttendeeImportResult> => {
    const form = new FormData()
    form.append('file', file)
    const res = await fetch(`/api/admin/events/${id}/attendees`, {
      method: 'POST',
      headers: authHeaders(),
      body: form,
    })
    await throwForStatus(res)
    return res.json() as Promise<AttendeeImportResult>
  },
  addAttendee: (id: string, userId: string) =>
    request<void>(`/api/admin/events/${id}/attendees/${userId}`, { method: 'POST' }),
  removeAttendee: (id: string, userId: string) =>
    request<void>(`/api/admin/events/${id}/attendees/${userId}`, { method: 'DELETE' }),
  // Event cover image (admin). Upload returns the new image URL (with its
  // cache-busting ?v= etag); delete is a fire-and-forget 204.
  uploadEventImage: async (id: string, file: File): Promise<{ imageUrl: string }> => {
    const form = new FormData()
    form.append('image', file)
    const res = await fetch(`/api/admin/events/${id}/image`, {
      method: 'POST',
      headers: authHeaders(),
      body: form,
    })
    await throwForStatus(res)
    return res.json() as Promise<{ imageUrl: string }>
  },
  deleteEventImage: (id: string) =>
    request<void>(`/api/admin/events/${id}/image`, { method: 'DELETE' }),

  // Messaging tab: templates, audience stats, channel availability; an
  // admin-pressed invitation to all attendees; a manual follow-up to current
  // non-responders. Both deliver over every configured channel (email + Slack).
  getMessaging: (id: string) => request<MessagingStatus>(`/api/admin/events/${id}/messaging`),
  saveMessaging: (id: string, data: MessageTemplates) =>
    request<MessageTemplates>(`/api/admin/events/${id}/messaging`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  sendInvitation: (id: string) =>
    request<SendMessageResult>(`/api/admin/events/${id}/messaging/invite`, { method: 'POST' }),
  sendFollowup: (id: string) =>
    request<SendMessageResult>(`/api/admin/events/${id}/messaging/followup`, { method: 'POST' }),

  // Per-event notification matrix: IRL team daily-summary toggle + each
  // admin's stream and channel preferences.
  getNotifications: (id: string) =>
    request<EventNotifications>(`/api/admin/events/${id}/notifications`),
  saveNotifications: (id: string, data: NotificationsInput) =>
    request<EventNotifications>(`/api/admin/events/${id}/notifications`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),

  dashboard: (id: string) => request<Dashboard>(`/api/admin/events/${id}/dashboard`),
  listSubmissions: (id: string) => request<Submission[]>(`/api/admin/events/${id}/submissions`),
  // Financial tab: travel costs converted to USD/GBP/EUR via live FX rates.
  financial: (id: string) => request<FinancialReport>(`/api/admin/events/${id}/financial`),
  // Fetches the filter-driven export as a Blob (the endpoint needs the bearer
  // header, so a plain <a> download can't be used). states = attending subset.
  fetchExport: async (id: string, states: string[]): Promise<Blob> => {
    const q = states.length ? `?attending=${states.join(',')}` : ''
    const res = await fetch(`/api/admin/events/${id}/export.csv${q}`, { headers: authHeaders() })
    await throwForStatus(res)
    return res.blob()
  },
}
