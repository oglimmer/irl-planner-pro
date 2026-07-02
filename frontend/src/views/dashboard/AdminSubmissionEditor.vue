<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, errMsg } from '../../api'
import { CURRENCIES } from '../../lib/currencies'
import type { Attending, Submission, SubmissionInput, TravelMode } from '../../types'

const props = defineProps<{
  eventId: string
  userId: string
  name: string
  email: string
  // The attendee's current submission, or null if they haven't responded yet.
  submission: Submission | null
}>()

const emit = defineEmits<{
  close: []
  // The admin saved (locked or not). Parent should refetch.
  saved: []
}>()

const TRAVEL_MODES: { value: TravelMode; label: string }[] = [
  { value: 'flight', label: 'Flight' },
  { value: 'car', label: 'Car' },
  { value: 'train', label: 'Train' },
  { value: 'other', label: 'Other' },
]

// Local form mirrors SubmissionInput but lets attending start blank for a
// not-yet-responded attendee; save() narrows it back to a real Attending.
type EditorState = Omit<SubmissionInput, 'attending'> & { attending: Attending | '' }

const s = props.submission
const form = reactive<EditorState>({
  attending: s?.attending ?? '',
  notSureReason: s?.notSureReason ?? '',
  arrivalDay: s?.arrivalDay ?? null,
  arrivalTime: s?.arrivalTime ?? '',
  arrivalMode: s?.arrivalMode ?? null,
  arrivalDetails: s?.arrivalDetails ?? '',
  departureDay: s?.departureDay ?? null,
  departureTime: s?.departureTime ?? '',
  departureMode: s?.departureMode ?? null,
  departureDetails: s?.departureDetails ?? '',
  arrivalIndependent: s?.arrivalIndependent ?? false,
  departureIndependent: s?.departureIndependent ?? false,
  longHaul: s?.longHaul ?? false,
  extraStayStart: s?.extraStayStart ?? null,
  extraStayEnd: s?.extraStayEnd ?? null,
  extraStaySelfFunded: s?.extraStaySelfFunded ?? false,
  comments: s?.comments ?? '',
  travelCost: s?.travelCost ?? null,
  travelCostCurrency: s?.travelCostCurrency || 'EUR',
})

// A native <input type="number"> yields '' when cleared; map that (and any
// non-numeric value) to null so the backend stores NULL rather than a bad amount.
function setTravelCost(ev: globalThis.Event) {
  const v = (ev.target as HTMLInputElement).value
  const n = Number(v)
  form.travelCost = v === '' || Number.isNaN(n) ? null : n
}

const saving = ref(false)
// Which action is in flight, so the right button shows "Saving…": true = the
// "Save & lock" button, false = the plain "Save" button.
const pendingLock = ref(false)
const error = ref('')

// Native <input type="date"> / <select> yield '' when cleared; map that to null
// so the backend stores NULL rather than an empty DATE / mode.
function setDay(key: 'arrivalDay' | 'departureDay' | 'extraStayStart', ev: globalThis.Event) {
  form[key] = (ev.target as HTMLInputElement).value || null
}
function setMode(key: 'arrivalMode' | 'departureMode', ev: globalThis.Event) {
  form[key] = ((ev.target as HTMLSelectElement).value || null) as TravelMode | null
}

// lock=true saves and locks (attendee can no longer self-edit); lock=false is a
// plain save that leaves the response attendee-editable.
async function save(lock: boolean) {
  if (!form.attending) {
    error.value = 'Pick an attendance answer first.'
    return
  }
  saving.value = true
  pendingLock.value = lock
  error.value = ''
  try {
    await api.adminUpdateSubmission(props.eventId, props.userId, {
      ...form,
      attending: form.attending,
    }, lock)
    emit('saved')
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    saving.value = false
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div class="overlay" @click.self="emit('close')">
      <div class="box" role="dialog" aria-modal="true" aria-labelledby="admin-edit-title">
        <header class="head">
          <div>
            <h2 id="admin-edit-title">Edit response</h2>
            <p class="who">{{ name || email }} <span class="muted">· {{ email }}</span></p>
          </div>
          <button type="button" class="x" aria-label="Close" @click="emit('close')">×</button>
        </header>

        <p class="warn">
          Admin edits accept any value with no validation — any day, any option.
          <strong>Save &amp; lock</strong> also makes the response read-only for the
          attendee; <strong>Save</strong> leaves it editable by them. A lock is
          permanent — a later plain Save won't undo it.
        </p>

        <form class="form" novalidate @submit.prevent="save(true)">
          <div class="field">
            <span class="q">Attending?</span>
            <div class="radios">
              <label><input v-model="form.attending" type="radio" value="yes"> Yes</label>
              <label><input v-model="form.attending" type="radio" value="no"> No</label>
              <label><input v-model="form.attending" type="radio" value="not_sure"> Not sure</label>
            </div>
          </div>

          <label v-if="form.attending === 'not_sure'" class="field">
            Not-sure reason
            <textarea v-model="form.notSureReason" rows="2" />
          </label>

          <template v-if="form.attending === 'yes'">
            <fieldset class="leg">
              <legend>Arrival</legend>
              <label class="check">
                <input v-model="form.arrivalIndependent" type="checkbox"> Self-arranged (no support)
              </label>
              <template v-if="!form.arrivalIndependent">
                <div class="grid2">
                  <label>Day<input :value="form.arrivalDay ?? ''" type="date" @input="setDay('arrivalDay', $event)"></label>
                  <label>Time<input v-model="form.arrivalTime" type="text" placeholder="14:30"></label>
                  <label>Mode
                    <select :value="form.arrivalMode ?? ''" @change="setMode('arrivalMode', $event)">
                      <option value="">—</option>
                      <option v-for="m in TRAVEL_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
                    </select>
                  </label>
                  <label>Details<input v-model="form.arrivalDetails" type="text" placeholder="Flight no. / notes"></label>
                </div>
              </template>
            </fieldset>

            <fieldset class="leg">
              <legend>Departure</legend>
              <label class="check">
                <input v-model="form.departureIndependent" type="checkbox"> Self-arranged (no support)
              </label>
              <template v-if="!form.departureIndependent">
                <div class="grid2">
                  <label>Day<input :value="form.departureDay ?? ''" type="date" @input="setDay('departureDay', $event)"></label>
                  <label>Time<input v-model="form.departureTime" type="text" placeholder="10:00"></label>
                  <label>Mode
                    <select :value="form.departureMode ?? ''" @change="setMode('departureMode', $event)">
                      <option value="">—</option>
                      <option v-for="m in TRAVEL_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
                    </select>
                  </label>
                  <label>Details<input v-model="form.departureDetails" type="text" placeholder="Flight no. / notes"></label>
                </div>
              </template>
            </fieldset>

            <fieldset class="leg">
              <legend>Accommodation</legend>
              <label class="check">
                <input v-model="form.longHaul" type="checkbox"> Long-haul traveller
              </label>
              <label class="check">
                <input v-model="form.extraStaySelfFunded" type="checkbox"> Self-funded early arrival (own accommodation, wants company transport)
              </label>
              <div class="grid2">
                <label>Extra night before<input :value="form.extraStayStart ?? ''" type="date" @input="setDay('extraStayStart', $event)"></label>
              </div>
            </fieldset>

            <fieldset class="leg">
              <legend>Travel cost</legend>
              <div class="grid2">
                <label>Total cost<input :value="form.travelCost ?? ''" type="number" min="0" step="0.01" inputmode="decimal" placeholder="0.00" @input="setTravelCost"></label>
                <label>Currency
                  <select v-model="form.travelCostCurrency">
                    <option v-for="c in CURRENCIES" :key="c" :value="c">{{ c }}</option>
                  </select>
                </label>
              </div>
            </fieldset>

            <label class="field">
              Comments
              <textarea v-model="form.comments" rows="2" />
            </label>
          </template>

          <p v-if="error" class="error">{{ error }}</p>

          <div class="actions">
            <button type="button" class="btn secondary" @click="emit('close')">Cancel</button>
            <button
              type="button"
              class="btn secondary"
              :disabled="saving || !form.attending"
              @click="save(false)"
            >
              {{ saving && !pendingLock ? 'Saving…' : 'Save' }}
            </button>
            <button type="submit" class="btn" :disabled="saving || !form.attending">
              {{ saving && pendingLock ? 'Saving…' : 'Save & lock' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 2rem 1.5rem;
  overflow-y: auto;
  background: rgba(20, 24, 35, 0.45);
}
.box {
  width: 100%;
  max-width: 34rem;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.25rem 1.4rem 1.2rem;
  box-shadow: 0 10px 30px rgba(20, 24, 35, 0.2);
}
.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}
.head h2 { margin: 0 0 0.2rem; font-size: 1.1rem; }
.who { margin: 0; font-size: 0.9rem; }
.muted { color: var(--muted); }
.x {
  border: none;
  background: none;
  font-size: 1.5rem;
  line-height: 1;
  cursor: pointer;
  color: var(--muted);
  padding: 0;
}
.warn {
  margin: 0.85rem 0 1rem;
  padding: 0.6rem 0.75rem;
  font-size: 0.85rem;
  background: rgb(var(--accent-rgb) / 0.12);
  border-radius: var(--radius);
  color: var(--text);
}
.form { display: flex; flex-direction: column; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; }
.q { font-weight: 600; }
.radios { display: flex; gap: 1.25rem; }
.radios label { display: flex; align-items: center; gap: 0.35rem; }
.leg {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 0.75rem 0.9rem 0.9rem;
}
.leg legend { font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--muted); padding: 0 0.3rem; }
.check { display: flex; align-items: center; gap: 0.4rem; margin-bottom: 0.6rem; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
.grid2 label, .field label { display: flex; flex-direction: column; gap: 0.3rem; font-size: 0.85rem; }
input[type='date'], input[type='text'], select, textarea {
  padding: 0.4rem 0.5rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font: inherit;
  width: 100%;
  box-sizing: border-box;
}
.error { color: var(--danger); margin: 0; }
.actions { display: flex; justify-content: flex-end; gap: 0.6rem; }
@media (max-width: 480px) {
  .grid2 { grid-template-columns: 1fr; }
}
</style>
