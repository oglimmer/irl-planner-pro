<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, errMsg } from '../api'
import { formatDate } from '../lib/datetime'
import { useAutoReload } from '../composables/useAutoReload'
import ResponsesTab from './dashboard/ResponsesTab.vue'
import AttendeesTab from './dashboard/AttendeesTab.vue'
import ActivityTab from './dashboard/ActivityTab.vue'
import FinancialTab from './dashboard/FinancialTab.vue'
import EventMessaging from './EventMessaging.vue'
import type { ActivityEntry, Dashboard, Event, Submission, UserSummary } from '../types'

const props = defineProps<{ id: string }>()

const event = ref<Event | null>(null)
const error = ref('')
const copied = ref(false)
const tab = ref<'responses' | 'activity' | 'attendees' | 'financial' | 'messaging'>('responses')

// Data owned by the page and shared across tabs: the Responses and Attendees
// tabs both read `dashboard`, so it lives here and is passed down rather than
// fetched twice.
const dashboard = ref<Dashboard | null>(null)
const submissions = ref<Submission[]>([])
const activity = ref<ActivityEntry[]>([])
const directory = ref<UserSummary[]>([])

const shareUrl = computed(() =>
  event.value ? `${window.location.origin}/events/${event.value.slug}` : '',
)

let copiedTimer: ReturnType<typeof setTimeout> | null = null
async function copyShareUrl() {
  try {
    await navigator.clipboard.writeText(shareUrl.value)
    copied.value = true
    if (copiedTimer) clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => (copied.value = false), 2000)
  } catch {
    // clipboard unavailable — nothing to do
  }
}

async function loadDashboard() {
  try {
    dashboard.value = await api.dashboard(props.id)
  } catch (e) {
    error.value = errMsg(e)
  }
}

async function loadSubmissions() {
  try {
    submissions.value = await api.listSubmissions(props.id)
  } catch (e) {
    error.value = errMsg(e)
  }
}

// The Responses tab needs both the attendee overview and the full submission
// detail (for the optional travel/profile columns), so refresh them together.
async function loadResponses() {
  await Promise.all([loadDashboard(), loadSubmissions()])
}

async function loadActivity() {
  try {
    activity.value = await api.eventActivity(props.id)
  } catch (e) {
    error.value = errMsg(e)
  }
}

async function loadDirectory() {
  try {
    directory.value = await api.listUsers()
  } catch (e) {
    error.value = errMsg(e)
  }
}

// An admin saved an edit to someone's response — refresh the responses (now
// showing the change + lock) and the activity timeline (which logged the edit).
function onResponseSaved() {
  void Promise.all([loadResponses(), loadActivity()])
}

// An attendee was added/removed/imported — refresh the shared dashboard (both
// tabs) and the directory (the add picker's options).
function onAttendeesChanged() {
  void Promise.all([loadDashboard(), loadDirectory()])
}

// Poll the responses (dashboard + submissions) on the chosen interval (default 1m).
const { intervalMs, options } = useAutoReload(loadResponses)

onMounted(async () => {
  try {
    event.value = await api.getEvent(props.id)
  } catch (e) {
    error.value = errMsg(e)
  }
  loadActivity()
  loadDirectory()
})
</script>

<template>
  <section>
    <p v-if="error" class="error">{{ error }}</p>

    <template v-if="event">
      <div class="head">
        <div>
          <h1>{{ event.name }}</h1>
          <p class="muted">
            {{ event.city }}{{ event.city && event.country ? ', ' : '' }}{{ event.country }}
            · {{ formatDate(event.startDate) }} → {{ formatDate(event.endDate) }}
            <span v-if="event.isPast" class="badge past">Past</span>
          </p>
        </div>
        <div class="head-actions">
          <button type="button" class="btn secondary" @click="copyShareUrl">{{ copied ? 'Copied!' : 'Copy link' }}</button>
          <RouterLink :to="`/admin/events/${event.id}/edit`" class="btn secondary">Edit</RouterLink>
        </div>
      </div>

      <div class="tabs">
        <button :class="{ active: tab === 'responses' }" @click="tab = 'responses'">Responses</button>
        <button :class="{ active: tab === 'activity' }" @click="tab = 'activity'">Activity</button>
        <button :class="{ active: tab === 'attendees' }" @click="tab = 'attendees'">Attendees</button>
        <button :class="{ active: tab === 'financial' }" @click="tab = 'financial'">Financial</button>
        <button :class="{ active: tab === 'messaging' }" @click="tab = 'messaging'">Messaging</button>
      </div>

      <ResponsesTab
        v-show="tab === 'responses'"
        v-model:interval-ms="intervalMs"
        :dashboard="dashboard"
        :submissions="submissions"
        :event-id="props.id"
        :event-slug="event.slug"
        :timezone="event.timezone"
        :reload-options="options"
        @error="error = $event"
        @saved="onResponseSaved"
      />

      <ActivityTab
        v-show="tab === 'activity'"
        :entries="activity"
        :timezone="event.timezone"
      />

      <AttendeesTab
        v-show="tab === 'attendees'"
        :dashboard="dashboard"
        :directory="directory"
        :event-id="props.id"
        :event-name="event.name"
        @changed="onAttendeesChanged"
        @error="error = $event"
      />

      <FinancialTab
        v-show="tab === 'financial'"
        :event-id="props.id"
        :active="tab === 'financial'"
        @error="error = $event"
      />

      <EventMessaging v-show="tab === 'messaging'" :event-id="props.id" />
    </template>
  </section>
</template>

<style scoped>
.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}
.muted { color: var(--muted); }
.error { color: var(--danger); }
.badge.past {
  font-size: 0.7rem;
  text-transform: uppercase;
  background: var(--bg-2);
  color: var(--muted);
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  margin-left: 0.4rem;
}
.head-actions {
  display: flex;
  gap: 0.5rem;
  flex-shrink: 0;
}
.tabs {
  display: flex;
  gap: 0.5rem;
  border-bottom: 1px solid var(--border);
  margin: 1rem 0 1.25rem;
}
.tabs button {
  border: none;
  background: none;
  padding: 0.5rem 0.25rem;
  margin-right: 0.75rem;
  color: var(--muted);
  border-bottom: 2px solid transparent;
}
.tabs button.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
}
</style>
