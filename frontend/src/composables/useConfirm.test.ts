import { describe, it, expect } from 'vitest'
import { useConfirm, useConfirmDialog } from './useConfirm'

describe('useConfirm', () => {
  it('opens the shared dialog with the given options', () => {
    const { confirm } = useConfirm()
    const { state } = useConfirmDialog()

    void confirm({ message: 'Delete it?', title: 'Sure?', confirmLabel: 'Yes', danger: true })

    expect(state.value.open).toBe(true)
    expect(state.value.message).toBe('Delete it?')
    expect(state.value.title).toBe('Sure?')
    expect(state.value.confirmLabel).toBe('Yes')
    expect(state.value.danger).toBe(true)
  })

  it('resolves true and closes on accept', async () => {
    const { confirm } = useConfirm()
    const { state, accept } = useConfirmDialog()

    const p = confirm({ message: 'ok?' })
    accept()

    expect(await p).toBe(true)
    expect(state.value.open).toBe(false)
  })

  it('resolves false and closes on cancel', async () => {
    const { confirm } = useConfirm()
    const { state, cancel } = useConfirmDialog()

    const p = confirm({ message: 'ok?' })
    cancel()

    expect(await p).toBe(false)
    expect(state.value.open).toBe(false)
  })

  it('defaults labels and danger when omitted', () => {
    const { confirm } = useConfirm()
    const { state, cancel } = useConfirmDialog()

    void confirm({ message: 'plain' })

    expect(state.value.confirmLabel).toBe('Confirm')
    expect(state.value.cancelLabel).toBe('Cancel')
    expect(state.value.danger).toBe(false)
    cancel()
  })

  it('cancels a still-open prompt when a new one is requested', async () => {
    const { confirm } = useConfirm()
    const { accept } = useConfirmDialog()

    const first = confirm({ message: 'first' })
    const second = confirm({ message: 'second' })

    expect(await first).toBe(false) // superseded
    accept()
    expect(await second).toBe(true)
  })
})
