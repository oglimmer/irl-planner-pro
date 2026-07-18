// Theme registry + DOM/localStorage plumbing.
//
// A theme is purely a palette swap: each id corresponds to an
// `html[data-theme="…"]` block in styles.css (except `light`, which is the
// :root default). We set `data-theme` on <html> and persist the choice to
// localStorage so it survives reloads and applies instantly on next boot.
//
// NOTE: index.html contains a tiny inline pre-boot copy of this logic to avoid
// a flash-of-wrong-theme before the bundle loads. If THEMES changes here,
// update the id list in index.html too.

export interface ThemeOption {
  id: string
  label: string
}

export const THEMES: ThemeOption[] = [
  { id: 'light', label: 'Light' },
  { id: 'dark', label: 'Dark' },
  { id: 'midnight', label: 'Midnight' },
  { id: 'sepia', label: 'Sepia' },
  { id: 'contrast', label: 'High contrast' },
]

export const DEFAULT_THEME = 'light'

const THEME_IDS = new Set(THEMES.map((t) => t.id))
const STORAGE_KEY = 'theme'

export function isValidTheme(value: unknown): value is string {
  return typeof value === 'string' && THEME_IDS.has(value)
}

// normalizeTheme coerces any input to a known theme id, falling back to the
// default — so a stale/garbage localStorage value can never leave the UI in an
// unstyled state.
export function normalizeTheme(value: unknown): string {
  return isValidTheme(value) ? value : DEFAULT_THEME
}

// applyTheme writes the active theme onto <html>, which is what the CSS keys
// off. Safe to call before mount and on every change.
export function applyTheme(theme: string): void {
  document.documentElement.dataset.theme = normalizeTheme(theme)
}

export function getStoredTheme(): string {
  try {
    return normalizeTheme(localStorage.getItem(STORAGE_KEY))
  } catch {
    return DEFAULT_THEME
  }
}

export function setStoredTheme(theme: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, normalizeTheme(theme))
  } catch {
    // Private-mode / disabled storage: the in-memory data-theme still applies
    // for this session; persistence is best-effort.
  }
}
