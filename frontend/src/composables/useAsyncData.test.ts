import { describe, it, expect } from 'vitest'
import { defineComponent, type Ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useAsyncData, type AsyncData } from './useAsyncData'
import { ApiError } from '../api'

// Mount a throwaway component so onMounted fires, exposing the composable's
// return value for assertions.
function withComposable<T>(factory: () => AsyncData<T>) {
  let exposed!: AsyncData<T>
  const wrapper = mount(
    defineComponent({
      setup() {
        exposed = factory()
        return () => null
      },
    }),
  )
  return { exposed, wrapper }
}

describe('useAsyncData', () => {
  it('fetches on mount and tracks loading', async () => {
    const { exposed } = withComposable(() => useAsyncData(() => Promise.resolve([1, 2, 3]), [] as number[]))
    // loading is true synchronously during the initial fetch
    expect(exposed.loading.value).toBe(true)
    await flushPromises()
    expect(exposed.loading.value).toBe(false)
    expect(exposed.data.value).toEqual([1, 2, 3])
    expect(exposed.error.value).toBe('')
  })

  it('captures the error message and keeps the initial data', async () => {
    const { exposed } = withComposable(() =>
      useAsyncData<number[]>(() => Promise.reject(new ApiError(500, 'kaboom')), []),
    )
    await flushPromises()
    expect(exposed.error.value).toBe('kaboom')
    expect(exposed.data.value).toEqual([])
    expect(exposed.loading.value).toBe(false)
  })

  it('clears a prior error on reload', async () => {
    let fail = true
    const { exposed } = withComposable(() =>
      useAsyncData<string>(
        () => (fail ? Promise.reject(new ApiError(500, 'boom')) : Promise.resolve('ok')),
        '',
      ),
    )
    await flushPromises()
    expect(exposed.error.value).toBe('boom')
    fail = false
    await exposed.reload()
    expect(exposed.error.value).toBe('')
    expect(exposed.data.value).toBe('ok')
  })

  it('does not fetch on mount when immediate is false', async () => {
    let calls = 0
    const { exposed } = withComposable(() =>
      useAsyncData(
        () => {
          calls++
          return Promise.resolve('x')
        },
        '',
        { immediate: false },
      ),
    )
    await flushPromises()
    expect(calls).toBe(0)
    expect(exposed.loading.value).toBe(false)
    await exposed.reload()
    expect(calls).toBe(1)
    expect((exposed.data as Ref<string>).value).toBe('x')
  })
})
