import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { api, ApiError } from './api'

// Exercises the shared request layer (authHeaders + throwForStatus) through the
// public `api` surface, with `fetch` stubbed. This is the code every call flows
// through, so it's worth pinning: token attach, pre-flight expiry, error
// extraction, 204/404 handling, and the multipart/blob helpers.

function jwt(expSecondsFromNow: number): string {
  const payload = btoa(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + expSecondsFromNow }))
  return `header.${payload}.sig`
}

interface MockResOpts {
  status?: number
  json?: unknown
  // When set, json() rejects (simulating a non-JSON body).
  nonJson?: boolean
  blob?: Blob
  statusText?: string
}
function mockRes(opts: MockResOpts = {}): Response {
  const status = opts.status ?? 200
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: opts.statusText ?? '',
    json: () => (opts.nonJson ? Promise.reject(new Error('not json')) : Promise.resolve(opts.json)),
    blob: () => Promise.resolve(opts.blob ?? new Blob()),
  } as unknown as Response
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  localStorage.clear()
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('auth header attachment', () => {
  it('adds a Bearer token and JSON Content-Type on a write', async () => {
    localStorage.setItem('token', jwt(3600))
    fetchMock.mockResolvedValue(mockRes({ json: { id: 'u1' } }))

    await api.updateMe({ firstName: 'A', lastName: 'B', allergies: '' })

    const [path, init] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/me')
    const headers = init.headers as Headers
    expect(headers.get('Authorization')).toBe(`Bearer ${jwt(3600)}`)
    expect(headers.get('Content-Type')).toBe('application/json')
  })

  it('omits Authorization when there is no token', async () => {
    fetchMock.mockResolvedValue(mockRes({ json: {} }))
    await api.authConfig()
    const headers = fetchMock.mock.calls[0][1].headers as Headers
    expect(headers.get('Authorization')).toBeNull()
  })

  it('short-circuits to 401 without hitting the network when the token is expired', async () => {
    localStorage.setItem('token', jwt(-60))
    await expect(api.me()).rejects.toMatchObject({ status: 401 })
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('error handling (throwForStatus)', () => {
  it('prefers the JSON error field', async () => {
    fetchMock.mockResolvedValue(mockRes({ status: 422, json: { error: 'bad slug' } }))
    await expect(api.getEvent('x')).rejects.toMatchObject({ status: 422, message: 'bad slug' })
  })

  it('falls back to status text for a non-JSON body', async () => {
    fetchMock.mockResolvedValue(mockRes({ status: 500, nonJson: true, statusText: 'Server Error' }))
    const err = await api.getEvent('x').catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(500)
    expect(err.message).toBe('Server Error')
  })
})

describe('response decoding', () => {
  it('returns undefined for 204 No Content', async () => {
    localStorage.setItem('token', jwt(3600))
    fetchMock.mockResolvedValue(mockRes({ status: 204 }))
    await expect(api.promoteUser('u1')).resolves.toBeUndefined()
  })

  it('maps a 404 submission to null (rest rethrow)', async () => {
    localStorage.setItem('token', jwt(3600))
    fetchMock.mockResolvedValue(mockRes({ status: 404, json: { error: 'none' } }))
    await expect(api.getMySubmission('slug')).resolves.toBeNull()

    fetchMock.mockResolvedValue(mockRes({ status: 500, json: { error: 'boom' } }))
    await expect(api.getMySubmission('slug')).rejects.toMatchObject({ status: 500 })
  })
})

describe('multipart / blob helpers share the same auth + error path', () => {
  it('imports attendees as multipart with the bearer token', async () => {
    localStorage.setItem('token', jwt(3600))
    fetchMock.mockResolvedValue(mockRes({ json: { added: 2, skipped: 1, errors: [] } }))

    const file = new File(['name,email'], 'a.csv', { type: 'text/csv' })
    const res = await api.importAttendees('e1', file)

    expect(res).toEqual({ added: 2, skipped: 1, errors: [] })
    const [path, init] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/admin/events/e1/attendees')
    expect(init.method).toBe('POST')
    expect(init.body).toBeInstanceOf(FormData)
    expect((init.headers as Headers).get('Authorization')).toBe(`Bearer ${jwt(3600)}`)
    // Multipart: the browser sets the boundary Content-Type, so we must NOT.
    expect((init.headers as Headers).get('Content-Type')).toBeNull()
  })

  it('builds the export query and returns a blob', async () => {
    localStorage.setItem('token', jwt(3600))
    const blob = new Blob(['csv'], { type: 'text/csv' })
    fetchMock.mockResolvedValue(mockRes({ blob }))

    const out = await api.fetchExport('e1', ['yes', 'no'])
    expect(out).toBe(blob)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/admin/events/e1/export.csv?attending=yes,no')
  })

  it('surfaces an export failure as an ApiError', async () => {
    localStorage.setItem('token', jwt(3600))
    fetchMock.mockResolvedValue(mockRes({ status: 403, json: { error: 'forbidden' } }))
    await expect(api.fetchExport('e1', [])).rejects.toMatchObject({ status: 403, message: 'forbidden' })
  })
})
