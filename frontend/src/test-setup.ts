// Vitest global setup. Node's experimental built-in `localStorage` global is
// disabled unless `--localstorage-file` is passed, and it shadows jsdom's
// `window.localStorage`. The app uses the bare `localStorage` global (which is
// real in browsers), so we install a small in-memory Storage shim here so
// storage-backed code (theme, column config, auth store) is testable.
class MemoryStorage implements Storage {
  private store = new Map<string, string>()
  get length(): number {
    return this.store.size
  }
  clear(): void {
    this.store.clear()
  }
  getItem(key: string): string | null {
    return this.store.has(key) ? this.store.get(key)! : null
  }
  key(index: number): string | null {
    return [...this.store.keys()][index] ?? null
  }
  removeItem(key: string): void {
    this.store.delete(key)
  }
  setItem(key: string, value: string): void {
    this.store.set(key, String(value))
  }
}

const storage = new MemoryStorage()
Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
}
