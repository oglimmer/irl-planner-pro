import { ref, type Ref } from 'vue'

export type SortDir = 'asc' | 'desc'

// Client-side column sort shared by the dashboard's tables. A null sortKey keeps
// the source order; clicking a header sorts that column ascending, clicking the
// same header again flips to descending (the usual toggle). Helpers are exposed
// as methods (not raw refs) so they unwrap correctly when called in a template.
export function useColumnSort<K extends string>(initialKey: K | null = null) {
  const sortKey = ref(initialKey) as Ref<K | null>
  const sortDir = ref<SortDir>('asc')

  function toggleSort(key: K) {
    if (sortKey.value === key) {
      sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey.value = key
      sortDir.value = 'asc'
    }
  }

  const isSorted = (key: K) => sortKey.value === key

  function sortArrow(key: K): string {
    if (sortKey.value !== key) return ''
    return sortDir.value === 'asc' ? '▲' : '▼'
  }

  function ariaSort(key: K): 'ascending' | 'descending' | 'none' {
    if (sortKey.value !== key) return 'none'
    return sortDir.value === 'asc' ? 'ascending' : 'descending'
  }

  // Returns a sorted copy (never mutates the input) ordered by a per-row
  // comparable value. Reading the refs here makes callers reactive to changes.
  function sortRows<T>(rows: T[], value: (row: T, key: K) => string | number): T[] {
    if (!sortKey.value) return rows
    const key = sortKey.value
    const dir = sortDir.value === 'asc' ? 1 : -1
    return [...rows].sort((a, b) => {
      const av = value(a, key)
      const bv = value(b, key)
      if (av < bv) return -dir
      if (av > bv) return dir
      return 0
    })
  }

  return { sortKey, sortDir, toggleSort, isSorted, sortArrow, ariaSort, sortRows }
}
