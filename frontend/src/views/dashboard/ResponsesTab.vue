<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, errMsg } from '../../api'
import { formatDate, formatInZone } from '../../lib/datetime'
import { attendingLabels, attendingRank, matchesQuery } from '../../lib/attending'
import { useColumnSort } from '../../composables/useColumnSort'
import { useColumnConfig } from '../../composables/useColumnConfig'
import type { AutoReloadOption } from '../../composables/useAutoReload'
import AttendingFilter from '../../components/AttendingFilter.vue'
import ColumnPicker from '../../components/ColumnPicker.vue'
import AdminSubmissionEditor from './AdminSubmissionEditor.vue'
import type { AttendingState, Dashboard, DashboardEntry, Submission } from '../../types'

const props = defineProps<{
  dashboard: Dashboard | null
  submissions: Submission[]
  eventId: string
  eventSlug: string
  timezone: string
  intervalMs: number
  reloadOptions: AutoReloadOption[]
}>()

const emit = defineEmits<{
  'update:intervalMs': [number]
  error: [string]
  // An admin edit was saved; the parent refetches the dashboard + submissions.
  saved: []
}>()

// The refresh <select> drives the parent-owned auto-reload via v-model.
const interval = computed({
  get: () => props.intervalMs,
  set: (v) => emit('update:intervalMs', v),
})

const filter = ref<AttendingState[]>([])
const search = ref('')

// A ResponseRow is a dashboard entry joined to that user's submission (null for
// non-responders), so columns can read either side.
interface ResponseRow extends DashboardEntry {
  sub: Submission | null
}

const submissionByUser = computed(() => {
  const m = new Map<string, Submission>()
  for (const s of props.submissions) m.set(s.userId, s)
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
  { key: 'extraStaySelfFunded', label: 'Self-funded early arrival', text: (r) => yesNo(r.sub?.extraStaySelfFunded) },
  { key: 'allergies', label: 'Allergies', text: (r) => r.sub?.allergies ?? '' },
  { key: 'comments', label: 'Comments', text: (r) => r.sub?.comments ?? '' },
  {
    key: 'travelCost',
    label: 'Travel cost',
    text: (r) => (r.sub?.travelCost != null ? `${r.sub.travelCost.toFixed(2)} ${r.sub.travelCostCurrency}` : ''),
    sort: (r) => r.sub?.travelCost ?? -1,
  },
  {
    key: 'updatedAt',
    label: 'Last updated',
    text: (r) => (r.sub ? formatInZone(r.sub.updatedAt, props.timezone) : ''),
    sort: (r) => r.sub?.updatedAt ?? '',
  },
]

const responseColumnByKey = new Map(responseColumns.map((c) => [c.key, c]))
const responseColumnKeys = responseColumns.map((c) => c.key)
const responseColumnDefaults = ['name', 'email', 'attending', 'status']

const sort = useColumnSort<string>()
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

const filteredEntries = computed(() => {
  const d = props.dashboard
  if (!d) return []
  let rows: ResponseRow[] = d.entries.map((e) => ({
    ...e,
    sub: submissionByUser.value.get(e.userId) ?? null,
  }))

  if (filter.value.length) {
    const set = new Set(filter.value)
    rows = rows.filter((e) => set.has(e.attending))
  }

  const q = search.value.trim().toLowerCase()
  if (q) rows = rows.filter((e) => matchesQuery(q, e.name, e.email))

  return sort.sortRows(rows, responseSortValue)
})

// The attendee whose response is open in the admin editor (null = closed).
const editing = ref<ResponseRow | null>(null)

function onEditorSaved() {
  editing.value = null
  emit('saved')
}

async function exportCsv() {
  try {
    const blob = await api.fetchExport(props.eventId, filter.value)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${props.eventSlug || 'event'}-responses.csv`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    emit('error', errMsg(e))
  }
}
</script>

<template>
  <div>
    <div v-if="dashboard" class="responses">
      <div class="responses-controls">
        <div class="controls-row">
          <div class="search-wrap">
            <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <circle cx="11" cy="11" r="7" />
              <line x1="20" y1="20" x2="16.65" y2="16.65" />
            </svg>
            <input
              v-model="search"
              type="search"
              class="search"
              placeholder="Search name or email…"
              aria-label="Search responses by name or email"
            >
          </div>
          <div class="tools">
            <label class="reload">
              Refresh
              <select v-model.number="interval">
                <option v-for="o in reloadOptions" :key="o.label" :value="o.ms">{{ o.label }}</option>
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
                :class="{ sorted: sort.isSorted(col.key) }"
                :aria-sort="sort.ariaSort(col.key)"
                role="button"
                tabindex="0"
                @click="sort.toggleSort(col.key)"
                @keydown.enter.prevent="sort.toggleSort(col.key)"
                @keydown.space.prevent="sort.toggleSort(col.key)"
              >
                {{ col.label }}<span class="arrow">{{ sort.sortArrow(col.key) }}</span>
              </th>
              <th class="actions-col">Edit</th>
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
              <td class="actions-col">
                <button type="button" class="btn-link" @click="editing = e">Edit</button>
                <span v-if="e.sub?.locked" class="lock" title="Finalized by an organizer — locked for the attendee">🔒</span>
              </td>
            </tr>
            <tr v-if="filteredEntries.length === 0">
              <td :colspan="visibleColumns.length + 1" class="muted">No matching attendees.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <p v-else class="muted">Loading…</p>

    <AdminSubmissionEditor
      v-if="editing"
      :event-id="eventId"
      :user-id="editing.userId"
      :name="editing.name"
      :email="editing.email"
      :submission="editing.sub"
      @close="editing = null"
      @saved="onEditorSaved"
    />
  </div>
</template>

<style scoped>
.muted { color: var(--muted); }
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
.count {
  margin: 0;
  font-size: 0.85rem;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
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
.grid th.sortable {
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
}
.grid th.sortable:hover { color: var(--text); }
.grid th.sorted { color: var(--text); }
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
.badge.late {
  font-size: 0.7rem;
  text-transform: uppercase;
  background: rgb(var(--rust-rgb) / 0.12);
  color: var(--danger);
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
}
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
.actions-col {
  white-space: nowrap;
  text-align: right;
}
.btn-link {
  border: none;
  background: none;
  padding: 0;
  cursor: pointer;
  color: var(--accent);
  font-size: 0.85rem;
}
.lock {
  margin-left: 0.4rem;
  font-size: 0.8rem;
}
</style>
