import type {
  ActiveEvent,
  ActivityEntry,
  AuthConfig,
  BackendBuildInfo,
  Dashboard,
  Event,
  EventInput,
  RosterEntry,
  RosterUploadResult,
  ProfileInput,
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

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers = new Headers(opts.headers)
  if (opts.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const t = token()
  if (t) {
    // Bail before the network call once the token is past its exp — the server
    // would only answer 401 anyway.
    if (isJwtExpired(t)) throw new ApiError(401, 'session expired')
    headers.set('Authorization', `Bearer ${t}`)
  }
  const res = await fetch(path, { ...opts, headers })
  if (!res.ok) {
    let msg = res.statusText
    try {
      const data = await res.json()
      if (data && data.error) msg = data.error
    } catch {
      // non-JSON body — keep the status text
    }
    throw new ApiError(res.status, msg)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  version: () => request<BackendBuildInfo>('/api/version'),
  authConfig: () => request<AuthConfig>('/api/auth/config'),
  me: () => request<User>('/api/me'),
  // Self-service profile edit (display name).
  updateMe: (data: ProfileInput) =>
    request<User>('/api/me', { method: 'PUT', body: JSON.stringify(data) }),

  // Dev-only password-mode login (AUTH_MODE=password).
  devLogin: (email: string, firstName: string, lastName: string) =>
    request<{ token: string; user: User }>('/api/auth/dev-login', {
      method: 'POST',
      body: JSON.stringify({ email, firstName, lastName }),
    }),

  // Admin user management.
  listUsers: () => request<UserSummary[]>('/api/users'),
  promoteUser: (id: string) =>
    request<void>(`/api/users/${id}/promote`, { method: 'POST' }),
  demoteUser: (id: string) =>
    request<void>(`/api/users/${id}/demote`, { method: 'POST' }),

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
  eventActivity: (id: string) =>
    request<ActivityEntry[]>(`/api/admin/events/${id}/activity`),
  adminUpdateSubmission: (id: string, userId: string, data: SubmissionInput) =>
    request<Submission>(`/api/admin/events/${id}/submissions/${userId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),

  // Roster + dashboard + export.
  listRoster: (id: string) => request<RosterEntry[]>(`/api/admin/events/${id}/roster`),
  uploadRoster: async (id: string, file: File): Promise<RosterUploadResult> => {
    const form = new FormData()
    form.append('file', file)
    const headers = new Headers()
    const t = localStorage.getItem('token')
    if (t) {
      if (isJwtExpired(t)) throw new ApiError(401, 'session expired')
      headers.set('Authorization', `Bearer ${t}`)
    }
    const res = await fetch(`/api/admin/events/${id}/roster`, { method: 'POST', headers, body: form })
    if (!res.ok) {
      let msg = res.statusText
      try {
        const data = await res.json()
        if (data && data.error) msg = data.error
      } catch {
        // non-JSON body
      }
      throw new ApiError(res.status, msg)
    }
    return res.json() as Promise<RosterUploadResult>
  },
  dashboard: (id: string) => request<Dashboard>(`/api/admin/events/${id}/dashboard`),
  listSubmissions: (id: string) => request<Submission[]>(`/api/admin/events/${id}/submissions`),
  // Fetches the filter-driven export as a Blob (the endpoint needs the bearer
  // header, so a plain <a> download can't be used). states = attending subset.
  fetchExport: async (id: string, states: string[]): Promise<Blob> => {
    const q = states.length ? `?attending=${states.join(',')}` : ''
    const headers = new Headers()
    const t = localStorage.getItem('token')
    if (t) {
      if (isJwtExpired(t)) throw new ApiError(401, 'session expired')
      headers.set('Authorization', `Bearer ${t}`)
    }
    const res = await fetch(`/api/admin/events/${id}/export.csv${q}`, { headers })
    if (!res.ok) throw new ApiError(res.status, res.statusText)
    return res.blob()
  },
}
