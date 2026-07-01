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
    <li v-for="e in entries" :key="e.id" :class="e.afterDeadline ? 'late' : 'ontime'">
      <div class="line">
        <span v-if="e.afterDeadline" class="badge late">
          <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true">
            <path
              fill="currentColor"
              d="M12 2 1 21h22L12 2Zm0 6a1 1 0 0 1 1 1v5a1 1 0 0 1-2 0V9a1 1 0 0 1 1-1Zm0 9.5a1.25 1.25 0 1 1 0 2.5 1.25 1.25 0 0 1 0-2.5Z"
            />
          </svg>
          After deadline
        </span>
        <span v-else class="badge ontime">On time</span>
        <span class="summary">{{ e.summary }}</span>
      </div>
      <ul v-if="e.detail?.changes?.length" class="changes">
        <li v-for="c in e.detail.changes" :key="c.field">
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
/* On-time entries get a quiet green rail; after-deadline entries become a loud
   red-tinted alert card so the IRL team can spot late changes at a glance. */
.timeline > li.ontime {
  border-left-color: var(--success);
}
.timeline > li.late {
  border-left: 4px solid var(--danger);
  background: rgb(var(--rust-rgb) / 0.07);
  border-radius: 0 6px 6px 0;
  padding: 0.55rem 0.7rem 0.55rem 0.85rem;
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
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.68rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  padding: 0.12rem 0.5rem;
  border-radius: 999px;
  white-space: nowrap;
}
.badge.late {
  background: var(--danger);
  color: #fff;
}
.badge.ontime {
  background: rgb(var(--success-rgb) / 0.12);
  color: var(--success);
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
