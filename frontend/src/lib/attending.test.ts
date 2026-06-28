import { describe, it, expect } from 'vitest'
import { attendingLabels, attendingRank, matchesQuery } from './attending'
import type { AttendingState } from '../types'

describe('attending maps', () => {
  it('labels and ranks every state', () => {
    const states: AttendingState[] = ['yes', 'no', 'not_sure', 'no_response']
    for (const s of states) {
      expect(attendingLabels[s]).toBeTruthy()
      expect(typeof attendingRank[s]).toBe('number')
    }
  })

  it('ranks states in pipeline order (yes → not_sure → no → no_response)', () => {
    const order = (['yes', 'no', 'not_sure', 'no_response'] as AttendingState[])
      .slice()
      .sort((a, b) => attendingRank[a] - attendingRank[b])
    expect(order).toEqual(['yes', 'not_sure', 'no', 'no_response'])
  })
})

describe('matchesQuery', () => {
  it('matches case-insensitively across any provided field', () => {
    expect(matchesQuery('ann', 'Anna Smith', 'anna@id5.io')).toBe(true)
    expect(matchesQuery('id5.io', 'Anna Smith', 'anna@id5.io')).toBe(true)
  })
  it('returns false when no field contains the query', () => {
    expect(matchesQuery('zzz', 'Anna Smith', 'anna@id5.io')).toBe(false)
  })
  it('tolerates undefined fields', () => {
    expect(matchesQuery('anna', undefined, 'anna@id5.io')).toBe(true)
    expect(matchesQuery('anna', undefined, undefined)).toBe(false)
  })
})
