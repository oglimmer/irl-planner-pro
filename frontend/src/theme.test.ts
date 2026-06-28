import { describe, it, expect, beforeEach } from 'vitest'
import {
  DEFAULT_THEME,
  THEMES,
  applyTheme,
  getStoredTheme,
  isValidTheme,
  normalizeTheme,
  setStoredTheme,
} from './theme'

describe('isValidTheme', () => {
  it('accepts every registered theme id', () => {
    for (const t of THEMES) expect(isValidTheme(t.id)).toBe(true)
  })
  it('rejects unknown ids and non-strings', () => {
    expect(isValidTheme('neon')).toBe(false)
    expect(isValidTheme(null)).toBe(false)
    expect(isValidTheme(42)).toBe(false)
    expect(isValidTheme(undefined)).toBe(false)
  })
})

describe('normalizeTheme', () => {
  it('passes through a valid id', () => {
    expect(normalizeTheme('dark')).toBe('dark')
  })
  it('falls back to the default for garbage', () => {
    expect(normalizeTheme('neon')).toBe(DEFAULT_THEME)
    expect(normalizeTheme(null)).toBe(DEFAULT_THEME)
  })
})

describe('applyTheme', () => {
  it('writes the normalized theme onto <html>', () => {
    applyTheme('midnight')
    expect(document.documentElement.dataset.theme).toBe('midnight')
    applyTheme('garbage')
    expect(document.documentElement.dataset.theme).toBe(DEFAULT_THEME)
  })
})

describe('stored theme round-trip', () => {
  beforeEach(() => localStorage.clear())

  it('returns the default when nothing is stored', () => {
    expect(getStoredTheme()).toBe(DEFAULT_THEME)
  })
  it('persists and reloads a valid theme', () => {
    setStoredTheme('sepia')
    expect(getStoredTheme()).toBe('sepia')
  })
  it('normalizes a stale stored value on read', () => {
    localStorage.setItem('theme', 'neon')
    expect(getStoredTheme()).toBe(DEFAULT_THEME)
  })
})
