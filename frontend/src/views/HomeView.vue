<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, errMsg } from '../api'
import { useAuthStore } from '../stores/auth'
import { formatDeadline } from '../lib/datetime'
import type { ActiveEvent, Attending } from '../types'

const auth = useAuthStore()

const events = ref<ActiveEvent[]>([])
const loading = ref(true)
const error = ref('')

const firstName = computed(() => {
  const u = auth.user
  if (!u) return 'there'
  return u.firstName || u.name?.split(' ')[0] || u.email.split('@')[0]
})

// Editorial issue-line date, e.g. "THURSDAY · 26 JUNE 2026".
const todayLine = computed(() =>
  new Intl.DateTimeFormat('en-GB', {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
    .format(new Date())
    .toUpperCase(),
)

const feature = computed(() => events.value[0] ?? null)
const rest = computed(() => events.value.slice(1))

// One-line location summary ("Lisbon, Portugal" / "Lisbon" / "Portugal" / "").
function place(ev: ActiveEvent): string {
  return [ev.city, ev.country].filter(Boolean).join(', ')
}

// Whole calendar days from today until the event's first day. Both sides are
// reduced to a UTC midnight so the result is a clean integer day count.
function daysToStart(ev: ActiveEvent): number {
  const start = new Date(`${ev.startDate}T00:00:00Z`).getTime()
  const now = new Date()
  const today = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate())
  return Math.round((start - today) / 86_400_000)
}

function inProgress(ev: ActiveEvent): boolean {
  return daysToStart(ev) <= 0
}

// The big number + caption shown in the countdown block.
function countdown(ev: ActiveEvent): { value: string; caption: string } {
  const d = daysToStart(ev)
  if (d > 1) return { value: String(d), caption: 'days to go' }
  if (d === 1) return { value: '1', caption: 'day to go' }
  if (d === 0) return { value: 'Today', caption: 'it begins' }
  return { value: 'Now', caption: 'happening' }
}

function tripLength(ev: ActiveEvent): number {
  if (ev.days?.length) return ev.days.length
  const s = new Date(`${ev.startDate}T00:00:00Z`).getTime()
  const e = new Date(`${ev.endDate}T00:00:00Z`).getTime()
  return Math.round((e - s) / 86_400_000) + 1
}

// Compact, editorial date range: "27–31 Jul 2026" when same month, else the
// two full dates. Dates are plain calendar dates, parsed as UTC to avoid drift.
function dateRange(ev: ActiveEvent): string {
  const s = new Date(`${ev.startDate}T00:00:00Z`)
  const e = new Date(`${ev.endDate}T00:00:00Z`)
  const dmy = (d: Date) =>
    new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', day: '2-digit', month: 'short', year: 'numeric' }).format(d)
  if (ev.startDate === ev.endDate) return dmy(s)
  const sameMonth = s.getUTCFullYear() === e.getUTCFullYear() && s.getUTCMonth() === e.getUTCMonth()
  if (!sameMonth) return `${dmy(s)} – ${dmy(e)}`
  const day = (d: Date) => new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', day: '2-digit' }).format(d)
  const monthYear = new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', month: 'short', year: 'numeric' }).format(s)
  return `${day(s)}–${day(e)} ${monthYear}`
}

// The RSVP deadline as a full date + time in the company timezone (Europe/Paris).
function rsvpDate(ev: ActiveEvent): string {
  return formatDeadline(ev.submissionDeadline)
}

type StatusKey = 'none' | Attending
function statusKey(ev: ActiveEvent): StatusKey {
  return ev.hasSubmitted ? (ev.myAttending || 'none') as StatusKey : 'none'
}
function statusLabel(ev: ActiveEvent): string {
  if (!ev.hasSubmitted) return 'Awaiting your RSVP'
  switch (ev.myAttending) {
    case 'yes':
      return "You're going"
    case 'no':
      return 'Not attending'
    case 'not_sure':
      return 'Still deciding'
    default:
      return 'Responded'
  }
}
function ctaLabel(ev: ActiveEvent): string {
  return ev.hasSubmitted ? 'View or edit your response' : 'RSVP now'
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
  <section class="home">
    <header class="masthead">
      <p class="dateline">{{ todayLine }}</p>
      <h1 class="greeting">Hello, <em>{{ firstName }}</em>.</h1>
      <p class="standfirst">
        <template v-if="loading">Loading the latest from the road…</template>
        <template v-else-if="feature">Here's where the team is heading next — let us know you're in.</template>
        <template v-else>No offsite on the calendar just yet. The next destination will land right here.</template>
      </p>
    </header>

    <p v-if="error" class="error">{{ error }}</p>

    <!-- Cover story: the next offsite, with the countdown as the focal point. -->
    <RouterLink
      v-if="feature"
      :to="`/events/${feature.slug}`"
      class="feature"
      :class="{ live: inProgress(feature) }"
    >
      <img v-if="feature.imageUrl" :src="feature.imageUrl" alt="" class="feature-cover">
      <div class="feature-body">
        <p class="eyebrow">{{ inProgress(feature) ? 'Happening now' : 'Your next offsite' }}</p>
        <h2 class="dest">{{ feature.name }}</h2>
        <p v-if="place(feature)" class="place">{{ place(feature) }}</p>
        <p v-if="feature.hotelName" class="lodging">
          Staying at
          <a
            v-if="feature.hotelLink"
            :href="feature.hotelLink"
            target="_blank"
            rel="noopener noreferrer"
            class="hotel-link"
          >{{ feature.hotelName }}</a><template v-else>{{ feature.hotelName }}</template>
        </p>

        <dl class="stats">
          <div class="stat">
            <dt>Dates</dt>
            <dd>{{ dateRange(feature) }}</dd>
          </div>
          <div class="stat">
            <dt>Trip length</dt>
            <dd>{{ tripLength(feature) }} {{ tripLength(feature) === 1 ? 'day' : 'days' }}</dd>
          </div>
          <div class="stat">
            <dt>RSVP by</dt>
            <dd>{{ rsvpDate(feature) }}</dd>
          </div>
        </dl>

        <div class="feature-foot">
          <span class="status" :class="`status--${statusKey(feature)}`">{{ statusLabel(feature) }}</span>
          <span class="cta">{{ ctaLabel(feature) }}<span class="arrow" aria-hidden="true">→</span></span>
        </div>
      </div>

      <aside class="countdown" aria-hidden="true">
        <span class="count-num">{{ countdown(feature).value }}</span>
        <span class="count-caption">{{ countdown(feature).caption }}</span>
      </aside>
    </RouterLink>

    <!-- Anything else on the horizon, as a compact ledger. -->
    <template v-if="rest.length">
      <h3 class="section-label">Also on the horizon</h3>
      <ul class="ledger">
        <li v-for="ev in rest" :key="ev.id">
          <RouterLink :to="`/events/${ev.slug}`" class="ledger-row">
            <span class="ledger-when">
              <template v-if="inProgress(ev)">Now</template>
              <template v-else>{{ countdown(ev).value }}<small>d</small></template>
            </span>
            <span class="ledger-main">
              <span class="ledger-name">{{ ev.name }}</span>
              <span class="ledger-meta">{{ place(ev) }}<template v-if="place(ev)"> · </template>{{ dateRange(ev) }}</span>
            </span>
            <span class="ledger-status" :class="`status--${statusKey(ev)}`">{{ statusLabel(ev) }}</span>
            <span class="ledger-arrow" aria-hidden="true">→</span>
          </RouterLink>
        </li>
      </ul>
    </template>

    <!-- Admin desk. -->
    <template v-if="auth.user?.isAdmin">
      <h3 class="section-label">People-team desk</h3>
      <div class="desk">
        <RouterLink to="/admin/events" class="desk-card">
          <span class="desk-index">01</span>
          <span class="desk-title">Events</span>
          <span class="desk-sub">Configure offsites, rosters &amp; dashboards</span>
        </RouterLink>
        <RouterLink to="/admin/users" class="desk-card">
          <span class="desk-index">02</span>
          <span class="desk-title">Users</span>
          <span class="desk-sub">Manage admins &amp; access</span>
        </RouterLink>
      </div>
    </template>
  </section>
</template>

<style scoped>
.home {
  display: flex;
  flex-direction: column;
}

/* Masthead ─────────────────────────────────────────────────── */
.masthead {
  margin-bottom: 36px;
}
.dateline {
  margin: 0 0 14px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.26em;
  text-transform: uppercase;
  color: var(--muted);
}
.greeting {
  margin: 0;
  font-size: clamp(34px, 5vw, 54px);
}
.greeting em {
  font-style: italic;
  color: var(--accent-2);
}
.standfirst {
  margin: 14px 0 0;
  max-width: 46ch;
  font-size: 15px;
  line-height: 1.6;
  color: var(--text-soft);
}
.error {
  color: var(--danger);
  margin: 0 0 20px;
}

/* Cover story / feature card ───────────────────────────────── */
.feature {
  display: grid;
  grid-template-columns: 1fr minmax(180px, 0.42fr);
  gap: 0;
  margin-bottom: 44px;
  border: 1px solid var(--border);
  border-top: 3px solid var(--accent);
  background:
    linear-gradient(180deg, rgb(var(--accent-rgb) / 0.05), transparent 38%),
    var(--panel);
  color: var(--text);
  text-decoration: none;
  transition: border-color 0.18s ease, box-shadow 0.18s ease;
}
.feature:hover {
  border-color: var(--accent);
  box-shadow: 0 14px 40px rgb(var(--shadow-rgb) / 0.10);
}

/* Cover image spans both columns as a banner across the top of the card. */
.feature-cover {
  grid-column: 1 / -1;
  display: block;
  width: 100%;
  height: clamp(150px, 22vw, 250px);
  object-fit: cover;
  border-bottom: 1px solid var(--border);
}

.feature-body {
  padding: 30px 34px 28px;
  min-width: 0;
}
.eyebrow {
  margin: 0 0 14px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.24em;
  text-transform: uppercase;
  color: var(--accent-2);
}
.feature.live .eyebrow {
  color: var(--success);
}
.dest {
  margin: 0;
  font-size: clamp(30px, 4.4vw, 50px);
  line-height: 1.02;
  letter-spacing: -0.02em;
}
.place {
  margin: 10px 0 0;
  font-family: var(--mono);
  font-size: 13px;
  letter-spacing: 0.04em;
  color: var(--text-soft);
}
.lodging {
  margin: 4px 0 0;
  font-size: 13px;
  color: var(--muted);
}

.hotel-link {
  color: inherit;
  text-decoration: underline;
}

.hotel-link:hover {
  color: var(--text);
}

.stats {
  display: flex;
  flex-wrap: wrap;
  gap: 0;
  margin: 26px 0 0;
  border-top: 1px solid var(--border-soft);
}
.stat {
  flex: 1;
  min-width: 120px;
  padding: 16px 22px 4px 0;
  margin-right: 22px;
  border-right: 1px solid var(--border-soft);
}
.stat:last-child {
  border-right: 0;
  margin-right: 0;
}
.stat dt {
  font-family: var(--mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--muted);
}
.stat dd {
  margin: 6px 0 0;
  font-family: var(--serif);
  font-size: 19px;
  font-weight: 420;
  color: var(--text);
}

.feature-foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 14px 22px;
  margin-top: 26px;
}
.cta {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--text);
  padding-bottom: 3px;
  border-bottom: 2px solid var(--accent);
}
.cta .arrow {
  transition: transform 0.18s ease;
}
.feature:hover .cta .arrow {
  transform: translateX(4px);
}

/* Status chips — shared between the feature and the ledger. */
.status {
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.feature-foot .status {
  padding: 6px 12px;
  border: 1px solid currentcolor;
}
.status--none {
  color: var(--accent-2);
}
.status--yes {
  color: var(--success);
}
.status--not_sure {
  color: var(--blue);
}
.status--no {
  color: var(--muted);
}

/* Countdown block — the focal point. */
.countdown {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 24px;
  border-left: 1px solid var(--border);
  background: rgb(var(--accent-rgb) / 0.06);
}
.count-num {
  font-family: var(--serif);
  font-style: italic;
  font-weight: 360;
  font-size: clamp(56px, 9vw, 104px);
  line-height: 0.9;
  letter-spacing: -0.03em;
  color: var(--accent-2);
}
.count-caption {
  margin-top: 12px;
  font-family: var(--mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.24em;
  text-transform: uppercase;
  color: var(--muted);
}
.feature.live .count-num {
  color: var(--success);
}

/* Section labels reuse the h3 token but add breathing room. */
.section-label {
  margin: 8px 0 16px;
}

/* Ledger of other upcoming events ──────────────────────────── */
.ledger {
  list-style: none;
  margin: 0 0 44px;
  padding: 0;
  border-top: 1px solid var(--border);
}
.ledger-row {
  display: grid;
  grid-template-columns: 64px 1fr auto 18px;
  align-items: center;
  gap: 18px;
  padding: 16px 4px;
  border-bottom: 1px solid var(--border-soft);
  color: var(--text);
  text-decoration: none;
  transition: background-color 0.15s ease;
}
.ledger-row:hover {
  background: rgb(var(--accent-rgb) / 0.07);
}
.ledger-when {
  font-family: var(--serif);
  font-style: italic;
  font-size: 28px;
  line-height: 1;
  color: var(--accent-2);
  text-align: right;
}
.ledger-when small {
  font-size: 13px;
  font-style: normal;
}
.ledger-main {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}
.ledger-name {
  font-family: var(--serif);
  font-size: 19px;
  letter-spacing: -0.01em;
}
.ledger-meta {
  font-family: var(--mono);
  font-size: 11.5px;
  letter-spacing: 0.03em;
  color: var(--muted);
}
.ledger-status {
  white-space: nowrap;
}
.ledger-arrow {
  color: var(--muted);
  transition: transform 0.15s ease, color 0.15s ease;
}
.ledger-row:hover .ledger-arrow {
  color: var(--accent-2);
  transform: translateX(3px);
}

/* Admin desk ───────────────────────────────────────────────── */
.desk {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 14px;
}
.desk-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 22px 22px 24px;
  border: 1px solid var(--border);
  background: var(--panel);
  color: var(--text);
  text-decoration: none;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}
.desk-card:hover {
  border-color: var(--accent);
  background: rgb(var(--accent-rgb) / 0.05);
}
.desk-index {
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.2em;
  color: var(--accent-2);
}
.desk-title {
  font-family: var(--serif);
  font-style: italic;
  font-size: 24px;
  line-height: 1.1;
}
.desk-sub {
  font-size: 13px;
  color: var(--muted);
}

@media (max-width: 640px) {
  .feature {
    grid-template-columns: 1fr;
  }
  .feature-body {
    padding: 24px 22px 22px;
  }
  .countdown {
    flex-direction: row;
    gap: 14px;
    padding: 18px 22px;
    border-left: 0;
    border-top: 1px solid var(--border);
  }
  .count-num {
    font-size: 52px;
  }
  .count-caption {
    margin-top: 0;
  }
  .stat {
    border-right: 0;
    margin-right: 0;
    padding-right: 0;
    flex-basis: 45%;
  }
  .ledger-row {
    grid-template-columns: 52px 1fr 16px;
  }
  .ledger-status {
    display: none;
  }
}
</style>
