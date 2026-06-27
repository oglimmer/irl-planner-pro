import { ref } from 'vue'

export interface ConfirmOptions {
  message: string
  title?: string
  confirmLabel?: string
  cancelLabel?: string
  // danger styles the confirm button as destructive and focuses Cancel by
  // default, so an accidental Enter does not trigger the irreversible action.
  danger?: boolean
  // 'warning' renders a loud, high-visibility alert (warning icon, red banner)
  // for prompts the user must not skim past — e.g. an after-deadline edit that
  // gets flagged to the People team.
  variant?: 'default' | 'warning'
}

interface ConfirmState {
  open: boolean
  message: string
  title: string
  confirmLabel: string
  cancelLabel: string
  danger: boolean
  variant: 'default' | 'warning'
}

// Single shared dialog: one <ConfirmDialog> is mounted in App.vue and every
// caller drives it through this module-level state. confirm() returns a promise
// that resolves true on accept and false on cancel/dismiss.
const state = ref<ConfirmState>({
  open: false,
  message: '',
  title: '',
  confirmLabel: 'Confirm',
  cancelLabel: 'Cancel',
  danger: false,
  variant: 'default',
})

let resolver: ((ok: boolean) => void) | null = null

function settle(ok: boolean) {
  if (resolver) {
    resolver(ok)
    resolver = null
  }
  state.value.open = false
}

function confirm(options: ConfirmOptions): Promise<boolean> {
  // If a prompt is somehow already open, dismiss it as cancelled first.
  settle(false)
  state.value = {
    open: true,
    message: options.message,
    title: options.title ?? '',
    confirmLabel: options.confirmLabel ?? 'Confirm',
    cancelLabel: options.cancelLabel ?? 'Cancel',
    danger: options.danger ?? false,
    variant: options.variant ?? 'default',
  }
  return new Promise<boolean>((resolve) => {
    resolver = resolve
  })
}

// For callers that need to ask a question.
export function useConfirm() {
  return { confirm }
}

// For the single dialog component that renders the prompt.
export function useConfirmDialog() {
  return {
    state,
    accept: () => settle(true),
    cancel: () => settle(false),
  }
}
