<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, errMsg } from '../api'
import { useAuthStore } from '../stores/auth'
import { useConfirm } from '../composables/useConfirm'
import { addDays, formatDate, formatDateRange, formatDeadline, tripLength as tripLengthOf } from '../lib/datetime'
import { extraNightErrors, fieldChecks, missingRequiredCount, type FieldKey } from '../lib/submissionRules'
import ActivityLog from '../components/ActivityLog.vue'
import SubmitFeedback from '../components/SubmitFeedback.vue'
import type { ActivityEntry, Attending, Event, SubmissionInput, TravelMode } from '../types'

const auth = useAuthStore()
const { confirm } = useConfirm()

const props = defineProps<{ slug: string }>()

const event = ref<Event | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const hasSubmitted = ref(false)
// Snapshot of hasSubmitted *before* the current save, so the success message can
// say "Saved" for a first submission and "Updated" for a later edit.
const savedWasUpdate = ref(false)
const activity = ref<ActivityEntry[]>([])

const TRAVEL_MODES: { value: TravelMode; label: string }[] = [
  { value: 'flight', label: 'Flight' },
  { value: 'car', label: 'Car' },
  { value: 'train', label: 'Train' },
  { value: 'other', label: 'Other' },
]

// The form mirrors SubmissionInput but starts with attendance unanswered (''),
// which the over-the-wire type doesn't allow — so the local state widens just
// that field. submit() narrows it back to a real Attending before sending.
type AttendeeFormState = Omit<SubmissionInput, 'attending'> & { attending: Attending | '' }

const form = reactive<AttendeeFormState>({
  attending: '',
  notSureReason: '',
  arrivalDay: null,
  arrivalTime: '',
  arrivalMode: null,
  arrivalDetails: '',
  departureDay: null,
  departureTime: '',
  departureMode: null,
  departureDetails: '',
  arrivalIndependent: false,
  departureIndependent: false,
  longHaul: false,
  extraStayStart: null,
  extraStayEnd: null,
  comments: '',
})

// Sentinel value for the "no travel support needed" option at the top of each
// leg's Day dropdown. Travel to and from the offsite are independent: selecting
// it on a leg sets that leg's *_independent flag and hides only that leg's
// fields. The long-haul section is hidden only when BOTH legs are independent.
const INDEPENDENT_TRAVEL = '__independent__'

// Each leg's Day <select> binds to its own computed so the independent sentinel
// and the real date value can share one control.
const arrivalDaySelection = computed<string | null>({
  get: () => (form.arrivalIndependent ? INDEPENDENT_TRAVEL : form.arrivalDay),
  set: (v) => {
    if (v === INDEPENDENT_TRAVEL) {
      form.arrivalIndependent = true
      // Clear any arrival detail the user may have entered before switching.
      form.arrivalDay = null
      form.arrivalTime = ''
      form.arrivalMode = null
      form.arrivalDetails = ''
    } else {
      form.arrivalIndependent = false
      form.arrivalDay = v
    }
  },
})

const departureDaySelection = computed<string | null>({
  get: () => (form.departureIndependent ? INDEPENDENT_TRAVEL : form.departureDay),
  set: (v) => {
    if (v === INDEPENDENT_TRAVEL) {
      form.departureIndependent = true
      form.departureDay = null
      form.departureTime = ''
      form.departureMode = null
      form.departureDetails = ''
    } else {
      form.departureIndependent = false
      form.departureDay = v
    }
  },
})

// Long-haul accommodation only applies when the People team books at least one
// leg. When the attendee self-arranges both, clear and hide that whole block
// (mirrors the server rule in submissions.go).
// Changing the attendance answer invalidates any prior save outcome: a success
// banner for a previous choice (or a stale validation error) is misleading once
// the user picks a different answer, so clear both.
watch(() => form.attending, () => {
  error.value = ''
  saved.value = false
})

const needsTravelSupport = computed(() => !(form.arrivalIndependent && form.departureIndependent))
watch(needsTravelSupport, (needs) => {
  if (!needs) {
    form.longHaul = false
    form.extraStayStart = null
    form.extraStayEnd = null
  }
})

const arrivalTimeLabel = computed(() =>
  form.arrivalMode === 'flight' ? 'Flight arrival time' : 'Time (optional)',
)
const departureTimeLabel = computed(() =>
  form.departureMode === 'flight' ? 'Flight departure time' : 'Time (optional)',
)

// Flight number is mandatory for flights and optional for every other mode, so
// the label flips between a required flight-number field and a free-text details
// field (mirrors the server rule in submissions.go / validateTravelLeg).
const arrivalDetailsLabel = computed(() =>
  form.arrivalMode === 'flight' ? 'Flight number' : 'Travel details (optional)',
)
const departureDetailsLabel = computed(() =>
  form.departureMode === 'flight' ? 'Flight number' : 'Travel details (optional)',
)

// An admin who edits a response on the attendee's behalf locks it: the attendee
// can no longer change it here. Set from the loaded submission.
const lockedByAdmin = ref(false)

const eventEnded = computed(() => event.value?.isPast ?? false)
const readOnly = computed(() => eventEnded.value || lockedByAdmin.value)

// The RSVP deadline has passed but the event itself is still open, so edits are
// allowed yet land *after deadline*. The server already stamps these, but we warn
// the attendee first (see submit) so the late change is a deliberate choice.
// Evaluated as a plain function rather than a computed because it depends on the
// current wall-clock time (Date.now), which is not reactive — a cached computed
// loaded before the deadline would never flip to true within the session.
function isAfterDeadline(): boolean {
  const e = event.value
  if (!e || readOnly.value) return false
  const t = new Date(e.submissionDeadline).getTime()
  return !isNaN(t) && Date.now() > t
}

// Google Maps search link for a hotel address, opened in a new tab.
function mapsUrl(address: string): string {
  return `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(address)}`
}

// One-line location summary ("Lisbon, Portugal" / "Lisbon" / ""), mirrors HomeView.
const placeLine = computed(() =>
  [event.value?.city, event.value?.country].filter(Boolean).join(', '),
)

// Trip length in whole calendar days, inclusive of both ends.
const tripLength = computed(() => (event.value ? tripLengthOf(event.value.startDate, event.value.endDate) : 0))

// Compact, editorial date range — "27–31 Jul 2026" when same month, else two
// full dates.
const dateRange = computed(() =>
  event.value ? formatDateRange(event.value.startDate, event.value.endDate) : '',
)

// The RSVP deadline as a full date + time in the company timezone (Europe/Paris).
const rsvpDate = computed(() => {
  const e = event.value
  if (!e) return ''
  return formatDeadline(e.submissionDeadline)
})

// The focal countdown block, mirroring HomeView's feature card — but counting
// down to the RSVP deadline (the urgency on this page), not the event start.
const deadlineBlock = computed<{ value: string; caption: string }>(() => {
  const e = event.value
  if (!e) return { value: '', caption: '' }
  if (eventEnded.value) return { value: 'Ended', caption: 'event closed' }
  const ms = new Date(e.submissionDeadline).getTime() - Date.now()
  if (isNaN(ms)) return { value: '', caption: '' }
  const days = Math.ceil(ms / 86_400_000)
  if (days > 1) return { value: String(days), caption: 'days to RSVP' }
  if (days === 1) return { value: '1', caption: 'day to RSVP' }
  if (days === 0) return { value: 'Today', caption: 'RSVP closes' }
  return { value: 'Closed', caption: 'deadline passed' }
})

// Live RSVP status chip — tracks the radio selection as the user picks, reusing
// HomeView's status--* color tokens.
type StatusKey = 'none' | Attending
const statusKey = computed<StatusKey>(() => form.attending || 'none')
const statusLabel = computed(() => {
  switch (form.attending) {
    case 'yes':
      return "You're going"
    case 'no':
      return 'Not attending'
    case 'not_sure':
      return 'Still deciding'
    default:
      return 'Awaiting your RSVP'
  }
})

// Joyful, choice-aware confirmation shown after a successful save. The headline
// celebrates a "yes" the loudest while still warmly acknowledging the other
// branches; the sub-line distinguishes a first submission from a later edit.
const successTitle = computed(() => {
  if (form.attending === 'yes') return savedWasUpdate.value ? 'Updated — see you there! 🎉' : "You're going! 🎉"
  if (form.attending === 'no') return 'Response saved — thanks for letting us know'
  return savedWasUpdate.value ? 'Response updated' : 'Response saved — thanks!'
})
const successMessage = computed(() =>
  form.attending === 'yes'
    ? 'Your travel details are with the People team. You can come back and edit any time before the deadline.'
    : 'You can come back and change your answer any time before the deadline.',
)

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

// Admins can set any day, bypassing the ±1-day window (DESIGN.md §8). For a
// read-only locked response the stored day may fall outside `travelDates`, which
// would leave the <select> blank — so fold the current value into the option list
// (kept in chronological order; ISO date strings sort that way). A no-op for a
// normal attendee, whose day is always within the window.
function withCurrentDay(base: string[], current: string | null): string[] {
  if (!current || base.includes(current)) return base
  return [...base, current].sort()
}
const arrivalDayOptions = computed(() => withCurrentDay(travelDates.value, form.arrivalDay))
const departureDayOptions = computed(() => withCurrentDay(travelDates.value, form.departureDay))

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

// Becomes true the first time the attendee tries to save. Until then we show the
// neutral "required" hints and live ✓s, but never the red "missing" markers — so
// the form doesn't shout at someone who is still filling it in.
const triedSave = ref(false)

// Which fields are mandatory depends on the chosen branch. The rule matrix lives
// in lib/submissionRules.ts (a pure, unit-tested mirror of the server's
// validateTravelLeg) so the UI marks and the backend stay in lock-step.
const fields = computed(() => fieldChecks(form))
const missingCount = computed(() => missingRequiredCount(form))

// Class bindings for a required field's <label>: `req-field` shows the neutral
// hint, `filled` flips it to a live ✓, and `missing` (only after a save attempt)
// turns it into a red "missing" marker plus a red control border.
function fieldClass(key: FieldKey) {
  const f = fields.value[key]
  return {
    'req-field': f.required,
    filled: f.required && f.filled,
    missing: f.required && !f.filled && triedSave.value,
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    event.value = await api.getEventBySlug(props.slug)
    const existing = await api.getMySubmission(props.slug)
    if (existing) {
      hasSubmitted.value = true
      lockedByAdmin.value = existing.locked
      Object.assign(form, {
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
        arrivalIndependent: existing.arrivalIndependent,
        departureIndependent: existing.departureIndependent,
        longHaul: existing.longHaul,
        extraStayStart: existing.extraStayStart,
        extraStayEnd: existing.extraStayEnd,
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
  // Custom required-field validation (the form is `novalidate`, so no native
  // bubbles). Surface which fields are missing inline rather than blocking the
  // submit silently — and don't even prompt the after-deadline confirm yet.
  triedSave.value = true
  // No attendance answer yet — the submit button is disabled in this state, but
  // guard anyway so the rest can treat `attending` as a real choice.
  const attending = form.attending
  if (!attending) return
  if (missingCount.value > 0) {
    saved.value = false
    error.value =
      missingCount.value === 1
        ? 'One required field is still missing — see the highlighted field below.'
        : `${missingCount.value} required fields are still missing — see the highlighted fields below.`
    return
  }
  // Cross-field check: arriving the day before / leaving the day after the event
  // only holds up if the matching long-haul "Extra night" box is ticked, so the
  // night has accommodation. Spell out what's still unchecked (mirrors the server).
  const stayErrors = extraNightErrors(form, beforeDate.value, afterDate.value, formatDate)
  if (stayErrors.length > 0) {
    saved.value = false
    error.value = stayErrors.join(' ')
    return
  }
  // After the deadline the event is still editable, but the change is flagged to
  // the People team — make the attendee confirm so it is never an accidental edit.
  if (isAfterDeadline()) {
    const ok = await confirm({
      variant: 'warning',
      title: 'This change is after the deadline',
      message:
        `The RSVP deadline (${formatDeadline(event.value!.submissionDeadline)}) ` +
        'has passed. Saving now will flag this change to the People team as a late ' +
        'edit, and travel may already be booked. Continue?',
      confirmLabel: 'Save late change',
      cancelLabel: 'Go back',
    })
    if (!ok) return
  }
  saving.value = true
  error.value = ''
  saved.value = false
  try {
    await api.putMySubmission(props.slug, { ...form, attending })
    savedWasUpdate.value = hasSubmitted.value
    saved.value = true
    hasSubmitted.value = true
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
    <!-- Cover header: mirrors HomeView's feature card, with the RSVP deadline
         countdown as the focal point. -->
    <header class="feature" :class="{ ended: eventEnded }">
      <img v-if="event.imageUrl" :src="event.imageUrl" alt="" class="feature-cover">
      <div class="feature-body">
        <p class="eyebrow">{{ eventEnded ? 'Event closed' : lockedByAdmin ? 'Response finalized' : 'Your RSVP' }}</p>
        <h1 class="dest">{{ event.name }}</h1>
        <p v-if="placeLine" class="place">{{ placeLine }}</p>
        <p v-if="event.hotelName" class="lodging">
          Staying at
          <a
            v-if="event.hotelLink"
            :href="event.hotelLink"
            target="_blank"
            rel="noopener noreferrer"
            class="hotel-link"
          >{{ event.hotelName }}</a><template v-else>{{ event.hotelName }}</template><span v-if="event.hotelAddress"> · {{ event.hotelAddress }}
            <a
              :href="mapsUrl(event.hotelAddress)"
              target="_blank"
              rel="noopener noreferrer"
              class="maps-link"
              title="Open in Google Maps"
              aria-label="Open in Google Maps"
            >
              <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
                <path
                  fill="currentColor"
                  d="M12 2a7 7 0 0 0-7 7c0 5.25 7 13 7 13s7-7.75 7-13a7 7 0 0 0-7-7Zm0 9.5A2.5 2.5 0 1 1 12 6.5a2.5 2.5 0 0 1 0 5Z"
                />
              </svg>
            </a></span>
        </p>

        <dl class="stats">
          <div class="stat">
            <dt>Dates</dt>
            <dd>{{ dateRange }}</dd>
          </div>
          <div class="stat">
            <dt>Trip length</dt>
            <dd>{{ tripLength }} {{ tripLength === 1 ? 'day' : 'days' }}</dd>
          </div>
          <div class="stat">
            <dt>RSVP by</dt>
            <dd>{{ rsvpDate }}</dd>
          </div>
        </dl>

        <div class="feature-foot">
          <span class="status" :class="`status--${statusKey}`">{{ statusLabel }}</span>
          <span class="deadline-note">Closes {{ formatDeadline(event.submissionDeadline) }}</span>
        </div>
      </div>

      <aside class="countdown" aria-hidden="true">
        <span class="count-num">{{ deadlineBlock.value }}</span>
        <span class="count-caption">{{ deadlineBlock.caption }}</span>
      </aside>
    </header>

    <p v-if="eventEnded" class="locked">
      This event has ended — your response can no longer be edited. Contact the
      People team if something needs changing.
    </p>
    <p v-else-if="lockedByAdmin" class="locked">
      The People team has finalized your response, so it can no longer be edited
      here. Contact them if something needs changing.
    </p>

    <form class="form" novalidate @submit.prevent="submit">
      <fieldset :disabled="readOnly || saving">
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
          <p class="notice">
            Use this option only if you cannot make a yes/no call before the deadline
            ends. Let us know what you currently think, what the decision depends on,
            and when you might be able to tell us the final decision.
          </p>
          <label :class="fieldClass('notSureReason')">
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
          <h3 class="section-head">Travel</h3>
          <div class="leg">
            <h3>Arrival</h3>
            <div class="row">
              <label :class="fieldClass('arrivalDay')">Day
                <select v-model="arrivalDaySelection" required>
                  <option :value="null" disabled>Select a day</option>
                  <option :value="INDEPENDENT_TRAVEL">
                    I arrange my own travel here, no support needed
                  </option>
                  <option v-for="d in arrivalDayOptions" :key="d" :value="d">{{ formatDate(d) }}</option>
                </select>
              </label>
              <label v-if="!form.arrivalIndependent" :class="fieldClass('arrivalTime')">{{ arrivalTimeLabel }}
                <input
                  v-model="form.arrivalTime"
                  type="time"
                  class="time"
                  :class="{ empty: !form.arrivalTime }"
                  :required="form.arrivalMode === 'flight'"
                >
              </label>
            </div>
            <div v-if="!form.arrivalIndependent" class="row">
              <label :class="fieldClass('arrivalMode')">Travel mode
                <select v-model="form.arrivalMode" required>
                  <option :value="null" disabled>Select</option>
                  <option v-for="m in TRAVEL_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
                </select>
              </label>
              <label :class="fieldClass('arrivalDetails')">{{ arrivalDetailsLabel }}
                <input
                  v-model="form.arrivalDetails"
                  type="text"
                  placeholder="BA123, or other info"
                  :required="form.arrivalMode === 'flight'"
                >
              </label>
            </div>
          </div>

          <div class="leg">
            <h3>Departure</h3>
            <div class="row">
              <label :class="fieldClass('departureDay')">Day
                <select v-model="departureDaySelection" required>
                  <option :value="null" disabled>Select a day</option>
                  <option :value="INDEPENDENT_TRAVEL">
                    I arrange my own travel here, no support needed
                  </option>
                  <option v-for="d in departureDayOptions" :key="d" :value="d">{{ formatDate(d) }}</option>
                </select>
              </label>
              <label v-if="!form.departureIndependent" :class="fieldClass('departureTime')">{{ departureTimeLabel }}
                <input
                  v-model="form.departureTime"
                  type="time"
                  class="time"
                  :class="{ empty: !form.departureTime }"
                  :required="form.departureMode === 'flight'"
                >
              </label>
            </div>
            <div v-if="!form.departureIndependent" class="row">
              <label :class="fieldClass('departureMode')">Travel mode
                <select v-model="form.departureMode" required>
                  <option :value="null" disabled>Select</option>
                  <option v-for="m in TRAVEL_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
                </select>
              </label>
              <label :class="fieldClass('departureDetails')">{{ departureDetailsLabel }}
                <input
                  v-model="form.departureDetails"
                  type="text"
                  placeholder="BA456, or other info"
                  :required="form.departureMode === 'flight'"
                >
              </label>
            </div>
          </div>

          <template v-if="needsTravelSupport">
            <div class="field">
              <h3 class="sub">Company-Paid Accommodation for Long-Haul Travellers</h3>
              <p class="field-note">
                See the
                <a
                  href="https://app.notion.com/p/id5technology/Traveling-to-the-IRL-388334ab4b6a8027a829f184455c1eeb"
                  target="_blank"
                  rel="noopener noreferrer"
                >Traveling to the IRL policy</a>.
              </p>
              <label class="check">
                <input v-model="form.longHaul" type="checkbox">
                I'm a long-haul traveller (international flight of 7+ hours)
              </label>
            </div>

            <div v-if="form.longHaul" class="field extra">
              <span class="q">Would you require an extra night?</span>
              <label class="check">
                <input v-model="extraNightBefore" type="checkbox">
                Extra night before — {{ formatDate(form.extraStayStart || beforeDate) }}
              </label>
              <label class="check">
                <input v-model="extraNightAfter" type="checkbox">
                Extra night after — {{ formatDate(form.extraStayEnd || afterDate) }}
              </label>
            </div>
          </template>

          <h3 class="section-head">Other</h3>
          <label>Comments
            <textarea v-model="form.comments" rows="2" />
          </label>
        </template>

        <SubmitFeedback
          :error="error"
          :success="saved"
          error-title="Couldn't save your response"
          :success-title="successTitle"
          :success-message="successMessage"
        />

        <div v-if="!readOnly" class="actions">
          <button class="btn" type="submit" :disabled="saving || !form.attending">
            {{ saving ? 'Saving…' : hasSubmitted ? 'Update' : 'Submit' }}
          </button>
        </div>

        <div class="submitting-as">
          <p>Submitting as <strong>{{ auth.user?.name || auth.user?.email }}</strong>.</p>
          <p>
            Allergies / dietary preferences:
            <template v-if="auth.user?.allergies">{{ auth.user.allergies }}</template>
            <span v-else class="none">none set</span>
          </p>
          <RouterLink :to="{ path: '/profile', query: { redirect: `/events/${slug}` } }">Edit your profile</RouterLink>
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
  max-width: 720px;
}

/* Cover header / feature card — shared visual language with HomeView ── */
.feature {
  display: grid;
  grid-template-columns: 1fr minmax(170px, 0.4fr);
  margin-bottom: 28px;
  border: 1px solid var(--border);
  border-top: 3px solid var(--accent);
  background:
    linear-gradient(180deg, rgb(var(--accent-rgb) / 0.05), transparent 38%),
    var(--panel);
}
.feature.ended {
  border-top-color: var(--muted);
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
  padding: 28px 32px 26px;
  min-width: 0;
}
.eyebrow {
  margin: 0 0 12px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.24em;
  text-transform: uppercase;
  color: var(--accent-2);
}
.feature.ended .eyebrow {
  color: var(--muted);
}
.dest {
  margin: 0;
  font-style: normal;
  font-weight: 420;
  font-size: clamp(28px, 4vw, 44px);
  line-height: 1.04;
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

.maps-link {
  display: inline-flex;
  align-items: center;
  vertical-align: middle;
  margin-left: 2px;
  color: inherit;
}

.maps-link:hover {
  color: var(--text);
}

.stats {
  display: flex;
  flex-wrap: wrap;
  margin: 24px 0 0;
  border-top: 1px solid var(--border-soft);
}
.stat {
  flex: 1;
  min-width: 110px;
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
  gap: 10px 18px;
  margin-top: 24px;
}
.status {
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
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
.deadline-note {
  font-family: var(--mono);
  font-size: 11px;
  letter-spacing: 0.04em;
  color: var(--muted);
}

/* Countdown — the focal point */
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
.feature.ended .countdown {
  background: rgb(var(--text-rgb) / 0.04);
}
.count-num {
  font-family: var(--serif);
  font-style: italic;
  font-weight: 360;
  font-size: clamp(48px, 8vw, 88px);
  line-height: 0.9;
  letter-spacing: -0.03em;
  color: var(--accent-2);
}
.feature.ended .count-num {
  color: var(--muted);
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

.locked {
  background: rgb(var(--accent-rgb) / 0.08);
  border-left: 2px solid var(--accent);
  padding: 12px 16px;
  margin: 0 0 24px;
  font-family: var(--mono);
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--text-soft);
}

/* Form ─────────────────────────────────────────────────────── */
.form fieldset {
  border: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 1.1rem;
}
.section-head {
  margin: 14px 0 4px;
  padding-top: 18px;
  border-top: 1px solid var(--border);
}
.section-head:first-of-type {
  /* the first section sits right under the leg cards; no double rule needed */
}
label {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.04em;
  color: var(--muted);
}
label.check {
  flex-direction: row;
  align-items: center;
  gap: 0.55rem;
  font-size: 13px;
  letter-spacing: 0;
  color: var(--text);
}
input[type='checkbox'],
input[type='radio'] {
  accent-color: var(--accent);
  width: 15px;
  height: 15px;
}

/* Safari/WebKit render an empty <input type="time"> with the field-format
   text (e.g. "12:30 PM") at full value color, so an empty field looks
   pre-filled. Time inputs have no ::placeholder, so we fade the format text
   ourselves whenever the field is empty — kept barely visible so it clearly
   reads as a placeholder, never as a real entered value. */
input.time.empty::-webkit-datetime-edit {
  color: rgb(var(--text-rgb) / 0.25);
}

/* Required-field markers. A required <label> carries `req-field`; the corner
   tag progresses neutral "required" → green "✓" once filled → red "missing"
   after a save attempt (the `missing` class is only added post-submit). */
.req-field {
  position: relative;
}
.req-field::after {
  position: absolute;
  top: 0;
  right: 0;
  content: 'required';
  font-family: var(--mono);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  opacity: 0.65;
  pointer-events: none;
}
.req-field.filled::after {
  content: '✓';
  font-size: 12px;
  color: var(--success);
  opacity: 1;
}
.req-field.missing::after {
  content: 'missing';
  color: var(--danger);
  opacity: 1;
}
.req-field.missing select,
.req-field.missing input {
  border-bottom-color: var(--danger);
}
.req-field.missing textarea {
  border-color: var(--danger);
}

.submitting-as {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  border-left: 2px solid var(--border);
  padding: 2px 0 2px 16px;
  margin: 0.75rem 0 0;
  font-family: var(--mono);
  font-size: 12.5px;
  color: var(--text-soft);
}
.submitting-as p {
  margin: 0;
}
.submitting-as strong {
  color: var(--text);
}
.submitting-as .none {
  font-style: italic;
  color: var(--muted);
}

.row {
  display: flex;
  gap: 1.25rem;
}
.row > label {
  flex: 1;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.field .q {
  display: block;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--text-soft);
}
.field-note {
  font-size: 12.5px;
  color: var(--muted);
  margin: 0;
}

/* Attendance radios as segmented editorial chips */
.radios {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.radios label {
  flex-direction: row;
  align-items: center;
  gap: 0.5rem;
  padding: 9px 16px;
  border: 1px solid var(--border);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--text-soft);
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}
.radios label:hover {
  border-color: var(--accent);
}
.radios label:has(input:checked) {
  border-color: var(--accent);
  background: rgb(var(--accent-rgb) / 0.08);
  color: var(--text);
}

.leg {
  border: 1px solid var(--border);
  padding: 18px 20px;
  background: var(--panel);
}
.leg h3 {
  margin-top: 0;
}
/* Long, sentence-style subheads read better as serif than all-caps mono. */
.sub {
  margin: 0 0 2px;
  font-family: var(--serif);
  font-style: italic;
  font-weight: 400;
  font-size: 17px;
  letter-spacing: -0.01em;
  text-transform: none;
  color: var(--text);
}

.extra,
.notice {
  background: rgb(var(--accent-rgb) / 0.07);
  border-left: 2px solid var(--accent);
  padding: 14px 18px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-soft);
}
.notice p {
  margin: 0;
}
.notice ol {
  margin: 0.5rem 0 0;
  padding-left: 1.2rem;
}
.notice ol li {
  margin: 0.15rem 0;
}

.actions {
  margin-top: 0.5rem;
}

.my-activity {
  margin-top: 2.5rem;
  border-top: 1px solid var(--border);
  padding-top: 1.25rem;
}

@media (max-width: 640px) {
  .feature {
    grid-template-columns: 1fr;
  }
  .feature-body {
    padding: 22px 20px 20px;
  }
  .countdown {
    flex-direction: row;
    gap: 14px;
    padding: 16px 20px;
    border-left: 0;
    border-top: 1px solid var(--border);
  }
  .count-num {
    font-size: 46px;
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
  .row {
    flex-direction: column;
    gap: 0.9rem;
  }
}
</style>
