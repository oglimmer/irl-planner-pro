<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, errMsg } from '../api'
import { COMMON_TIMEZONES, formatDate } from '../lib/datetime'
import { useAuthStore } from '../stores/auth'
import type { DayType, EventInput } from '../types'

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

const tzOptions = computed(() => {
  const set = new Set(COMMON_TIMEZONES)
  if (form.timezone) set.add(form.timezone)
  return [...set]
})

// The ordered list of dates between start and end (inclusive).
const dayList = computed<string[]>(() => {
  if (!form.startDate || !form.endDate) return []
  const start = new Date(`${form.startDate}T00:00:00Z`)
  const end = new Date(`${form.endDate}T00:00:00Z`)
  if (isNaN(start.getTime()) || isNaN(end.getTime()) || end < start) return []
  const out: string[] = []
  for (let d = new Date(start); d <= end; d.setUTCDate(d.getUTCDate() + 1)) {
    out.push(d.toISOString().slice(0, 10))
  }
  return out
})

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
        dayTypes[date] = i === 0 || i === dates.length - 1 ? 'travel' : 'event'
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
      timezone: e.timezone,
      startDate: e.startDate,
      endDate: e.endDate,
      submissionDeadlineLocal: e.submissionDeadlineLocal,
      reminderDaysBefore: e.reminderDaysBefore,
      weeklyReminders: e.weeklyReminders,
      reminderHour: e.reminderHour,
      dailyActivityEmail: e.dailyActivityEmail,
    })
    for (const k of Object.keys(dayTypes)) delete dayTypes[k]
    e.days.forEach((d) => (dayTypes[d.date] = d.type))
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    loading.value = false
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
    router.push(`/admin/events/${saved.id}`)
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    saving.value = false
  }
}

onMounted(loadForEdit)
</script>

<template>
  <section>
    <h1>{{ isEdit ? 'Edit event' : 'New event' }}</h1>
    <p v-if="loading" class="muted">Loading…</p>

    <form v-else class="form" @submit.prevent="save">
      <label>
        Event name
        <input v-model="form.name" type="text" required placeholder="IRL Dubrovnik October 2026">
      </label>
      <label>
        URL slug
        <input v-model="form.slug" type="text" required placeholder="dubrovnik-oct-2026">
        <small>Shareable link: /events/{{ form.slug || '…' }}</small>
      </label>

      <div class="row">
        <label>Country <input v-model="form.country" type="text"></label>
        <label>City <input v-model="form.city" type="text"></label>
      </div>
      <label>Hotel name <input v-model="form.hotelName" type="text"></label>
      <label>Hotel address <input v-model="form.hotelAddress" type="text"></label>

      <label>
        Timezone
        <select v-model="form.timezone">
          <option v-for="tz in tzOptions" :key="tz" :value="tz">{{ tz }}</option>
        </select>
        <small>All dates and times are shown in this timezone.</small>
      </label>

      <div class="row">
        <label>Start date <input v-model="form.startDate" type="date" required></label>
        <label>End date <input v-model="form.endDate" type="date" required></label>
      </div>

      <label>
        Submission deadline ({{ form.timezone }})
        <input v-model="form.submissionDeadlineLocal" type="datetime-local" required>
      </label>

      <fieldset v-if="dayList.length">
        <legend>Days</legend>
        <p class="muted small">Travel days vs event days. First and last default to travel.</p>
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
      </fieldset>

      <fieldset>
        <legend>Reminders</legend>
        <label class="check">
          <input v-model="form.weeklyReminders" type="checkbox">
          Send a weekly reminder to non-responders
        </label>
        <div class="row">
          <label>
            Days before deadline to send daily reminders
            <input v-model.number="form.reminderDaysBefore" type="number" min="0">
          </label>
          <label>
            Send hour (0–23, {{ form.timezone }})
            <input v-model.number="form.reminderHour" type="number" min="0" max="23">
          </label>
        </div>
        <label class="check">
          <input v-model="form.dailyActivityEmail" type="checkbox">
          Email me a daily activity digest (only on days with activity)
        </label>
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
.form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  max-width: 640px;
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
select {
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
small {
  color: var(--muted);
  font-size: 0.8rem;
}
.small {
  font-size: 0.85rem;
}
fieldset {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1rem;
}
legend {
  padding: 0 0.4rem;
  font-weight: 600;
  color: var(--text);
}
.days {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.day {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.15rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  padding: 0.45rem 0.7rem;
  cursor: pointer;
}
.day.travel {
  border-color: #c9a227;
  background: #fdf6e3;
}
.day.event {
  border-color: var(--accent);
  background: #eef0ff;
}
.day-type {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--muted);
}
.error {
  color: var(--danger);
}
.actions {
  display: flex;
  gap: 0.75rem;
}
.muted {
  color: var(--muted);
}
</style>
