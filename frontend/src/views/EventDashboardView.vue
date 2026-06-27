<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, errMsg } from '../api'
import { formatDate } from '../lib/datetime'
import { useAutoReload } from '../composables/useAutoReload'
import { useColumnSort } from '../composables/useColumnSort'
import { useConfirm } from '../composables/useConfirm'
import ActivityLog from '../components/ActivityLog.vue'
import AttendingFilter from '../components/AttendingFilter.vue'
import type { ActivityEntry, AttendingState, Dashboard, DashboardEntry, Event, UserSummary } from '../types'

const props = defineProps<{ id: string }>()

const { confirm } = useConfirm()

const event = ref<Event | null>(null)
const error = ref('')
const copied = ref(false)
const tab = ref<'responses' | 'activity' | 'attendees'>('responses')

const dashboard = ref<Dashboard | null>(null)
const filter = ref<AttendingState[]>([])
const activity = ref<ActivityEntry[]>([])

// Per-tab client-side search (contains, case-insensitive). Each tab keeps its
// own query so switching tabs doesn't surprise the user with a stale filter.
const responsesSearch = ref('')
const attendeesSearch = ref('')
const activitySearch = ref('')

// Activity category filter. Defaults to 'user' (participant actions) — the
// common review case — with 'admin' and 'all' available. This classifies what
// was done, not who did it. See ActivityEntry.category.
const activityCategory = ref<'user' | 'admin' | 'all'>('user')

// Attending sorts in a logical pipeline order rather than alphabetically; status
// ranks the most notable flags highest so a descending sort surfaces them first.
const attendingRank: Record<AttendingState, number> = {
  yes: 0,
  not_sure: 1,
  no: 2,
  no_response: 3,
}

// Responses table: Name / Email / Attending / Status, all sortable.
type ResponseKey = 'name' | 'email' | 'attending' | 'status'
const responsesSort = useColumnSort<ResponseKey>()
const responseColumns: { key: ResponseKey; label: string }[] = [
  { key: 'name', label: 'Name' },
  { key: 'email', label: 'Email' },
  { key: 'attending', label: 'Attending' },
  { key: 'status', label: 'Status' },
]
function responseSortValue(e: DashboardEntry, key: ResponseKey): string | number {
  switch (key) {
    case 'name':
      return e.name.toLowerCase()
    case 'email':
      return e.email.toLowerCase()
    case 'attending':
      return attendingRank[e.attending]
    case 'status':
      return (e.afterDeadlineEdit ? 2 : 0) + (e.hasLoggedIn ? 0 : 1)
  }
}

// Attendees table: Name / Email / Attending / Signed in (the last column is the
// Remove action, not sortable). Attending and sign-in are two separate facts, so
// they get their own sortable columns.
type AttendeeKey = 'name' | 'email' | 'attending' | 'signedIn'
const attendeesSort = useColumnSort<AttendeeKey>()
const attendeeColumns: { key: AttendeeKey; label: string }[] = [
  { key: 'name', label: 'Name' },
  { key: 'email', label: 'Email' },
  { key: 'attending', label: 'Attending' },
  { key: 'signedIn', label: 'Signed in' },
]
function attendeeSortValue(e: DashboardEntry, key: AttendeeKey): string | number {
  switch (key) {
    case 'name':
      return e.name.toLowerCase()
    case 'email':
      return e.email.toLowerCase()
    case 'attending':
      return attendingRank[e.attending]
    case 'signedIn':
      // Ascending surfaces the not-signed-in (false → 0) people first.
      return e.hasLoggedIn ? 1 : 0
  }
}

// Activity is a timeline, not a table — so instead of column headers it gets a
// search plus a newest/oldest toggle (RFC3339 timestamps sort lexically).
const activityNewestFirst = ref(true)

function matches(q: string, ...fields: (string | undefined)[]): boolean {
  return fields.some((f) => (f ?? '').toLowerCase().includes(q))
}

// Company directory, for the "add an employee" picker on the Attendees tab.
const directory = ref<UserSummary[]>([])
const addUserId = ref('')

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
  let rows = d.entries

  if (filter.value.length) {
    const set = new Set(filter.value)
    rows = rows.filter((e) => set.has(e.attending))
  }

  const q = responsesSearch.value.trim().toLowerCase()
  if (q) rows = rows.filter((e) => matches(q, e.name, e.email))

  return responsesSort.sortRows(rows, responseSortValue)
})

// Attendees tab: same directory rows, with their own search + sort (no attending
// filter — the Attendees tab is about who's on the list, not who responded).
const attendeeEntries = computed(() => {
  const d = dashboard.value
  if (!d) return []
  let rows = d.entries
  const q = attendeesSearch.value.trim().toLowerCase()
  if (q) rows = rows.filter((e) => matches(q, e.name, e.email))
  return attendeesSort.sortRows(rows, attendeeSortValue)
})

// Activity tab: search across summary + actor/subject email, then order by time.
const filteredActivity = computed(() => {
  let rows = activity.value
  if (activityCategory.value !== 'all') {
    rows = rows.filter((e) => e.category === activityCategory.value)
  }
  const q = activitySearch.value.trim().toLowerCase()
  if (q) rows = rows.filter((e) => matches(q, e.summary, e.actorEmail, e.subjectEmail))
  const dir = activityNewestFirst.value ? -1 : 1
  return [...rows].sort((a, b) => dir * a.createdAt.localeCompare(b.createdAt))
})

// Directory users not yet on this event's attendee list — the picker's options.
const addableUsers = computed(() => {
  const taken = new Set((dashboard.value?.entries ?? []).map((e) => e.userId))
  return directory.value.filter((u) => !taken.has(u.id))
})

async function copyShareUrl() {
  try {
    await navigator.clipboard.writeText(shareUrl.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
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

async function submitImport() {
  if (!uploadFile.value) return
  // Import is additive — it only ever adds attendees — so no destructive confirm.
  uploading.value = true
  uploadMsg.value = ''
  try {
    const res = await api.importAttendees(props.id, uploadFile.value)
    uploadMsg.value = `Added ${res.added}, skipped ${res.skipped}.`
    if (res.errors.length) uploadMsg.value += ` Issues: ${res.errors.slice(0, 3).join('; ')}`
    uploadFile.value = null
    await Promise.all([loadDashboard(), loadDirectory()])
  } catch (e) {
    uploadMsg.value = errMsg(e)
  } finally {
    uploading.value = false
  }
}

async function addAttendee() {
  if (!addUserId.value) return
  try {
    await api.addAttendee(props.id, addUserId.value)
    addUserId.value = ''
    await loadDashboard()
  } catch (e) {
    error.value = errMsg(e)
  }
}

async function removeAttendee(userId: string, name: string) {
  const ok = await confirm({
    title: 'Remove attendee?',
    message: `Remove ${name} from “${event.value?.name ?? 'this event'}”? Their profile and any response are kept — only their place on this event's list is removed.`,
    confirmLabel: 'Remove',
    danger: true,
  })
  if (!ok) return
  try {
    await api.removeAttendee(props.id, userId)
    await loadDashboard()
  } catch (e) {
    error.value = errMsg(e)
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
      </div>

      <!-- Responses -->
      <div v-show="tab === 'responses'">
        <div v-if="dashboard" class="responses">
          <div class="toolbar">
            <AttendingFilter v-model="filter" :counts="dashboard.counts" />
            <div class="right">
              <input
                v-model="responsesSearch"
                type="search"
                class="search"
                placeholder="Search name or email…"
                aria-label="Search responses by name or email"
              >
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
            {{ filteredEntries.length }} of {{ dashboard.total }} attendees shown.
          </p>

          <table class="grid">
            <thead>
              <tr>
                <th
                  v-for="col in responseColumns"
                  :key="col.key"
                  class="sortable"
                  :class="{ sorted: responsesSort.isSorted(col.key) }"
                  :aria-sort="responsesSort.ariaSort(col.key)"
                  role="button"
                  tabindex="0"
                  @click="responsesSort.toggleSort(col.key)"
                  @keydown.enter.prevent="responsesSort.toggleSort(col.key)"
                  @keydown.space.prevent="responsesSort.toggleSort(col.key)"
                >
                  {{ col.label }}<span class="arrow">{{ responsesSort.sortArrow(col.key) }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="e in filteredEntries" :key="e.userId">
                <td>{{ e.name }}</td>
                <td>{{ e.email }}</td>
                <td><span :class="['pill', e.attending]">{{ attendingLabels[e.attending] }}</span></td>
                <td>
                  <span v-if="!e.hasLoggedIn" class="badge muted-badge">not signed in</span>
                  <span v-if="e.afterDeadlineEdit" class="badge late">edited after deadline</span>
                </td>
              </tr>
              <tr v-if="filteredEntries.length === 0">
                <td colspan="4" class="muted">No matching attendees.</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="muted">Loading…</p>
      </div>

      <!-- Activity -->
      <div v-show="tab === 'activity'">
        <div class="toolbar">
          <div class="catfilter" role="group" aria-label="Filter activity by type">
            <button
              type="button"
              :class="{ active: activityCategory === 'user' }"
              @click="activityCategory = 'user'"
            >
              Participant
            </button>
            <button
              type="button"
              :class="{ active: activityCategory === 'admin' }"
              @click="activityCategory = 'admin'"
            >
              Admin
            </button>
            <button
              type="button"
              :class="{ active: activityCategory === 'all' }"
              @click="activityCategory = 'all'"
            >
              All
            </button>
          </div>
          <input
            v-model="activitySearch"
            type="search"
            class="search"
            placeholder="Search activity, actor or attendee…"
            aria-label="Search activity"
          >
          <button
            type="button"
            class="btn secondary"
            @click="activityNewestFirst = !activityNewestFirst"
          >
            {{ activityNewestFirst ? 'Newest first ↓' : 'Oldest first ↑' }}
          </button>
        </div>
        <p class="muted summary">
          {{ filteredActivity.length }} of {{ activity.length }} events shown.
        </p>
        <ActivityLog :entries="filteredActivity" :timezone="event.timezone" show-actor />
      </div>

      <!-- Attendees -->
      <div v-show="tab === 'attendees'" class="attendees">
        <p class="muted">
          Everyone in the company is an attendee by default, and new employees are
          added automatically as they join. Remove anyone who isn't expected at this
          event. Use the picker below to put someone back, or import a CSV of new
          hires who haven't signed in yet.
        </p>

        <div class="add-row">
          <select v-model="addUserId" class="picker">
            <option value="" disabled>Add an existing employee…</option>
            <option v-for="u in addableUsers" :key="u.id" :value="u.id">
              {{ u.name || u.email }} ({{ u.email }})
            </option>
          </select>
          <button class="btn" :disabled="!addUserId" @click="addAttendee">Add</button>
        </div>

        <p class="muted">
          Or import many at once from a CSV with <code>name,email</code> columns
          — handy for onboarding new employees who haven't used the app yet
          (additive — existing attendees are kept):
        </p>
        <div class="upload">
          <input type="file" accept=".csv,text/csv" @change="onFile">
          <button class="btn" :disabled="!uploadFile || uploading" @click="submitImport">
            {{ uploading ? 'Importing…' : 'Import' }}
          </button>
        </div>
        <p v-if="uploadMsg" class="muted">{{ uploadMsg }}</p>

        <template v-if="dashboard && dashboard.entries.length">
          <div class="toolbar">
            <input
              v-model="attendeesSearch"
              type="search"
              class="search"
              placeholder="Search name or email…"
              aria-label="Search attendees by name or email"
            >
            <p class="muted summary">
              {{ attendeeEntries.length }} of {{ dashboard.entries.length }} attendees shown.
            </p>
          </div>

          <table class="grid">
            <thead>
              <tr>
                <th
                  v-for="col in attendeeColumns"
                  :key="col.key"
                  class="sortable"
                  :class="{ sorted: attendeesSort.isSorted(col.key) }"
                  :aria-sort="attendeesSort.ariaSort(col.key)"
                  role="button"
                  tabindex="0"
                  @click="attendeesSort.toggleSort(col.key)"
                  @keydown.enter.prevent="attendeesSort.toggleSort(col.key)"
                  @keydown.space.prevent="attendeesSort.toggleSort(col.key)"
                >
                  {{ col.label }}<span class="arrow">{{ attendeesSort.sortArrow(col.key) }}</span>
                </th>
                <th />
              </tr>
            </thead>
            <tbody>
              <tr v-for="e in attendeeEntries" :key="e.userId">
                <td>{{ e.name }}</td>
                <td>{{ e.email }}</td>
                <td><span :class="['pill', e.attending]">{{ attendingLabels[e.attending] }}</span></td>
                <td>
                  <span v-if="e.hasLoggedIn" class="signed-in">Yes</span>
                  <span v-else class="signed-out">No</span>
                </td>
                <td>
                  <button type="button" class="btn-link danger" @click="removeAttendee(e.userId, e.name)">Remove</button>
                </td>
              </tr>
              <tr v-if="attendeeEntries.length === 0">
                <td colspan="5" class="muted">No matching attendees.</td>
              </tr>
            </tbody>
          </table>
        </template>
        <p v-else class="muted">No attendees yet.</p>
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
  background: var(--bg-2);
  color: var(--muted);
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  margin-left: 0.4rem;
}
.badge.late {
  font-size: 0.7rem;
  text-transform: uppercase;
  background: rgb(var(--rust-rgb) / 0.12);
  color: var(--danger);
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
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
  flex-wrap: wrap;
  justify-content: flex-end;
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
.reload {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  color: var(--muted);
  font-size: 0.85rem;
  flex-shrink: 0;
}
.reload select {
  padding: 0.3rem 0.4rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  min-width: 4rem;
}
.right .btn {
  flex-shrink: 0;
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
.search {
  padding: 0.3rem 0.5rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font-size: 0.85rem;
  flex: 1 1 12rem;
  min-width: 8rem;
  max-width: 20rem;
}
.grid th.sortable {
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
}
.grid th.sortable:hover {
  color: var(--text);
}
.grid th.sorted {
  color: var(--text);
}
.grid th .arrow {
  display: inline-block;
  width: 0.9em;
  margin-left: 0.2em;
  font-size: 0.7em;
}
.pill {
  font-size: 0.8rem;
  padding: 0.15rem 0.55rem;
  border-radius: 999px;
}
.pill.yes { background: rgb(var(--success-rgb) / 0.14); color: var(--ok); }
.pill.no { background: rgb(var(--rust-rgb) / 0.12); color: var(--danger); }
.pill.not_sure { background: rgb(var(--accent-rgb) / 0.15); color: var(--accent-2); }
.pill.no_response { background: var(--bg-2); color: var(--muted); }
.badge.muted-badge {
  font-size: 0.7rem;
  text-transform: uppercase;
  background: var(--bg-2);
  color: var(--muted);
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  margin-left: 0.4rem;
}
.signed-in {
  font-size: 0.8rem;
  color: var(--ok);
}
.signed-out {
  font-size: 0.8rem;
  font-weight: 600;
  color: var(--danger);
}
.add-row {
  display: flex;
  gap: 0.75rem;
  align-items: center;
  margin: 1rem 0;
}
.picker {
  flex: 1;
  max-width: 28rem;
  padding: 0.4rem 0.5rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.btn-link {
  border: none;
  background: none;
  padding: 0;
  cursor: pointer;
  color: var(--accent);
}
.btn-link.danger { color: var(--danger); }
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
