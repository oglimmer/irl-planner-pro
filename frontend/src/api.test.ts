import { describe, it, expect } from 'vitest'
import { isJwtExpired, ApiError, errMsg, errStatus } from './api'

function jwt(expSecondsFromNow: number): string {
  const payload = btoa(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + expSecondsFromNow }))
  return `header.${payload}.sig`
}

describe('isJwtExpired', () => {
  it('is true for a token whose exp is in the past', () => {
    expect(isJwtExpired(jwt(-60))).toBe(true)
  })
  it('is false for a token still valid', () => {
    expect(isJwtExpired(jwt(3600))).toBe(false)
  })
  it('is false for a malformed token (server stays the source of truth)', () => {
    expect(isJwtExpired('not-a-jwt')).toBe(false)
  })
})

describe('ApiError helpers', () => {
  it('extracts message and status', () => {
    const e = new ApiError(404, 'not found')
    expect(errMsg(e)).toBe('not found')
    expect(errStatus(e)).toBe(404)
  })
  it('errStatus is undefined for non-API errors', () => {
    expect(errStatus(new Error('boom'))).toBeUndefined()
  })
})
