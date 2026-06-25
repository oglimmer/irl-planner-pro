import { onMounted, onUnmounted, ref, watch, type Ref } from 'vue'

export interface AutoReloadOption {
  label: string
  ms: number // 0 = off
}

export const AUTO_RELOAD_OPTIONS: AutoReloadOption[] = [
  { label: '5s', ms: 5000 },
  { label: '15s', ms: 15000 },
  { label: '1m', ms: 60000 },
  { label: '5m', ms: 300000 },
  { label: 'Off', ms: 0 },
]

// useAutoReload polls fetchFn on the selected interval. It runs fetchFn once on
// mount, reschedules whenever the interval changes, pauses while the tab is
// hidden (resuming with an immediate refresh), and cleans up on unmount.
// Default interval is 1 minute (per the dashboard spec).
export function useAutoReload(fetchFn: () => void | Promise<void>) {
  const intervalMs = ref<number>(60000)
  let timer: ReturnType<typeof setInterval> | null = null

  function clear() {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  }

  function schedule() {
    clear()
    if (intervalMs.value > 0 && !document.hidden) {
      timer = setInterval(() => {
        void fetchFn()
      }, intervalMs.value)
    }
  }

  function onVisibility() {
    if (document.hidden) {
      clear()
    } else {
      void fetchFn() // refresh immediately on return, then resume polling
      schedule()
    }
  }

  watch(intervalMs, schedule)

  onMounted(() => {
    void fetchFn()
    schedule()
    document.addEventListener('visibilitychange', onVisibility)
  })

  onUnmounted(() => {
    clear()
    document.removeEventListener('visibilitychange', onVisibility)
  })

  return { intervalMs, options: AUTO_RELOAD_OPTIONS } as {
    intervalMs: Ref<number>
    options: AutoReloadOption[]
  }
}
