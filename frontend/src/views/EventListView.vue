<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { api, errMsg } from '../api'
import { formatDate } from '../lib/datetime'
import type { Event } from '../types'

const scope = ref<'current' | 'past'>('current')
const events = ref<Event[]>([])
const loading = ref(true)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    events.value = await api.listEvents(scope.value)
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    loading.value = false
  }
}

watch(scope, load)
onMounted(load)
</script>

<template>
  <section>
    <div class="head">
      <h1>Events</h1>
      <RouterLink to="/admin/events/new" class="btn">New event</RouterLink>
    </div>

    <div class="tabs">
      <button :class="{ active: scope === 'current' }" @click="scope = 'current'">
        Current &amp; upcoming
      </button>
      <button :class="{ active: scope === 'past' }" @click="scope = 'past'">
        Past
      </button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-else-if="loading" class="muted">Loading…</p>
    <p v-else-if="events.length === 0" class="muted">No {{ scope }} events.</p>

    <ul v-else class="events">
      <li v-for="e in events" :key="e.id">
        <RouterLink :to="`/admin/events/${e.id}`" class="event">
          <span class="name">{{ e.name }}</span>
          <span class="meta">
            {{ e.city }}{{ e.city && e.country ? ', ' : '' }}{{ e.country }}
            · {{ formatDate(e.startDate) }} → {{ formatDate(e.endDate) }}
          </span>
        </RouterLink>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.tabs {
  display: flex;
  gap: 0.5rem;
  margin: 1rem 0;
}
.tabs button {
  border: 1px solid var(--border);
  background: var(--surface);
  border-radius: 999px;
  padding: 0.35rem 0.9rem;
  color: var(--muted);
}
.tabs button.active {
  border-color: var(--accent);
  color: var(--accent);
}
.muted {
  color: var(--muted);
}
.error {
  color: var(--danger);
}
.events {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.event {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  padding: 0.85rem 1rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  text-decoration: none;
  color: var(--text);
}
.event:hover {
  border-color: var(--accent);
}
.name {
  font-weight: 600;
}
.meta {
  color: var(--muted);
  font-size: 0.88rem;
}
</style>
