<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, errMsg } from '../api'
import { formatDate, formatInZone } from '../lib/datetime'
import { useAutoReload } from '../composables/useAutoReload'
import { useColumnSort } from '../composables/useColumnSort'
import { useColumnConfig } from '../composables/useColumnConfig'
import { useConfirm } from '../composables/useConfirm'
import ActivityLog from '../components/ActivityLog.vue'
import AttendingFilter from '../components/AttendingFilter.vue'
import ColumnPicker from '../components/ColumnPicker.vue'
import EventMessaging from './EventMessaging.vue'
import type {
  ActivityEntry,
  AttendingState,
  Dashboard,
  DashboardEntry,
  Event,
  Submission,
  UserSummary,
} from '../types'

const props = defineProps<{ id: string }>()

const { confirm } = useConfirm()

const event = ref<Event | null>(null)
const error = ref('')
const copied = ref(false)
const tab = ref<'responses' | 'activity' | 'attendees' | 'messaging'>('responses')

const dashboard = ref<Dashboard | null>(null)
const submissions = ref<Submission[]>([])
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

// Responses table is a configurable data table: every attendee + submission field
// below is available, and the admin picks which to show (persisted per-browser).
// A ResponseRow is a dashboard entry joined to that user's submission (null for
// non-responders), so columns can read either side.
interface ResponseRow extends DashboardEntry {
  sub: Submission | null
}

const submissionByUser = computed(() => {
  const m = new Map<string, Submission>()
  for (const s of submissions.value) m.set(s.userId, s)
  return m
})

function fmtDay(d: string | null | undefined): string {
  return d ? formatDate(d) : ''
}
function yesNo(b: boolean | undefined): string {
  return b === undefined ? '' : b ? 'Yes' : 'No'
}
function cap(s: string): string {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : ''
}

// Each column knows how to render a plain-text cell (`text`) and how to produce a
// comparable sort value. A few columns render rich cells instead of text, flagged
// by `kind`; for those `text` still feeds the sort value unless `sort` overrides.
interface ResponseColumn {
  key: string
  label: string
  kind?: 'attending' | 'status' | 'signedIn'
  text: (r: ResponseRow) => string
  sort?: (r: ResponseRow) => string | number
}

const responseColumns: ResponseColumn[] = [
  { key: 'name', label: 'Name', text: (r) => r.name },
  { key: 'email', label: 'Email', text: (r) => r.email },
  {
    key: 'attending',
    label: 'Attending',
    kind: 'attending',
    text: (r) => attendingLabels[r.attending],
    sort: (r) => attendingRank[r.attending],
  },
  {
    key: 'status',
    label: 'Status',
    kind: 'status',
    text: (r) => `${r.hasLoggedIn ? '' : 'not signed in'} ${r.afterDeadlineEdit ? 'edited after deadline' : ''}`.trim(),
    sort: (r) => (r.afterDeadlineEdit ? 2 : 0) + (r.hasLoggedIn ? 0 : 1),
  },
  {
    key: 'signedIn',
    label: 'Signed in',
    kind: 'signedIn',
    text: (r) => (r.hasLoggedIn ? 'Yes' : 'No'),
    sort: (r) => (r.hasLoggedIn ? 1 : 0),
  },
  { key: 'notSureReason', label: 'Not-sure reason', text: (r) => r.sub?.notSureReason ?? '' },
  { key: 'arrivalDay', label: 'Arrival day', text: (r) => fmtDay(r.sub?.arrivalDay) },
  { key: 'arrivalTime', label: 'Arrival time', text: (r) => r.sub?.arrivalTime ?? '' },
  { key: 'arrivalMode', label: 'Arrival mode', text: (r) => cap(r.sub?.arrivalMode ?? '') },
  { key: 'arrivalDetails', label: 'Arrival details', text: (r) => r.sub?.arrivalDetails ?? '' },
  { key: 'arrivalIndependent', label: 'Arrival self-arranged', text: (r) => yesNo(r.sub?.arrivalIndependent) },
  { key: 'departureDay', label: 'Departure day', text: (r) => fmtDay(r.sub?.departureDay) },
  { key: 'departureTime', label: 'Departure time', text: (r) => r.sub?.departureTime ?? '' },
  { key: 'departureMode', label: 'Departure mode', text: (r) => cap(r.sub?.departureMode ?? '') },
  { key: 'departureDetails', label: 'Departure details', text: (r) => r.sub?.departureDetails ?? '' },
  { key: 'departureIndependent', label: 'Departure self-arranged', text: (r) => yesNo(r.sub?.departureIndependent) },
  { key: 'longHaul', label: 'Long haul', text: (r) => yesNo(r.sub?.longHaul) },
  { key: 'extraStayStart', label: 'Extra night before', text: (r) => fmtDay(r.sub?.extraStayStart) },
  { key: 'extraStayEnd', label: 'Extra night after', text: (r) => fmtDay(r.sub?.extraStayEnd) },
  { key: 'allergies', label: 'Allergies', text: (r) => r.sub?.allergies ?? '' },
  { key: 'comments', label: 'Comments', text: (r) => r.sub?.comments ?? '' },
  {
    key: 'updatedAt',
    label: 'Last updated',
    text: (r) => (r.sub ? formatInZone(r.sub.updatedAt, event.value?.timezone ?? 'UTC') : ''),
    sort: (r) => r.sub?.updatedAt ?? '',
  },
]

const responseColumnByKey = new Map(responseColumns.map((c) => [c.key, c]))
const responseColumnKeys = responseColumns.map((c) => c.key)
const responseColumnDefaults = ['name', 'email', 'attending', 'status']

const responsesSort = useColumnSort<string>()
const { selected: selectedColumns, reset: resetColumns } = useColumnConfig(
  'irl.responseColumns.v1',
  responseColumnKeys,
  responseColumnDefaults,
)

// When the table grows wide, the admin can break it out of the centered page
// column to use the full browser width. Persisted per-browser like the columns.
const fullWidth = ref(localStorage.getItem('irl.responsesFullWidth') === '1')
watch(fullWidth, (v) => {
  try {
    localStorage.setItem('irl.responsesFullWidth', v ? '1' : '0')
  } catch {
    // storage unavailable — toggle still works in-session
  }
})

// Visible columns are always rendered in canonical (definition) order, regardless
// of the order keys were toggled in.
const visibleColumns = computed(() => {
  const set = new Set(selectedColumns.value)
  return responseColumns.filter((c) => set.has(c.key))
})

function responseSortValue(r: ResponseRow, key: string): string | number {
  const col = responseColumnByKey.get(key)
  if (!col) return ''
  return col.sort ? col.sort(r) : col.text(r).toLowerCase()
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
  let rows: ResponseRow[] = d.entries.map((e) => ({
    ...e,
    sub: submissionByUser.value.get(e.userId) ?? null,
  }))

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
        <button :class="{ active: tab === 'messaging' }" @click="tab = 'messaging'">Messaging</button>
      </div>

      <!-- Responses -->
      <div v-show="tab === 'responses'">
        <div v-if="dashboard" class="responses">
          <div class="responses-controls">
            <div class="controls-row">
              <div class="search-wrap">
                <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
                  <circle cx="11" cy="11" r="7" />
                  <line x1="20" y1="20" x2="16.65" y2="16.65" />
                </svg>
                <input
                  v-model="responsesSearch"
                  type="search"
                  class="search"
                  placeholder="Search name or email…"
                  aria-label="Search responses by name or email"
                >
              </div>
              <div class="tools">
                <label class="reload">
                  Refresh
                  <select v-model.number="intervalMs">
                    <option v-for="o in options" :key="o.label" :value="o.ms">{{ o.label }}</option>
                  </select>
                </label>
                <ColumnPicker
                  v-model="selectedColumns"
                  v-model:full-width="fullWidth"
                  :columns="responseColumns"
                  @reset="resetColumns"
                />
                <button class="btn" @click="exportCsv">Export CSV</button>
              </div>
            </div>

            <div class="controls-row filter-row">
              <AttendingFilter v-model="filter" :counts="dashboard.counts" />
              <p class="muted count">
                {{ filteredEntries.length }} of {{ dashboard.total }} shown
              </p>
            </div>
          </div>

          <div class="table-scroll" :class="{ 'full-bleed': fullWidth }">
            <table class="grid">
              <thead>
                <tr>
                  <th
                    v-for="col in visibleColumns"
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
                  <td v-for="col in visibleColumns" :key="col.key">
                    <span
                      v-if="col.kind === 'attending'"
                      :class="['pill', e.attending]"
                    >{{ attendingLabels[e.attending] }}</span>
                    <template v-else-if="col.kind === 'status'">
                      <span v-if="!e.hasLoggedIn" class="badge muted-badge">not signed in</span>
                      <span v-if="e.afterDeadlineEdit" class="badge late">edited after deadline</span>
                    </template>
                    <template v-else-if="col.kind === 'signedIn'">
                      <span v-if="e.hasLoggedIn" class="signed-in">Yes</span>
                      <span v-else class="signed-out">No</span>
                    </template>
                    <template v-else>{{ col.text(e) }}</template>
                  </td>
                </tr>
                <tr v-if="filteredEntries.length === 0">
                  <td :colspan="visibleColumns.length" class="muted">No matching attendees.</td>
                </tr>
              </tbody>
            </table>
          </div>
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

      <!-- Messaging -->
      <div v-show="tab === 'messaging'">
        <EventMessaging :event-id="props.id" />
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

/* Responses tab control bar: a search + tools row, then a filter + count row. */
.responses-controls {
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
  margin: 0.25rem 0 1.1rem;
}
.controls-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem 1.25rem;
  flex-wrap: wrap;
}
.controls-row.filter-row {
  padding-top: 0.85rem;
  border-top: 1px solid var(--border);
}
.search-wrap {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1 1 16rem;
  max-width: 26rem;
}
.search-wrap .search-icon {
  position: absolute;
  left: 0.65rem;
  width: 1rem;
  height: 1rem;
  color: var(--muted);
  pointer-events: none;
}
.search-wrap .search {
  width: 100%;
  max-width: none;
  padding: 0.45rem 0.6rem 0.45rem 2.1rem;
}
.tools {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.tools .btn {
  flex-shrink: 0;
}
.count {
  margin: 0;
  font-size: 0.85rem;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.summary { margin: 0.75rem 0; }
/* The Responses table is configurable and can grow wide, so let it scroll
   horizontally rather than squeezing or overflowing the page. */
.table-scroll {
  overflow-x: auto;
}
/* Break the table out of the centered page column (main is max-width:1080px,
   margin:0 auto) so it spans the full browser width. The negative 50vw margins
   on a 50%-offset, 100vw-wide box pull it back to both viewport edges; the
   horizontal padding keeps the content off the very edge, matching main. */
.table-scroll.full-bleed {
  width: 100vw;
  position: relative;
  left: 50%;
  margin-left: -50vw;
  margin-right: -50vw;
  padding: 0 32px;
  box-sizing: border-box;
}
@media (max-width: 720px) {
  .table-scroll.full-bleed {
    padding: 0 18px;
  }
}
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
