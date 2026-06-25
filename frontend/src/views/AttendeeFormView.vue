<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, errMsg } from '../api'
import { formatDate, formatInZone } from '../lib/datetime'
import ActivityLog from '../components/ActivityLog.vue'
import type { ActivityEntry, Attending, Event, SubmissionInput, TravelMode } from '../types'

const props = defineProps<{ slug: string }>()

const event = ref<Event | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const activity = ref<ActivityEntry[]>([])

const TRAVEL_MODES: { value: TravelMode; label: string }[] = [
  { value: 'flight', label: 'Flight' },
  { value: 'car', label: 'Car' },
  { value: 'train', label: 'Train' },
  { value: 'other', label: 'Other' },
]

const form = reactive<SubmissionInput>({
  firstName: '',
  lastName: '',
  attending: '' as Attending,
  notSureReason: '',
  arrivalDay: null,
  arrivalTime: '',
  arrivalMode: null,
  arrivalDetails: '',
  departureDay: null,
  departureTime: '',
  departureMode: null,
  departureDetails: '',
  longHaul: false,
  extraStayStart: null,
  extraStayEnd: null,
  allergies: '',
  comments: '',
})

const readOnly = computed(() => event.value?.isPast ?? false)

function addDays(ymd: string, n: number): string {
  const d = new Date(`${ymd}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + n)
  return d.toISOString().slice(0, 10)
}

// Travel dates the attendee may pick: event window ±1 day (the extra-night span).
const travelDates = computed<string[]>(() => {
  if (!event.value) return []
  const out: string[] = []
  let d = addDays(event.value.startDate, -1)
  const last = addDays(event.value.endDate, 1)
  while (d <= last) {
    out.push(d)
    d = addDays(d, 1)
  }
  return out
})

const beforeDate = computed(() => (event.value ? addDays(event.value.startDate, -1) : ''))
const afterDate = computed(() => (event.value ? addDays(event.value.endDate, 1) : ''))

const extraNightBefore = computed({
  get: () => form.extraStayStart != null,
  set: (v: boolean) => (form.extraStayStart = v ? beforeDate.value : null),
})
const extraNightAfter = computed({
  get: () => form.extraStayEnd != null,
  set: (v: boolean) => (form.extraStayEnd = v ? afterDate.value : null),
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    event.value = await api.getEventBySlug(props.slug)
    const existing = await api.getMySubmission(props.slug)
    if (existing) {
      Object.assign(form, {
        firstName: existing.firstName,
        lastName: existing.lastName,
        attending: existing.attending,
        notSureReason: existing.notSureReason,
        arrivalDay: existing.arrivalDay,
        arrivalTime: existing.arrivalTime,
        arrivalMode: existing.arrivalMode,
        arrivalDetails: existing.arrivalDetails,
        departureDay: existing.departureDay,
        departureTime: existing.departureTime,
        departureMode: existing.departureMode,
        departureDetails: existing.departureDetails,
        longHaul: existing.longHaul,
        extraStayStart: existing.extraStayStart,
        extraStayEnd: existing.extraStayEnd,
        allergies: existing.allergies,
        comments: existing.comments,
      })
    }
    activity.value = await api.myActivity(props.slug)
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    loading.value = false
  }
}

async function submit() {
  saving.value = true
  error.value = ''
  saved.value = false
  try {
    await api.putMySubmission(props.slug, { ...form })
    saved.value = true
    activity.value = await api.myActivity(props.slug)
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <section v-if="loading" class="muted">Loading…</section>

  <section v-else-if="!event" class="muted">
    <p class="error">{{ error || 'Event not found.' }}</p>
  </section>

  <section v-else class="attendee">
    <header class="ev-head">
      <h1>{{ event.name }}</h1>
      <p class="muted">
        {{ event.city }}{{ event.city && event.country ? ', ' : '' }}{{ event.country }}
        · {{ formatDate(event.startDate) }} → {{ formatDate(event.endDate) }}
      </p>
      <p v-if="event.hotelName" class="muted small">
        {{ event.hotelName }}<span v-if="event.hotelAddress"> · {{ event.hotelAddress }}</span>
      </p>
      <p class="deadline">
        Please respond by {{ formatInZone(event.submissionDeadline, event.timezone) }}
      </p>
      <p v-if="readOnly" class="locked">
        This event has ended — your response can no longer be edited. Contact the
        People team if something needs changing.
      </p>
    </header>

    <form class="form" @submit.prevent="submit">
      <fieldset :disabled="readOnly || saving">
        <div class="row">
          <label>First name <input v-model="form.firstName" type="text" required></label>
          <label>Last name <input v-model="form.lastName" type="text" required></label>
        </div>

        <div class="field">
          <span class="q">Are you attending?</span>
          <div class="radios">
            <label><input v-model="form.attending" type="radio" value="yes"> Yes</label>
            <label><input v-model="form.attending" type="radio" value="no"> No</label>
            <label><input v-model="form.attending" type="radio" value="not_sure"> Not sure</label>
          </div>
        </div>

        <!-- Not sure -->
        <div v-if="form.attending === 'not_sure'" class="field">
          <label>
            Why aren't you sure yet?
            <textarea v-model="form.notSureReason" rows="2" required />
          </label>
        </div>

        <!-- No -->
        <div v-if="form.attending === 'no'" class="notice">
          <p>If for any reason you cannot attend this offsite, please follow the steps below:</p>
          <ol>
            <li>Let your manager know</li>
            <li>Inform the People team by emailing <a href="mailto:people@id5.io">people@id5.io</a></li>
          </ol>
        </div>

        <!-- Yes -->
        <template v-if="form.attending === 'yes'">
          <h2>Travel</h2>
          <div class="leg">
            <h3>Arrival</h3>
            <div class="row">
              <label>Day
                <select v-model="form.arrivalDay" required>
                  <option :value="null" disabled>Select a day</option>
                  <option v-for="d in travelDates" :key="d" :value="d">{{ formatDate(d) }}</option>
                </select>
              </label>
              <label>Time <input v-model="form.arrivalTime" type="text" placeholder="14:30"></label>
            </div>
            <div class="row">
              <label>Travel mode
                <select v-model="form.arrivalMode" required>
                  <option :value="null" disabled>Select</option>
                  <option v-for="m in TRAVEL_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
                </select>
              </label>
              <label>Flight number / details
                <input v-model="form.arrivalDetails" type="text" placeholder="BA123, or other info">
              </label>
            </div>
          </div>

          <div class="leg">
            <h3>Departure</h3>
            <div class="row">
              <label>Day
                <select v-model="form.departureDay" required>
                  <option :value="null" disabled>Select a day</option>
                  <option v-for="d in travelDates" :key="d" :value="d">{{ formatDate(d) }}</option>
                </select>
              </label>
              <label>Time <input v-model="form.departureTime" type="text" placeholder="18:00"></label>
            </div>
            <div class="row">
              <label>Travel mode
                <select v-model="form.departureMode" required>
                  <option :value="null" disabled>Select</option>
                  <option v-for="m in TRAVEL_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
                </select>
              </label>
              <label>Flight number / details
                <input v-model="form.departureDetails" type="text" placeholder="BA456, or other info">
              </label>
            </div>
          </div>

          <div class="field">
            <label class="check">
              <input v-model="form.longHaul" type="checkbox">
              I'm a long-haul traveller (international flight of 7+ hours)
            </label>
          </div>

          <div v-if="form.longHaul" class="field extra">
            <span class="q">Would you require an extra night?</span>
            <label class="check">
              <input v-model="extraNightBefore" type="checkbox">
              Extra night before — {{ formatDate(beforeDate) }}
            </label>
            <label class="check">
              <input v-model="extraNightAfter" type="checkbox">
              Extra night after — {{ formatDate(afterDate) }}
            </label>
          </div>

          <h2>Other</h2>
          <label>Allergies / dietary preferences
            <textarea v-model="form.allergies" rows="2" />
          </label>
          <label>Comments
            <textarea v-model="form.comments" rows="2" />
          </label>
        </template>

        <p v-if="error" class="error">{{ error }}</p>
        <p v-if="saved" class="ok">Saved. Thank you!</p>

        <div v-if="!readOnly" class="actions">
          <button class="btn" type="submit" :disabled="saving || !form.attending">
            {{ saving ? 'Saving…' : 'Submit' }}
          </button>
        </div>
      </fieldset>
    </form>

    <section class="my-activity">
      <h2>My activity</h2>
      <ActivityLog :entries="activity" :timezone="event.timezone" />
    </section>
  </section>
</template>

<style scoped>
.attendee {
  max-width: 640px;
}
.ev-head h1 {
  margin-bottom: 0.25rem;
}
.muted {
  color: var(--muted);
}
.small {
  font-size: 0.85rem;
}
.deadline {
  margin-top: 0.75rem;
  font-weight: 600;
}
.locked {
  background: #fdf6e3;
  border: 1px solid #e8d9a8;
  border-radius: var(--radius);
  padding: 0.6rem 0.8rem;
  font-size: 0.9rem;
}
.form fieldset {
  border: none;
  padding: 0;
  margin: 1.5rem 0 0;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
h2 {
  font-size: 1.1rem;
  margin: 0.5rem 0 0;
  border-top: 1px solid var(--border);
  padding-top: 1rem;
}
h3 {
  font-size: 0.95rem;
  margin: 0 0 0.5rem;
  color: var(--muted);
}
label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.9rem;
  color: var(--muted);
}
label.check {
  flex-direction: row;
  align-items: center;
  gap: 0.5rem;
}
input,
select,
textarea {
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font: inherit;
  color: var(--text);
}
.row {
  display: flex;
  gap: 1rem;
}
.row > label {
  flex: 1;
}
.field .q {
  display: block;
  font-size: 0.9rem;
  color: var(--text);
  margin-bottom: 0.4rem;
}
.radios {
  display: flex;
  gap: 1.25rem;
}
.radios label {
  flex-direction: row;
  align-items: center;
  gap: 0.35rem;
  color: var(--text);
}
.leg {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1rem;
}
.extra {
  background: #fdf6e3;
  border: 1px solid #e8d9a8;
  border-radius: var(--radius);
  padding: 0.8rem 1rem;
}
.notice {
  background: #eef0ff;
  border: 1px solid #d4d9ff;
  border-radius: var(--radius);
  padding: 0.8rem 1rem;
}
.notice ol {
  margin: 0.5rem 0 0;
  padding-left: 1.2rem;
}
.error {
  color: var(--danger);
}
.ok {
  color: var(--ok);
}
.actions {
  margin-top: 0.5rem;
}
.my-activity {
  margin-top: 2.5rem;
  border-top: 1px solid var(--border);
  padding-top: 1.25rem;
}
</style>
