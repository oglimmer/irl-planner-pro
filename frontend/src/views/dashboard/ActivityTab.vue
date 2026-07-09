<script setup lang="ts">
import { computed, ref } from 'vue'
import { matchesQuery } from '../../lib/attending'
import ActivityLog from '../../components/ActivityLog.vue'
import type { ActivityEntry } from '../../types'

const props = defineProps<{
  entries: ActivityEntry[]
  timezone: string
}>()

// Activity category filter. Defaults to 'user' (participant actions) — the common
// review case — with 'admin' and 'all' available. This classifies what was done,
// not who did it. See ActivityEntry.category.
const category = ref<'user' | 'admin' | 'all'>('user')
const search = ref('')
// Activity is a timeline; RFC3339 timestamps sort lexically, newest first.
const newestFirst = ref(true)

const filtered = computed(() => {
  let rows = props.entries
  if (category.value !== 'all') {
    rows = rows.filter((e) => e.category === category.value)
  }
  const q = search.value.trim().toLowerCase()
  if (q)
    rows = rows.filter((e) =>
      matchesQuery(q, e.summary, e.actorEmail, e.subjectEmail, e.channel, e.status),
    )
  const dir = newestFirst.value ? -1 : 1
  return [...rows].sort((a, b) => dir * a.createdAt.localeCompare(b.createdAt))
})
</script>

<template>
  <div>
    <div class="toolbar">
      <div class="catfilter" role="group" aria-label="Filter activity by type">
        <button type="button" :class="{ active: category === 'user' }" @click="category = 'user'">
          Participant
        </button>
        <button type="button" :class="{ active: category === 'admin' }" @click="category = 'admin'">
          Admin
        </button>
        <button type="button" :class="{ active: category === 'all' }" @click="category = 'all'">
          All
        </button>
      </div>
      <input
        v-model="search"
        type="search"
        class="search"
        placeholder="Search activity, actor or attendee…"
        aria-label="Search activity"
      >
      <button type="button" class="btn secondary" @click="newestFirst = !newestFirst">
        {{ newestFirst ? 'Newest first ↓' : 'Oldest first ↑' }}
      </button>
    </div>
    <p class="muted summary">
      {{ filtered.length }} of {{ entries.length }} events shown.
    </p>
    <ActivityLog :entries="filtered" :timezone="timezone" show-actor />
  </div>
</template>

<style scoped>
.muted { color: var(--muted); }
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  flex-wrap: wrap;
}
/* Segmented control for the activity category (Participant / Admin / All). */
.catfilter {
  display: inline-flex;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
  flex-shrink: 0;
}
.catfilter button {
  padding: 0.4rem 0.8rem;
  border: none;
  background: var(--surface);
  color: var(--muted);
  font-size: 0.85rem;
  cursor: pointer;
  border-left: 1px solid var(--border);
}
.catfilter button:first-child {
  border-left: none;
}
.catfilter button.active {
  background: rgb(var(--accent-rgb) / 0.12);
  color: var(--accent);
  font-weight: 600;
}
.search {
  padding: 0.3rem 0.5rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font-size: 0.85rem;
  flex: 1 1 12rem;
  min-width: 8rem;
  max-width: 20rem;
}
.summary { margin: 0.75rem 0; }
</style>
