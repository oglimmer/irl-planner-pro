<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useConfirmDialog } from '../composables/useConfirm'

const { state, accept, cancel } = useConfirmDialog()
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

function onKeydown(e: KeyboardEvent) {
  if (state.value.open && e.key === 'Escape') cancel()
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div
      v-if="state.open"
      class="confirm-overlay"
      role="dialog"
      aria-modal="true"
      @click.self="cancel"
    >
      <div class="confirm-box">
        <h2 v-if="state.title" class="confirm-title">{{ state.title }}</h2>
        <p class="confirm-message">{{ state.message }}</p>
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
