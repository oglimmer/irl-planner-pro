<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

// A dropdown that toggles which table columns are visible. Order is owned by the
// parent (it filters its canonical column list by this selection), so toggling
// here only adds/removes keys. At least one column must stay checked.
const props = defineProps<{
  columns: { key: string; label: string }[]
  modelValue: string[]
  fullWidth: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [string[]]
  'update:fullWidth': [boolean]
  reset: []
}>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)

const selected = computed(() => new Set(props.modelValue))

function toggle(key: string) {
  const next = new Set(props.modelValue)
  if (next.has(key)) {
    if (next.size === 1) return // never leave the table with no columns
    next.delete(key)
  } else {
    next.add(key)
  }
  emit('update:modelValue', props.columns.map((c) => c.key).filter((k) => next.has(k)))
}

function onDocClick(ev: MouseEvent) {
  if (open.value && root.value && !root.value.contains(ev.target as Node)) {
    open.value = false
  }
}
function onKey(ev: KeyboardEvent) {
  if (ev.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKey)
})
</script>

<template>
  <div ref="root" class="colpicker">
    <button
      type="button"
      class="btn secondary"
      :aria-expanded="open"
      aria-haspopup="true"
      @click="open = !open"
    >
      Columns <span class="count">{{ modelValue.length }}</span> ▾
    </button>
    <div v-if="open" class="panel" role="menu">
      <label class="opt toggle">
        <input
          type="checkbox"
          :checked="fullWidth"
          @change="emit('update:fullWidth', !fullWidth)"
        >
        Expand full width
      </label>
      <div class="panel-head">
        <span class="muted">Show columns</span>
        <button type="button" class="btn-link" @click="emit('reset')">Reset</button>
      </div>
      <label v-for="col in columns" :key="col.key" class="opt">
        <input
          type="checkbox"
          :checked="selected.has(col.key)"
          :disabled="selected.has(col.key) && modelValue.length === 1"
          @change="toggle(col.key)"
        >
        {{ col.label }}
      </label>
    </div>
  </div>
</template>

<style scoped>
.colpicker {
  position: relative;
  flex-shrink: 0;
}
.count {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  opacity: 0.7;
}
.panel {
  position: absolute;
  right: 0;
  top: calc(100% + 0.3rem);
  z-index: 20;
  min-width: 14rem;
  max-height: 60vh;
  overflow-y: auto;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: 0 8px 24px rgb(0 0 0 / 0.12);
  padding: 0.4rem;
}
.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.25rem 0.5rem 0.4rem;
  border-bottom: 1px solid var(--border);
  margin-bottom: 0.3rem;
  font-size: 0.78rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.muted { color: var(--muted); }
.opt {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.3rem 0.5rem;
  border-radius: var(--radius);
  font-size: 0.9rem;
  cursor: pointer;
}
.opt:hover {
  background: var(--bg-2);
}
.opt.toggle {
  font-weight: 600;
}
.opt input {
  cursor: pointer;
}
.btn-link {
  border: none;
  background: none;
  padding: 0;
  cursor: pointer;
  color: var(--accent);
  font-size: 0.8rem;
}
</style>
