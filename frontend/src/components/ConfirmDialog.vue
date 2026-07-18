<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useConfirmDialog } from '../composables/useConfirm'

const { state, accept, cancel } = useConfirmDialog()
const box = ref<HTMLElement | null>(null)
const confirmBtn = ref<HTMLButtonElement | null>(null)
const cancelBtn = ref<HTMLButtonElement | null>(null)

// On open, focus the safe action for destructive prompts (Cancel) and the
// primary action otherwise, so a stray Enter does the least harmful thing.
watch(
  () => state.value.open,
  (open) => {
    if (open) {
      void nextTick(() => (state.value.danger ? cancelBtn : confirmBtn).value?.focus())
    }
  },
)

// Keep Tab focus inside the modal while it's open: at the edges, wrap to the
// other end instead of letting focus escape to the page behind the overlay.
function trapTab(e: KeyboardEvent) {
  const focusables = box.value?.querySelectorAll<HTMLElement>(
    'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
  )
  if (!focusables || focusables.length === 0) return
  const first = focusables[0]
  const last = focusables[focusables.length - 1]
  const active = document.activeElement
  if (e.shiftKey && active === first) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && active === last) {
    e.preventDefault()
    first.focus()
  }
}

function onKeydown(e: KeyboardEvent) {
  if (!state.value.open) return
  if (e.key === 'Escape') cancel()
  else if (e.key === 'Tab') trapTab(e)
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div
      v-if="state.open"
      class="confirm-overlay"
      @click.self="cancel"
    >
      <div
        ref="box"
        class="confirm-box"
        :class="{ warning: state.variant === 'warning' }"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="state.title ? 'confirm-dialog-title' : undefined"
        :aria-label="state.title ? undefined : state.message"
        aria-describedby="confirm-dialog-message"
      >
        <div v-if="state.variant === 'warning'" class="warn-banner">
          <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
            <path
              fill="currentColor"
              d="M12 2 1 21h22L12 2Zm0 6a1 1 0 0 1 1 1v5a1 1 0 0 1-2 0V9a1 1 0 0 1 1-1Zm0 9.5a1.25 1.25 0 1 1 0 2.5 1.25 1.25 0 0 1 0-2.5Z"
            />
          </svg>
          <span>After the deadline</span>
        </div>
        <h2 v-if="state.title" id="confirm-dialog-title" class="confirm-title">{{ state.title }}</h2>
        <p id="confirm-dialog-message" class="confirm-message">{{ state.message }}</p>
        <div class="confirm-actions">
          <button ref="cancelBtn" type="button" class="btn secondary" @click="cancel">
            {{ state.cancelLabel }}
          </button>
          <button
            ref="confirmBtn"
            type="button"
            :class="['btn', { danger: state.danger }]"
            @click="accept"
          >
            {{ state.confirmLabel }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.confirm-overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1.5rem;
  background: rgba(20, 24, 35, 0.45);
}
.confirm-box {
  width: 100%;
  max-width: 26rem;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.25rem 1.4rem 1.1rem;
  box-shadow: 0 10px 30px rgba(20, 24, 35, 0.2);
}
.confirm-box.warning {
  border-color: var(--danger);
  border-top: 4px solid var(--danger);
  box-shadow: 0 10px 30px rgb(var(--rust-rgb) / 0.28);
}
.warn-banner {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin: 0 0 0.85rem;
  font-family: var(--mono);
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--danger);
}
.confirm-title {
  margin: 0 0 0.5rem;
  font-size: 1.05rem;
}
.confirm-message {
  margin: 0 0 1.25rem;
  color: var(--muted);
}
.confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.6rem;
}
</style>
