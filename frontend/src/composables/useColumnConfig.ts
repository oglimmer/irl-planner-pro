import { ref, watch, type Ref } from 'vue'

// useColumnConfig drives a configurable data table: which columns the user has
// chosen to show, persisted per-browser in localStorage so the choice survives
// reloads. Unknown keys in storage (e.g. a column removed in a later release) are
// dropped on load, and an empty/garbage value falls back to the supplied defaults.
// At least one column is always kept selected so the table never renders headless.
export function useColumnConfig(
  storageKey: string,
  allKeys: readonly string[],
  defaults: readonly string[],
): { selected: Ref<string[]>; reset: () => void } {
  const valid = new Set(allKeys)

  function load(): string[] {
    try {
      const raw = localStorage.getItem(storageKey)
      if (raw) {
        const parsed = JSON.parse(raw)
        if (Array.isArray(parsed)) {
          const filtered = parsed.filter((k): k is string => typeof k === 'string' && valid.has(k))
          if (filtered.length) return filtered
        }
      }
    } catch {
      // malformed/unavailable storage — fall through to defaults
    }
    return [...defaults]
  }

  const selected = ref<string[]>(load())

  watch(
    selected,
    (v) => {
      try {
        localStorage.setItem(storageKey, JSON.stringify(v))
      } catch {
        // storage unavailable (private mode / quota) — selection still works in-session
      }
    },
    { deep: true },
  )

  function reset() {
    selected.value = [...defaults]
  }

  return { selected, reset }
}
