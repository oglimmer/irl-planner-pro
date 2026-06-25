<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, errMsg } from '../api'
import { useAuthStore } from '../stores/auth'
import { formatDate, formatInZone } from '../lib/datetime'
import type { ActiveEvent } from '../types'

const auth = useAuthStore()

const events = ref<ActiveEvent[]>([])
const loading = ref(true)
const error = ref('')

// One-line location summary ("Lisbon, Portugal" / "Lisbon" / "Portugal" / "").
function place(ev: ActiveEvent): string {
  return [ev.city, ev.country].filter(Boolean).join(', ')
}

function dateRange(ev: ActiveEvent): string {
  return ev.startDate === ev.endDate
    ? formatDate(ev.startDate)
    : `${formatDate(ev.startDate)} – ${formatDate(ev.endDate)}`
}

// Short status line for the caller's current RSVP on this event.
function statusLabel(ev: ActiveEvent): string {
  if (!ev.hasSubmitted) return "You haven't responded yet"
  switch (ev.myAttending) {
    case 'yes':
      return "You're attending 🎉"
    case 'no':
      return "You marked: not attending"
    case 'not_sure':
      return "You marked: not sure yet"
    default:
      return 'You have responded'
  }
}

function ctaLabel(ev: ActiveEvent): string {
  return ev.hasSubmitted ? 'View / edit your response' : 'RSVP now'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    events.value = await api.activeEvents()
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <section>
    <h1>Welcome{{ auth.user ? `, ${auth.user.name || auth.user.email}` : '' }}</h1>

    <p v-if="error" class="error">{{ error }}</p>

    <!-- The prominent active-offsite card every user sees right after login. -->
    <template v-if="!loading && events.length">
      <RouterLink
        v-for="(ev, i) in events"
        :key="ev.id"
        :to="`/events/${ev.slug}`"
        class="hero"
        :class="{ secondary: i > 0 }"
      >
        <span class="hero-eyebrow">{{ i === 0 ? 'Your upcoming offsite' : 'Also coming up' }}</span>
        <span class="hero-title">{{ ev.name }}</span>
        <span class="hero-meta">
          <span v-if="place(ev)">📍 {{ place(ev) }}</span>
          <span>🗓 {{ dateRange(ev) }}</span>
        </span>
        <span class="hero-deadline">
          RSVP by {{ formatInZone(ev.submissionDeadline, ev.timezone) }}
        </span>
        <span class="hero-status" :class="`is-${ev.hasSubmitted ? ev.myAttending : 'none'}`">
          {{ statusLabel(ev) }}
        </span>
        <span class="hero-cta">{{ ctaLabel(ev) }} →</span>
      </RouterLink>
    </template>

    <p v-else-if="!loading && !error && !auth.user?.isAdmin" class="muted">
      There's no upcoming offsite to RSVP for right now. When the next one is
      scheduled, it'll show up here.
    </p>

    <template v-if="auth.user?.isAdmin">
      <h2 class="admin-heading">Admin</h2>
      <p class="muted">Manage offsites and attendance.</p>
      <div class="cards">
        <RouterLink to="/admin/events" class="card">
          <span class="card-title">Events</span>
          <span class="card-sub">Configure offsites, rosters, dashboards</span>
        </RouterLink>
        <RouterLink to="/admin/users" class="card">
          <span class="card-title">Users</span>
          <span class="card-sub">Manage admins</span>
        </RouterLink>
      </div>
    </template>
  </section>
</template>

<style scoped>
.muted {
  color: var(--muted);
}
.error {
  color: var(--danger, #c0392b);
}

/* Prominent landing card for the active offsite. */
.hero {
  display: flex;
  flex-direction: column;
  gap: 0.45rem;
  margin: 1.25rem 0;
  padding: 1.5rem 1.6rem;
  border: 1px solid var(--accent);
  border-left: 5px solid var(--accent);
  border-radius: var(--radius);
  background: var(--surface);
  text-decoration: none;
  color: var(--text);
  transition: box-shadow 0.15s ease, transform 0.15s ease;
}
.hero:hover {
  box-shadow: 0 6px 22px rgb(0 0 0 / 12%);
  transform: translateY(-1px);
}
.hero.secondary {
  border-color: var(--border);
  border-left-color: var(--accent);
}
.hero-eyebrow {
  font-size: 0.78rem;
  font-weight: 650;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--accent);
}
.hero-title {
  font-size: 1.6rem;
  font-weight: 700;
  line-height: 1.2;
}
.hero-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem 1.1rem;
  color: var(--muted);
  font-size: 0.95rem;
}
.hero-deadline {
  font-size: 0.9rem;
  color: var(--muted);
}
.hero-status {
  margin-top: 0.2rem;
  font-weight: 600;
}
.hero-status.is-none {
  color: var(--accent);
}
.hero-status.is-yes {
  color: var(--success, #2e7d32);
}
.hero-cta {
  margin-top: 0.6rem;
  align-self: flex-start;
  padding: 0.55rem 1.1rem;
  border-radius: var(--radius);
  background: var(--accent);
  color: #fff;
  font-weight: 600;
  font-size: 0.95rem;
}

.admin-heading {
  margin-top: 2rem;
  font-size: 1.05rem;
}
.cards {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  margin-top: 1rem;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 1.1rem 1.3rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  text-decoration: none;
  color: var(--text);
  min-width: 220px;
}
.card:hover {
  border-color: var(--accent);
}
.card-title {
  font-weight: 650;
}
.card-sub {
  color: var(--muted);
  font-size: 0.88rem;
}
</style>
