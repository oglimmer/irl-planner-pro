<script setup lang="ts">
import { formatInZone } from '../lib/datetime'
import type { ActivityEntry } from '../types'

// timezone renders timestamps in the event's zone. showActor adds the actor
// email (admin all-activity view); the employee "my activity" view omits it.
defineProps<{
  entries: ActivityEntry[]
  timezone: string
  showActor?: boolean
}>()
</script>

<template>
  <ul v-if="entries.length" class="timeline">
    <li v-for="e in entries" :key="e.id" :class="{ late: e.afterDeadline }">
      <div class="line">
        <span class="summary">{{ e.summary }}</span>
        <span v-if="e.afterDeadline" class="badge">after deadline</span>
      </div>
      <ul v-if="e.detail?.changes?.length" class="changes">
        <li v-for="(c, i) in e.detail.changes" :key="i">
          <span class="field">{{ c.field }}</span>
          <span class="from">{{ c.from || '—' }}</span>
          <span class="arrow">→</span>
          <span class="to">{{ c.to || '—' }}</span>
        </li>
      </ul>
      <div class="meta">
        {{ formatInZone(e.createdAt, timezone) }}
        <span v-if="showActor && e.actorEmail"> · {{ e.actorEmail }}</span>
      </div>
    </li>
  </ul>
  <p v-else class="muted">No activity yet.</p>
</template>

<style scoped>
.timeline {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.timeline > li {
  border-left: 3px solid var(--border);
  padding: 0.3rem 0 0.3rem 0.85rem;
}
.timeline > li.late {
  border-left-color: var(--danger);
}
.changes {
  list-style: none;
  margin: 0.35rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  font-size: 0.85rem;
}
.changes li {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.4rem;
}
.changes .field {
  color: var(--muted);
  min-width: 9rem;
}
.changes .from {
  color: var(--muted);
  text-decoration: line-through;
}
.changes .arrow {
  color: var(--muted);
}
.changes .to {
  color: var(--text);
}
.line {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.summary {
  color: var(--text);
}
.badge {
  font-size: 0.68rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  background: rgb(var(--rust-rgb) / 0.10);
  color: var(--danger);
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
}
.meta {
  color: var(--muted);
  font-size: 0.82rem;
  margin-top: 0.15rem;
}
.muted {
  color: var(--muted);
}
</style>
