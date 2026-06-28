import { onMounted, ref, type Ref } from 'vue'
import { errMsg } from '../api'

export interface AsyncData<T> {
  data: Ref<T>
  loading: Ref<boolean>
  // Empty string when there is no error; the message otherwise (via errMsg).
  error: Ref<string>
  reload: () => Promise<void>
}

// useAsyncData collapses the loading / error / fetch triplet repeated across the
// list views into one place: it runs `fetcher` on mount (unless immediate:false),
// tracks `loading`, and funnels failures through `errMsg` into `error`. Call
// `reload()` to refetch (e.g. after a mutation, or when a query input changes).
export function useAsyncData<T>(
  fetcher: () => Promise<T>,
  initial: T,
  opts: { immediate?: boolean } = {},
): AsyncData<T> {
  const immediate = opts.immediate !== false
  const data = ref(initial) as Ref<T>
  // Start "loading" when we'll fetch on mount, so the first paint shows the
  // spinner rather than an empty state that flickers to data.
  const loading = ref(immediate)
  const error = ref('')

  async function reload() {
    loading.value = true
    error.value = ''
    try {
      data.value = await fetcher()
    } catch (e) {
      error.value = errMsg(e)
    } finally {
      loading.value = false
    }
  }

  if (immediate) onMounted(reload)

  return { data, loading, error, reload }
}
