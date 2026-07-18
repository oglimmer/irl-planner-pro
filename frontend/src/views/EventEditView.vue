<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, errMsg } from '../api'
import { COMMON_TIMEZONES, defaultDayType, eventDayRange, formatDate } from '../lib/datetime'
import { useAuthStore } from '../stores/auth'
import type { AdminNotifPref, DayType, EventInput, NotificationsInput } from '../types'

const props = defineProps<{ id?: string }>()
const router = useRouter()
const auth = useAuthStore()

const isEdit = computed(() => !!props.id)
const loading = ref(false)
const saving = ref(false)
const error = ref('')

const form = reactive<EventInput>({
  slug: '',
  name: '',
  country: '',
  city: '',
  hotelName: '',
  hotelAddress: '',
  hotelLink: '',
  timezone: auth.defaultEventTimezone || 'Europe/Paris',
  startDate: '',
  endDate: '',
  submissionDeadlineLocal: '',
  reminderDaysBefore: 3,
  weeklyReminders: true,
  reminderHour: 9,
  dailyActivityEmail: false,
})

// Day types keyed by YYYY-MM-DD, preserved across range edits where possible.
const dayTypes = reactive<Record<string, DayType>>({})

// Cover image. The chosen file is uploaded separately after the event is saved
// (it needs the event id); existing images come back as a URL on the event.
const MAX_IMAGE_BYTES = 4 * 1024 * 1024
const fileInput = ref<HTMLInputElement | null>(null)
const imageFile = ref<File | null>(null) // newly chosen file, pending upload
const existingImageUrl = ref('') // image already stored on the server
const removeExisting = ref(false) // user cleared the existing image
const objectUrl = ref('') // preview URL for the chosen file (must be revoked)

const previewUrl = computed(() => {
  if (objectUrl.value) return objectUrl.value
  if (existingImageUrl.value && !removeExisting.value) return existingImageUrl.value
  return ''
})

function setImageFile(f: File | null) {
  if (objectUrl.value) {
    URL.revokeObjectURL(objectUrl.value)
    objectUrl.value = ''
  }
  imageFile.value = f
  if (f) {
    objectUrl.value = URL.createObjectURL(f)
    removeExisting.value = false
  }
}

function onImageChange(e: globalThis.Event) {
  error.value = ''
  const f = (e.target as HTMLInputElement).files?.[0] ?? null
  if (f && f.size > MAX_IMAGE_BYTES) {
    error.value = 'Image is too large (max 4 MB).'
    if (fileInput.value) fileInput.value.value = ''
    return
  }
  setImageFile(f)
}

function removeImage() {
  setImageFile(null)
  removeExisting.value = true
  if (fileInput.value) fileInput.value.value = ''
}

onBeforeUnmount(() => {
  if (objectUrl.value) URL.revokeObjectURL(objectUrl.value)
})

const tzOptions = computed(() => {
  const set = new Set(COMMON_TIMEZONES)
  if (form.timezone) set.add(form.timezone)
  return [...set]
})

// The ordered list of dates between start and end (inclusive).
const dayList = computed<string[]>(() => eventDayRange(form.startDate, form.endDate))

// Keep dayTypes in sync with the current range: default first/last to travel and
// the rest to event, but never clobber a value the user already set.
watch(
  dayList,
  (dates) => {
    for (const k of Object.keys(dayTypes)) {
      if (!dates.includes(k)) delete dayTypes[k]
    }
    dates.forEach((date, i) => {
      if (!dayTypes[date]) {
        dayTypes[date] = defaultDayType(i, dates.length)
      }
    })
  },
  { immediate: true },
)

function toggleDay(date: string) {
  dayTypes[date] = dayTypes[date] === 'travel' ? 'event' : 'travel'
}

async function loadForEdit() {
  if (!props.id) return
  loading.value = true
  try {
    const e = await api.getEvent(props.id)
    Object.assign(form, {
      slug: e.slug,
      name: e.name,
      country: e.country,
      city: e.city,
      hotelName: e.hotelName,
      hotelAddress: e.hotelAddress,
      hotelLink: e.hotelLink,
      timezone: e.timezone,
      startDate: e.startDate,
      endDate: e.endDate,
      submissionDeadlineLocal: e.submissionDeadlineLocal,
      reminderDaysBefore: e.reminderDaysBefore,
      weeklyReminders: e.weeklyReminders,
      reminderHour: e.reminderHour,
      dailyActivityEmail: e.dailyActivityEmail,
    })
    existingImageUrl.value = e.imageUrl
    removeExisting.value = false
    setImageFile(null)
    for (const k of Object.keys(dayTypes)) delete dayTypes[k]
    e.days.forEach((d) => (dayTypes[d.date] = d.type))
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    loading.value = false
  }
}

// --- Notifications (per-event admin matrix) --------------------------------
// Edit-mode only: needs the event id and the admin roster. The IRL team
// daily-summary toggle is `form.dailyActivityEmail` (the events column), kept
// in sync here so the main form's save and this section never disagree.
const notifLoading = ref(false)
const notifSaving = ref(false)
const notifError = ref('')
const notifNotice = ref('')
const irlTeamEmail = ref('')
const emailConfigured = ref(false)
const slackConfigured = ref(false)
const notifPrefs = ref<AdminNotifPref[]>([])

async function loadNotifications() {
  if (!props.id) return
  notifLoading.value = true
  notifError.value = ''
  try {
    const n = await api.getNotifications(props.id)
    form.dailyActivityEmail = n.irlTeamDailySummary
    irlTeamEmail.value = n.irlTeamEmail
    emailConfigured.value = n.channels.find((c) => c.name === 'email')?.configured ?? false
    slackConfigured.value = n.channels.find((c) => c.name === 'slack')?.configured ?? false
    notifPrefs.value = n.admins
  } catch (e) {
    notifError.value = errMsg(e)
  } finally {
    notifLoading.value = false
  }
}

// When an admin switches from "off" to a stream, default to a configured
// channel so the row is valid (the backend rejects a stream with no channel).
function ensureChannel(a: AdminNotifPref) {
  if (a.notifType !== '' && !a.viaEmail && !a.viaSlack) {
    if (emailConfigured.value) a.viaEmail = true
    else if (slackConfigured.value) a.viaSlack = true
  }
}

async function saveNotifications() {
  if (!props.id) return
  notifSaving.value = true
  notifError.value = ''
  notifNotice.value = ''
  const payload: NotificationsInput = {
    irlTeamDailySummary: form.dailyActivityEmail,
    admins: notifPrefs.value.map((a) => ({
      userId: a.userId,
      notifType: a.notifType,
      viaEmail: a.notifType === '' ? false : a.viaEmail,
      viaSlack: a.notifType === '' ? false : a.viaSlack,
    })),
  }
  try {
    const n = await api.saveNotifications(props.id, payload)
    notifPrefs.value = n.admins
    form.dailyActivityEmail = n.irlTeamDailySummary
    notifNotice.value = 'Notification settings saved.'
  } catch (e) {
    notifError.value = errMsg(e)
  } finally {
    notifSaving.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  const payload: EventInput = {
    ...form,
    days: dayList.value.map((date) => ({ date, type: dayTypes[date] })),
  }
  try {
    const saved = props.id
      ? await api.updateEvent(props.id, payload)
      : await api.createEvent(payload)
    // Image is a separate request — it needs the (possibly brand-new) event id.
    if (imageFile.value) {
      await api.uploadEventImage(saved.id, imageFile.value)
    } else if (removeExisting.value && existingImageUrl.value) {
      await api.deleteEventImage(saved.id)
    }
    router.push(`/admin/events/${saved.id}`)
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await loadForEdit()
  await loadNotifications()
})
</script>

<template>
  <section class="event-form">
    <header class="page-head">
      <h1>{{ isEdit ? 'Edit event' : 'New event' }}</h1>
      <p class="muted">
        Configure where and when the offsite happens, then attendees RSVP from the
        shareable link.
      </p>
    </header>

    <p v-if="loading" class="muted">Loading…</p>

    <form v-else class="form" @submit.prevent="save">
      <!-- Basics ───────────────────────────────────────────── -->
      <fieldset class="section">
        <legend class="section-title">Basics</legend>
        <div class="grid">
          <label class="field full">
            <span class="field-label">Event name</span>
            <input v-model="form.name" type="text" required placeholder="IRL Dubrovnik October 2026">
          </label>
          <label class="field full">
            <span class="field-label">URL slug</span>
            <input v-model="form.slug" type="text" required placeholder="dubrovnik-oct-2026">
            <small>Shareable link: <code>/events/{{ form.slug || '…' }}</code></small>
          </label>
        </div>
      </fieldset>

      <!-- Location & hotel ──────────────────────────────────── -->
      <fieldset class="section">
        <legend class="section-title">Location &amp; hotel</legend>
        <div class="grid">
          <label class="field">
            <span class="field-label">Country</span>
            <input v-model="form.country" type="text" placeholder="Croatia">
          </label>
          <label class="field">
            <span class="field-label">City</span>
            <input v-model="form.city" type="text" placeholder="Dubrovnik">
          </label>
          <label class="field full">
            <span class="field-label">Hotel name</span>
            <input v-model="form.hotelName" type="text">
          </label>
          <label class="field full">
            <span class="field-label">Hotel address</span>
            <input v-model="form.hotelAddress" type="text">
          </label>
          <label class="field full">
            <span class="field-label">Hotel link</span>
            <input v-model="form.hotelLink" type="url" placeholder="https://…">
          </label>
        </div>
      </fieldset>

      <!-- Cover image ───────────────────────────────────────── -->
      <fieldset class="section">
        <legend class="section-title">Cover image</legend>
        <p class="muted small">
          Shown on the home page and the event's RSVP page. JPEG, PNG, GIF or WebP, up to 4 MB.
        </p>
        <div class="image-row">
          <div class="image-preview" :class="{ empty: !previewUrl }">
            <img v-if="previewUrl" :src="previewUrl" alt="Event cover preview">
            <span v-else class="image-placeholder">No image</span>
          </div>
          <div class="image-actions">
            <label class="btn secondary file-trigger">
              {{ previewUrl ? 'Replace image' : 'Choose image' }}
              <input
                ref="fileInput"
                type="file"
                accept="image/jpeg,image/png,image/gif,image/webp"
                @change="onImageChange"
              >
            </label>
            <button v-if="previewUrl" type="button" class="btn danger" @click="removeImage">
              Remove
            </button>
          </div>
        </div>
      </fieldset>

      <!-- Schedule ──────────────────────────────────────────── -->
      <fieldset class="section">
        <legend class="section-title">Schedule</legend>
        <div class="grid">
          <label class="field full">
            <span class="field-label">Timezone</span>
            <select v-model="form.timezone">
              <option v-for="tz in tzOptions" :key="tz" :value="tz">{{ tz }}</option>
            </select>
            <small>All dates and times below are shown in this timezone.</small>
          </label>
          <label class="field">
            <span class="field-label">Start date</span>
            <input v-model="form.startDate" type="date" required>
          </label>
          <label class="field">
            <span class="field-label">End date</span>
            <input v-model="form.endDate" type="date" required>
          </label>
          <label class="field full">
            <span class="field-label">Submission deadline · {{ form.timezone }}</span>
            <input v-model="form.submissionDeadlineLocal" type="datetime-local" required>
          </label>
        </div>

        <div v-if="dayList.length" class="days-block">
          <span class="field-label">Days</span>
          <p class="muted small">Tap a day to switch between travel and event. First and last default to travel.</p>
          <div class="days">
            <button
              v-for="date in dayList"
              :key="date"
              type="button"
              :class="['day', dayTypes[date]]"
              @click="toggleDay(date)"
            >
              <span class="day-date">{{ formatDate(date) }}</span>
              <span class="day-type">{{ dayTypes[date] }}</span>
            </button>
          </div>
        </div>
      </fieldset>

      <!-- Reminders ─────────────────────────────────────────── -->
      <fieldset class="section">
        <legend class="section-title">Reminders</legend>
        <label class="check">
          <input v-model="form.weeklyReminders" type="checkbox">
          <span>Send a weekly reminder to non-responders</span>
        </label>
        <div class="grid">
          <label class="field">
            <span class="field-label">Days before deadline for daily reminders</span>
            <input v-model.number="form.reminderDaysBefore" type="number" min="0">
          </label>
          <label class="field">
            <span class="field-label">Send hour · 0–23 · {{ form.timezone }}</span>
            <input v-model.number="form.reminderHour" type="number" min="0" max="23">
          </label>
        </div>
        <p class="section-note muted">
          Reminders are delivered to each non-responder by email and Slack DM,
          whichever channels are configured on the server.
        </p>
      </fieldset>

      <!-- Notifications (edit-mode only) ─────────────────────── -->
      <fieldset v-if="isEdit" class="section">
        <legend class="section-title">Notifications</legend>
        <p class="muted">Who gets told about activity on this event.</p>

        <label class="check">
          <input v-model="form.dailyActivityEmail" type="checkbox">
          <span>
            Send a daily summary to the IRL team
            <code v-if="irlTeamEmail">{{ irlTeamEmail }}</code>
            <em v-else class="muted">(IRL_TEAM_EMAIL not configured)</em>
          </span>
        </label>

        <p v-if="notifLoading" class="muted">Loading admins…</p>
        <table v-else-if="notifPrefs.length" class="notif-matrix">
          <thead>
            <tr>
              <th>Admin</th>
              <th>Off</th>
              <th>Daily summary</th>
              <th>Any activity</th>
              <th>Email</th>
              <th>Slack</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in notifPrefs" :key="a.userId">
              <td class="who">
                <span class="who-name">{{ a.name || a.email }}</span>
                <small class="who-email">{{ a.email }}</small>
              </td>
              <td><input v-model="a.notifType" type="radio" :name="`nt-${a.userId}`" value=""></td>
              <td>
                <input
                  v-model="a.notifType" type="radio" :name="`nt-${a.userId}`" value="daily"
                  @change="ensureChannel(a)"
                >
              </td>
              <td>
                <input
                  v-model="a.notifType" type="radio" :name="`nt-${a.userId}`" value="activity"
                  @change="ensureChannel(a)"
                >
              </td>
              <td>
                <input
                  v-model="a.viaEmail" type="checkbox"
                  :disabled="a.notifType === '' || !emailConfigured"
                  :title="emailConfigured ? '' : 'email not configured (SMTP_HOST)'"
                >
              </td>
              <td>
                <input
                  v-model="a.viaSlack" type="checkbox"
                  :disabled="a.notifType === '' || !slackConfigured"
                  :title="slackConfigured ? '' : 'Slack not configured (SLACK_BOT_TOKEN)'"
                >
              </td>
            </tr>
          </tbody>
        </table>
        <p v-else class="muted">No admins yet.</p>

        <p v-if="notifError" class="error">{{ notifError }}</p>
        <p v-if="notifNotice" class="muted">{{ notifNotice }}</p>
        <div class="actions">
          <button type="button" class="btn secondary" :disabled="notifSaving" @click="saveNotifications">
            {{ notifSaving ? 'Saving…' : 'Save notification settings' }}
          </button>
        </div>
      </fieldset>

      <p v-if="error" class="error">{{ error }}</p>

      <div class="actions">
        <RouterLink to="/admin/events" class="btn secondary">Cancel</RouterLink>
        <button class="btn" type="submit" :disabled="saving">
          {{ saving ? 'Saving…' : isEdit ? 'Save changes' : 'Create event' }}
        </button>
      </div>
    </form>
  </section>
</template>

<style scoped>
.event-form {
  max-width: 720px;
}
.page-head {
  margin-bottom: 32px;
}
.page-head .muted {
  max-width: 56ch;
  margin: 0;
}

.form {
  display: flex;
  flex-direction: column;
  gap: 0;
}

/* Sections — hairline-separated editorial blocks ─────────────── */
.section {
  border: 0;
  border-top: 1px solid var(--border-soft);
  margin: 0;
  padding: 28px 0 32px;
  min-width: 0; /* let grid children shrink inside the fieldset */
}
.section:first-of-type {
  border-top: 0;
  padding-top: 0;
}
.section-title {
  padding: 0;
  margin: 0 0 20px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.26em;
  text-transform: uppercase;
  color: var(--text-soft);
}

/* Field grid ─────────────────────────────────────────────────── */
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 18px 28px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.field.full {
  grid-column: 1 / -1;
}
.field-label {
  font-family: var(--mono);
  font-size: 10.5px;
  font-weight: 500;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--muted);
}
small {
  color: var(--muted);
  font-size: 12px;
}
small code {
  color: var(--accent-2);
}
.small {
  font-size: 12.5px;
}
.muted.small {
  margin: -4px 0 16px;
}

/* Checkboxes ─────────────────────────────────────────────────── */
.check {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13.5px;
  color: var(--text);
  cursor: pointer;
  padding: 6px 0;
}
.check input[type='checkbox'] {
  width: 16px;
  height: 16px;
  accent-color: var(--accent);
  cursor: pointer;
}
.check + .grid {
  margin: 10px 0;
}

/* Days ───────────────────────────────────────────────────────── */
.days-block {
  margin-top: 24px;
}
.days-block > .field-label {
  display: block;
  margin-bottom: 4px;
}
.days {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}
.day {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  border: 1px solid var(--border);
  border-radius: 0;
  background: var(--panel);
  color: var(--text);
  padding: 8px 12px;
  cursor: pointer;
  text-transform: none;
  letter-spacing: normal;
  font-weight: 400;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}
.day:hover {
  border-color: var(--accent);
  background: var(--panel);
}
.day.travel {
  border-color: var(--accent);
  background: rgb(var(--accent-rgb) / 0.14);
}
.day.event {
  border-color: var(--border);
  background: var(--panel);
}
.day-date {
  font-size: 13px;
  font-family: var(--mono);
  color: var(--text);
}
.day-type {
  font-size: 9.5px;
  text-transform: uppercase;
  letter-spacing: 0.16em;
  color: var(--muted);
}
.day.travel .day-type {
  color: var(--accent-2);
}

/* Cover image ────────────────────────────────────────────────── */
.image-row {
  display: flex;
  align-items: flex-start;
  gap: 20px;
  flex-wrap: wrap;
}
.image-preview {
  width: 240px;
  aspect-ratio: 16 / 9;
  border: 1px solid var(--border);
  background: var(--panel-2);
  overflow: hidden;
}
.image-preview img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.image-preview.empty {
  display: flex;
  align-items: center;
  justify-content: center;
  border-style: dashed;
}
.image-placeholder {
  font-size: 11px;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--muted);
}
.image-actions {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.file-trigger {
  cursor: pointer;
}
.file-trigger input[type='file'] {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  border: 0;
}

/* Actions ────────────────────────────────────────────────────── */
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  border-top: 1px solid var(--border-soft);
  padding-top: 24px;
  margin-top: 8px;
}
.muted {
  color: var(--muted);
}
.section-note {
  margin: 16px 0 0;
  max-width: 60ch;
  font-size: 0.85rem;
  line-height: 1.45;
}

@media (max-width: 560px) {
  .grid {
    grid-template-columns: 1fr;
  }
}

/* Notification matrix ─────────────────────────────────────────── */
.notif-matrix {
  width: 100%;
  border-collapse: collapse;
  margin-top: 16px;
  font-size: 0.92rem;
}
.notif-matrix th,
.notif-matrix td {
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-soft);
  text-align: center;
}
.notif-matrix thead th {
  font-weight: 600;
  color: var(--muted);
  white-space: nowrap;
}
.notif-matrix th:first-child,
.notif-matrix td.who {
  text-align: left;
}
.notif-matrix .who-name {
  display: block;
}
.notif-matrix .who-email {
  color: var(--muted);
}
.notif-matrix input[type='radio'],
.notif-matrix input[type='checkbox'] {
  cursor: pointer;
}
.notif-matrix input:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}
</style>
