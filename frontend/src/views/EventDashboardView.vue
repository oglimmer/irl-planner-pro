<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, errMsg } from '../api'
import { formatDate } from '../lib/datetime'
import { useAutoReload } from '../composables/useAutoReload'
import ActivityLog from '../components/ActivityLog.vue'
import AttendingFilter from '../components/AttendingFilter.vue'
import type { ActivityEntry, AttendingState, Dashboard, Event, RosterEntry } from '../types'

const props = defineProps<{ id: string }>()

const event = ref<Event | null>(null)
const error = ref('')
const copied = ref(false)
const tab = ref<'responses' | 'activity' | 'roster'>('responses')

const dashboard = ref<Dashboard | null>(null)
const filter = ref<AttendingState[]>([])
const activity = ref<ActivityEntry[]>([])
const roster = ref<RosterEntry[]>([])

const uploadFile = ref<File | null>(null)
const uploadMsg = ref('')
const uploading = ref(false)

const shareUrl = computed(() =>
  event.value ? `${window.location.origin}/events/${event.value.slug}` : '',
)

const attendingLabels: Record<AttendingState, string> = {
  yes: 'Yes',
  no: 'No',
  not_sure: 'Not sure',
  no_response: 'No response',
}

const filteredEntries = computed(() => {
  const d = dashboard.value
  if (!d) return []
  if (filter.value.length === 0) return d.rosterEntries
  const set = new Set(filter.value)
  return d.rosterEntries.filter((e) => set.has(e.attending))
})

async function copyShareUrl() {
  try {
    await navigator.clipboard.writeText(shareUrl.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
  } catch {
    // clipboard unavailable — selectable input is the fallback
  }
}

async function loadDashboard() {
  try {
    dashboard.value = await api.dashboard(props.id)
  } catch (e) {
    error.value = errMsg(e)
  }
}

async function loadActivity() {
  try {
    activity.value = await api.eventActivity(props.id)
  } catch (e) {
    error.value = errMsg(e)
  }
}

async function loadRoster() {
  try {
    roster.value = await api.listRoster(props.id)
  } catch (e) {
    error.value = errMsg(e)
  }
}

async function exportCsv() {
  try {
    const blob = await api.fetchExport(props.id, filter.value)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${event.value?.slug ?? 'event'}-responses.csv`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    error.value = errMsg(e)
  }
}

function onFile(ev: globalThis.Event) {
  const input = ev.target as HTMLInputElement
  uploadFile.value = input.files?.[0] ?? null
}

async function submitRoster() {
  if (!uploadFile.value) return
  uploading.value = true
  uploadMsg.value = ''
  try {
    const res = await api.uploadRoster(props.id, uploadFile.value)
    uploadMsg.value = `Imported ${res.inserted}, skipped ${res.skipped}.`
    if (res.errors.length) uploadMsg.value += ` Issues: ${res.errors.slice(0, 3).join('; ')}`
    uploadFile.value = null
    await Promise.all([loadRoster(), loadDashboard()])
  } catch (e) {
    uploadMsg.value = errMsg(e)
  } finally {
    uploading.value = false
  }
}

// Poll the dashboard on the chosen interval (default 1m).
const { intervalMs, options } = useAutoReload(loadDashboard)

onMounted(async () => {
  try {
    event.value = await api.getEvent(props.id)
  } catch (e) {
    error.value = errMsg(e)
  }
  loadActivity()
  loadRoster()
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
        <RouterLink :to="`/admin/events/${event.id}/edit`" class="btn secondary">Edit</RouterLink>
      </div>

      <div class="share">
        <label for="share-url">Shareable link for attendees</label>
        <div class="share-row">
          <input id="share-url" class="share-input" :value="shareUrl" readonly @focus="($event.target as HTMLInputElement).select()">
          <button type="button" class="btn" @click="copyShareUrl">{{ copied ? 'Copied!' : 'Copy link' }}</button>
        </div>
      </div>

      <div class="tabs">
        <button :class="{ active: tab === 'responses' }" @click="tab = 'responses'">Responses</button>
        <button :class="{ active: tab === 'activity' }" @click="tab = 'activity'">Activity</button>
        <button :class="{ active: tab === 'roster' }" @click="tab = 'roster'">Roster</button>
      </div>

      <!-- Responses -->
      <div v-show="tab === 'responses'">
        <div v-if="dashboard" class="responses">
          <div class="toolbar">
            <AttendingFilter v-model="filter" :counts="dashboard.counts" />
            <div class="right">
              <label class="reload">
                Refresh
                <select v-model.number="intervalMs">
                  <option v-for="o in options" :key="o.label" :value="o.ms">{{ o.label }}</option>
                </select>
              </label>
              <button class="btn" @click="exportCsv">Export CSV</button>
            </div>
          </div>

          <p class="muted summary">
            {{ filteredEntries.length }} of {{ dashboard.rosterTotal }} roster members shown.
          </p>

          <table class="grid">
            <thead>
              <tr><th>Name</th><th>Email</th><th>Attending</th><th /></tr>
            </thead>
            <tbody>
              <tr v-for="e in filteredEntries" :key="e.email">
                <td>{{ e.fullName }}</td>
                <td>{{ e.email }}</td>
                <td><span :class="['pill', e.attending]">{{ attendingLabels[e.attending] }}</span></td>
                <td>
                  <span v-if="e.afterDeadlineEdit" class="badge late">edited after deadline</span>
                </td>
              </tr>
              <tr v-if="filteredEntries.length === 0">
                <td colspan="4" class="muted">No matching roster members.</td>
              </tr>
            </tbody>
          </table>

          <div v-if="dashboard.offRoster.length" class="offroster">
            <h3>Responded, not on roster</h3>
            <ul>
              <li v-for="o in dashboard.offRoster" :key="o.email">
                {{ o.name }} ({{ o.email }}) — {{ o.attending }}
              </li>
            </ul>
          </div>
        </div>
        <p v-else class="muted">Loading…</p>
      </div>

      <!-- Activity -->
      <div v-show="tab === 'activity'">
        <ActivityLog :entries="activity" :timezone="event.timezone" show-actor />
      </div>

      <!-- Roster -->
      <div v-show="tab === 'roster'" class="roster">
        <p class="muted">
          Upload a CSV with <code>name,email</code> columns. Re-uploading replaces
          the whole roster. Used for non-responder tracking only.
        </p>
        <div class="upload">
          <input type="file" accept=".csv,text/csv" @change="onFile">
          <button class="btn" :disabled="!uploadFile || uploading" @click="submitRoster">
            {{ uploading ? 'Uploading…' : 'Upload' }}
          </button>
        </div>
        <p v-if="uploadMsg" class="muted">{{ uploadMsg }}</p>

        <table v-if="roster.length" class="grid">
          <thead><tr><th>Name</th><th>Email</th></tr></thead>
          <tbody>
            <tr v-for="m in roster" :key="m.email"><td>{{ m.fullName }}</td><td>{{ m.email }}</td></tr>
          </tbody>
        </table>
        <p v-else class="muted">No roster uploaded yet.</p>
      </div>
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
  background: #f0f1f4;
  color: var(--muted);
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  margin-left: 0.4rem;
}
.badge.late {
  font-size: 0.7rem;
  text-transform: uppercase;
  background: #fdecef;
  color: var(--danger);
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
}
.share {
  margin: 1.25rem 0;
  padding: 1rem 1.1rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.share > label {
  display: block;
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--muted);
  margin-bottom: 0.5rem;
}
.share-row { display: flex; gap: 0.5rem; }
.share-input {
  flex: 1;
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font-family: ui-monospace, monospace;
  background: var(--bg);
  color: var(--text);
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
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  flex-wrap: wrap;
}
.right {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.reload {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  color: var(--muted);
  font-size: 0.85rem;
}
.reload select {
  padding: 0.3rem 0.4rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.summary { margin: 0.75rem 0; }
.grid {
  width: 100%;
  border-collapse: collapse;
}
.grid th, .grid td {
  text-align: left;
  padding: 0.55rem 0.5rem;
  border-bottom: 1px solid var(--border);
}
.grid th {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--muted);
}
.pill {
  font-size: 0.8rem;
  padding: 0.15rem 0.55rem;
  border-radius: 999px;
}
.pill.yes { background: #e7f7ee; color: var(--ok); }
.pill.no { background: #fdecef; color: var(--danger); }
.pill.not_sure { background: #fdf6e3; color: #9a7b1a; }
.pill.no_response { background: #f0f1f4; color: var(--muted); }
.offroster {
  margin-top: 1.5rem;
}
.offroster ul {
  margin: 0.5rem 0 0;
  padding-left: 1.2rem;
  color: var(--muted);
}
.upload {
  display: flex;
  gap: 0.75rem;
  align-items: center;
  margin: 1rem 0;
}
code {
  background: var(--bg);
  padding: 0.1rem 0.3rem;
  border-radius: 4px;
}
</style>
