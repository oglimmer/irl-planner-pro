import { describe, it, expect } from 'vitest'
import { useColumnSort } from './useColumnSort'

interface Row {
  name: string
  age: number
}
const rows: Row[] = [
  { name: 'Charlie', age: 30 },
  { name: 'alice', age: 25 },
  { name: 'Bob', age: 40 },
]

describe('toggleSort', () => {
  it('sorts a fresh column ascending', () => {
    const s = useColumnSort<'name'>()
    s.toggleSort('name')
    expect(s.sortKey.value).toBe('name')
    expect(s.sortDir.value).toBe('asc')
  })
  it('flips direction when the same column is clicked again', () => {
    const s = useColumnSort<'name'>()
    s.toggleSort('name')
    s.toggleSort('name')
    expect(s.sortDir.value).toBe('desc')
  })
  it('resets to ascending when switching columns', () => {
    const s = useColumnSort<'name' | 'age'>()
    s.toggleSort('name')
    s.toggleSort('name') // now desc
    s.toggleSort('age')
    expect(s.sortKey.value).toBe('age')
    expect(s.sortDir.value).toBe('asc')
  })
})

describe('indicator helpers', () => {
  it('reports isSorted / arrow / ariaSort only for the active column', () => {
    const s = useColumnSort<'name' | 'age'>()
    s.toggleSort('name')
    expect(s.isSorted('name')).toBe(true)
    expect(s.isSorted('age')).toBe(false)
    expect(s.sortArrow('name')).toBe('▲')
    expect(s.sortArrow('age')).toBe('')
    expect(s.ariaSort('name')).toBe('ascending')
    expect(s.ariaSort('age')).toBe('none')
    s.toggleSort('name')
    expect(s.sortArrow('name')).toBe('▼')
    expect(s.ariaSort('name')).toBe('descending')
  })
})

describe('sortRows', () => {
  it('keeps source order when no column is selected', () => {
    const s = useColumnSort<'name'>()
    expect(s.sortRows(rows, (r) => r.name)).toEqual(rows)
  })
  it('does not mutate the input array', () => {
    const s = useColumnSort<'age'>()
    s.toggleSort('age')
    const copy = [...rows]
    s.sortRows(rows, (r) => r.age)
    expect(rows).toEqual(copy)
  })
  it('sorts ascending then descending by a numeric value', () => {
    const s = useColumnSort<'age'>()
    s.toggleSort('age')
    expect(s.sortRows(rows, (r) => r.age).map((r) => r.age)).toEqual([25, 30, 40])
    s.toggleSort('age')
    expect(s.sortRows(rows, (r) => r.age).map((r) => r.age)).toEqual([40, 30, 25])
  })
  it('sorts by a string value (case-sensitive, matching the raw comparator)', () => {
    const s = useColumnSort<'name'>()
    s.toggleSort('name')
    // Uppercase sorts before lowercase under < on raw strings.
    expect(s.sortRows(rows, (r) => r.name).map((r) => r.name)).toEqual(['Bob', 'Charlie', 'alice'])
  })
})
