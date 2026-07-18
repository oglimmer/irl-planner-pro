<script setup lang="ts">
import { computed } from 'vue'
import type { AttendingState, Dashboard } from '../types'

const props = defineProps<{
  modelValue: AttendingState[]
  counts: Dashboard['counts']
}>()
const emit = defineEmits<{ 'update:modelValue': [AttendingState[]] }>()

const chips: { state: AttendingState; label: string; countKey: keyof Dashboard['counts'] }[] = [
  { state: 'yes', label: 'Yes', countKey: 'yes' },
  { state: 'no', label: 'No', countKey: 'no' },
  { state: 'not_sure', label: 'Not sure', countKey: 'notSure' },
  { state: 'no_response', label: 'No response', countKey: 'noResponse' },
]

const active = computed(() => new Set(props.modelValue))

function toggle(state: AttendingState) {
  const next = new Set(props.modelValue)
  if (next.has(state)) next.delete(state)
  else next.add(state)
  emit('update:modelValue', [...next])
}

function clearAll() {
  emit('update:modelValue', [])
}
</script>

<template>
  <div class="filter">
    <button
      v-for="c in chips"
      :key="c.state"
      type="button"
      :class="['chip', { active: active.has(c.state) }]"
      @click="toggle(c.state)"
    >
      {{ c.label }} <span class="count">{{ counts[c.countKey] }}</span>
    </button>
    <button v-if="modelValue.length" type="button" class="chip clear" @click="clearAll">
      Clear filter
    </button>
  </div>
</template>

<style scoped>
.filter {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: center;
}
.chip {
  border: 1px solid var(--border);
  background: var(--surface);
  border-radius: 999px;
  padding: 0.35rem 0.8rem;
  color: var(--muted);
  font-size: 0.9rem;
}
.chip.active {
  border-color: var(--accent);
  color: var(--accent);
  background: rgb(var(--accent-rgb) / 0.07);
}
.count {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  margin-left: 0.2rem;
}
.chip.clear {
  color: var(--danger);
  border-style: dashed;
}
</style>
