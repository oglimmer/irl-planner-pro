<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, errMsg } from '../../api'
import { attendingLabels, attendingRank, matchesQuery } from '../../lib/attending'
import { useColumnSort } from '../../composables/useColumnSort'
import { useConfirm } from '../../composables/useConfirm'
import type { Dashboard, DashboardEntry, UserSummary } from '../../types'

const props = defineProps<{
  dashboard: Dashboard | null
  directory: UserSummary[]
  eventId: string
  eventName: string
}>()

// `changed` asks the parent to refetch the shared dashboard + directory after a
// membership change; `error` surfaces a failure on the page-level banner.
const emit = defineEmits<{
  changed: []
  error: [string]
}>()

const { confirm } = useConfirm()

const search = ref('')
type AttendeeKey = 'name' | 'email' | 'attending' | 'signedIn'
const sort = useColumnSort<AttendeeKey>()
const columns: { key: AttendeeKey; label: string }[] = [
  { key: 'name', label: 'Name' },
  { key: 'email', label: 'Email' },
  { key: 'attending', label: 'Attending' },
  { key: 'signedIn', label: 'Signed in' },
]
function sortValue(e: DashboardEntry, key: AttendeeKey): string | number {
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

// Attendees tab: directory rows with their own search + sort (no attending
// filter — this tab is about who's on the list, not who responded).
const entries = computed(() => {
  const d = props.dashboard
  if (!d) return []
  let rows = d.entries
  const q = search.value.trim().toLowerCase()
  if (q) rows = rows.filter((e) => matchesQuery(q, e.name, e.email))
  return sort.sortRows(rows, sortValue)
})

// Directory users not yet on this event's attendee list — the picker's options.
const addUserId = ref('')
const addableUsers = computed(() => {
  const taken = new Set((props.dashboard?.entries ?? []).map((e) => e.userId))
  return props.directory.filter((u) => !taken.has(u.id))
})

const uploadFile = ref<File | null>(null)
const uploadMsg = ref('')
const uploading = ref(false)

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
    const res = await api.importAttendees(props.eventId, uploadFile.value)
    uploadMsg.value = `Added ${res.added}, skipped ${res.skipped}.`
    if (res.errors.length) uploadMsg.value += ` Issues: ${res.errors.slice(0, 3).join('; ')}`
    uploadFile.value = null
    emit('changed')
  } catch (e) {
    uploadMsg.value = errMsg(e)
  } finally {
    uploading.value = false
  }
}

async function addAttendee() {
  if (!addUserId.value) return
  try {
    await api.addAttendee(props.eventId, addUserId.value)
    addUserId.value = ''
    emit('changed')
  } catch (e) {
    emit('error', errMsg(e))
  }
}

async function removeAttendee(userId: string, name: string) {
  const ok = await confirm({
    title: 'Remove attendee?',
    message: `Remove ${name} from “${props.eventName}”? Their profile and any response are kept — only their place on this event's list is removed.`,
    confirmLabel: 'Remove',
    danger: true,
  })
  if (!ok) return
  try {
    await api.removeAttendee(props.eventId, userId)
    emit('changed')
  } catch (e) {
    emit('error', errMsg(e))
  }
}
</script>

<template>
  <div class="attendees">
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
          v-model="search"
          type="search"
          class="search"
          placeholder="Search name or email…"
          aria-label="Search attendees by name or email"
        >
        <p class="muted summary">
          {{ entries.length }} of {{ dashboard.entries.length }} attendees shown.
        </p>
      </div>

      <table class="grid">
        <thead>
          <tr>
            <th
              v-for="col in columns"
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
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in entries" :key="e.userId">
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
          <tr v-if="entries.length === 0">
            <td :colspan="columns.length + 1" class="muted">No matching attendees.</td>
          </tr>
        </tbody>
      </table>
    </template>
    <p v-else class="muted">No attendees yet.</p>
  </div>
</template>

<style scoped>
.muted { color: var(--muted); }
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  flex-wrap: wrap;
}
.summary { margin: 0.75rem 0; }
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
.upload {
  display: flex;
  gap: 0.75rem;
  align-items: center;
  margin: 1rem 0;
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
.signed-in {
  font-size: 0.8rem;
  color: var(--ok);
}
.signed-out {
  font-size: 0.8rem;
  font-weight: 600;
  color: var(--danger);
}
.btn-link {
  border: none;
  background: none;
  padding: 0;
  cursor: pointer;
  color: var(--accent);
}
.btn-link.danger { color: var(--danger); }
code {
  background: var(--bg);
  padding: 0.1rem 0.3rem;
  border-radius: 4px;
}
</style>
