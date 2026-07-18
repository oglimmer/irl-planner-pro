import { describe, it, expect, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import { useColumnConfig } from './useColumnConfig'

const ALL = ['name', 'email', 'attending', 'logged'] as const
const DEFAULTS = ['name', 'attending'] as const
const KEY = 'test-columns'

describe('useColumnConfig', () => {
  beforeEach(() => localStorage.clear())

  it('starts from the defaults when nothing is stored', () => {
    const { selected } = useColumnConfig(KEY, ALL, DEFAULTS)
    expect(selected.value).toEqual(['name', 'attending'])
  })

  it('restores a valid stored selection', () => {
    localStorage.setItem(KEY, JSON.stringify(['email', 'logged']))
    const { selected } = useColumnConfig(KEY, ALL, DEFAULTS)
    expect(selected.value).toEqual(['email', 'logged'])
  })

  it('drops unknown keys from stored selection', () => {
    localStorage.setItem(KEY, JSON.stringify(['email', 'ghost', 'logged']))
    const { selected } = useColumnConfig(KEY, ALL, DEFAULTS)
    expect(selected.value).toEqual(['email', 'logged'])
  })

  it('falls back to defaults when storage has only invalid keys', () => {
    localStorage.setItem(KEY, JSON.stringify(['ghost']))
    const { selected } = useColumnConfig(KEY, ALL, DEFAULTS)
    expect(selected.value).toEqual([...DEFAULTS])
  })

  it('falls back to defaults on malformed JSON', () => {
    localStorage.setItem(KEY, '{not json')
    const { selected } = useColumnConfig(KEY, ALL, DEFAULTS)
    expect(selected.value).toEqual([...DEFAULTS])
  })

  it('persists changes to localStorage', async () => {
    const { selected } = useColumnConfig(KEY, ALL, DEFAULTS)
    selected.value = ['email']
    await nextTick()
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual(['email'])
  })

  it('reset restores (a fresh copy of) the defaults', () => {
    const { selected, reset } = useColumnConfig(KEY, ALL, DEFAULTS)
    selected.value = ['email']
    reset()
    expect(selected.value).toEqual([...DEFAULTS])
    expect(selected.value).not.toBe(DEFAULTS) // copy, not the same reference
  })
})
