<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, errMsg } from '../api'
import { useConfirm } from '../composables/useConfirm'
import type { MessagingStatus } from '../types'

// The Messaging tab: edit the per-event invite/reminder copy, send the
// invitation to every attendee not yet invited, and fire a manual follow-up to
// current non-responders. Email (SMTP) and Slack (bot DMs) are both live
// channels; each is selectable once configured on the server.
const props = defineProps<{ eventId: string }>()

const { confirm } = useConfirm()

const status = ref<MessagingStatus | null>(null)
const error = ref('')
const notice = ref('')

// Editable template fields (bound to the textareas). Seeded from the stored
// overrides; empty means "use the default", so the default is shown as the
// placeholder rather than pre-filled.
const inviteSubject = ref('')
const inviteBody = ref('')
const reminderSubject = ref('')
const reminderBody = ref('')

// Placeholder tokens shown as a hint. Held in script so the literal "{{…}}"
// never appears inside a template interpolation (which the compiler would try
// to parse as a binding).
const placeholderTokens = ['{{name}}', '{{event}}', '{{city}}', '{{link}}', '{{deadline}}']

const channel = ref('email')
const savingTemplates = ref(false)
const inviting = ref(false)
const followingUp = ref(false)

const channelOptions = computed(() => status.value?.channels ?? [])
const selectedChannel = computed(() => channelOptions.value.find((c) => c.name === channel.value))
const selectedAvailable = computed(() => selectedChannel.value?.available ?? false)
const selectedConfigured = computed(() => selectedChannel.value?.configured ?? false)
const stats = computed(() => status.value?.stats)
const notYetInvited = computed(() => {
  const s = status.value?.stats
  return s ? Math.max(0, s.attendees - s.invited) : 0
})
const defaults = computed(() => status.value?.defaults)
const failures = computed(() => status.value?.failures ?? [])

function formatWhen(iso: string): string {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

// Seed the editors with the stored override, or the generated default when none
// is set — so the fields show real, editable copy rather than an empty box.
function seed(s: MessagingStatus) {
  status.value = s
  inviteSubject.value = s.templates.inviteSubject || s.defaults.inviteSubject
  inviteBody.value = s.templates.inviteBody || s.defaults.inviteBody
  reminderSubject.value = s.templates.reminderSubject || s.defaults.reminderSubject
  reminderBody.value = s.templates.reminderBody || s.defaults.reminderBody
}

async function load() {
  try {
    seed(await api.getMessaging(props.eventId))
  } catch (e) {
    error.value = errMsg(e)
  }
}

// refresh updates the stats/channels only — it does NOT re-seed the editor refs,
// so a poll after a background send won't wipe in-progress (unsaved) edits.
async function refresh() {
  try {
    status.value = await api.getMessaging(props.eventId)
  } catch {
    // best-effort progress refresh — ignore
  }
}

// Background sends settle asynchronously, so we poll a couple of times after one
// fires. Track the timers and cancel any still pending on unmount, so a refresh
// can't run against (and write `status` on) a torn-down component.
const pollTimers = new Set<ReturnType<typeof setTimeout>>()
function scheduleRefresh(ms: number) {
  const id = setTimeout(() => {
    pollTimers.delete(id)
    void refresh()
  }, ms)
  pollTimers.add(id)
}
onUnmounted(() => {
  for (const id of pollTimers) clearTimeout(id)
  pollTimers.clear()
})

async function saveTemplates() {
  savingTemplates.value = true
  error.value = ''
  notice.value = ''
  try {
    await api.saveMessaging(props.eventId, {
      inviteSubject: inviteSubject.value,
      inviteBody: inviteBody.value,
      reminderSubject: reminderSubject.value,
      reminderBody: reminderBody.value,
    })
    notice.value = 'Templates saved.'
    await load()
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    savingTemplates.value = false
  }
}

async function sendInvitation() {
  const n = notYetInvited.value
  const ok = await confirm({
    title: 'Send invitation?',
    message:
      n > 0
        ? `Send the invitation to ${n} attendee(s) not yet invited, via ${channel.value}. Already-invited attendees are skipped.`
        : `Everyone has already been invited. Re-pressing won't email anyone unless new attendees were added.`,
    confirmLabel: 'Send invitation',
  })
  if (!ok) return
  inviting.value = true
  error.value = ''
  notice.value = ''
  try {
    const res = await api.sendInvitation(props.eventId, channel.value)
    notice.value = `Inviting ${res.queued} attendee(s) in the background — already-invited people are skipped. This can take a few minutes; counts update as it sends.`
    // Poll progress a couple of times without disturbing the editors.
    scheduleRefresh(3000)
    scheduleRefresh(15000)
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    inviting.value = false
  }
}

async function sendFollowup() {
  const n = stats.value?.nonResponders ?? 0
  const ok = await confirm({
    title: 'Send follow-up now?',
    message: `Send the reminder to ${n} non-responder(s) now, via ${channel.value}. Scheduled reminders still send on their own. A repeat within the same day is skipped.`,
    confirmLabel: 'Send follow-up',
  })
  if (!ok) return
  followingUp.value = true
  error.value = ''
  notice.value = ''
  try {
    const res = await api.sendFollowup(props.eventId, channel.value)
    notice.value = `Sending the follow-up to ${res.queued} non-responder(s) in the background. This can take a few minutes.`
    scheduleRefresh(3000)
    scheduleRefresh(15000)
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    followingUp.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="messaging">
    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="notice" class="notice">{{ notice }}</p>

    <template v-if="status">
      <!-- Audience + channel -->
      <div class="card">
        <div class="stats">
          <div><strong>{{ stats?.attendees ?? 0 }}</strong><span>attendees</span></div>
          <div><strong>{{ stats?.invited ?? 0 }}</strong><span>invited</span></div>
          <div><strong>{{ stats?.nonResponders ?? 0 }}</strong><span>not responded</span></div>
        </div>
        <label class="channel">
          Channel
          <select v-model="channel">
            <option
              v-for="c in channelOptions"
              :key="c.name"
              :value="c.name"
              :disabled="!c.available"
            >
              {{ c.name }}{{ c.available ? '' : ' (coming soon)' }}
            </option>
          </select>
        </label>
        <p v-if="!selectedAvailable" class="muted hint">
          The “{{ channel }}” channel isn’t available yet — coming soon. Pick email to send.
        </p>
        <p v-else-if="!selectedConfigured" class="muted hint">
          The “{{ channel }}” channel isn’t configured on the server yet
          (set <code>{{ channel === 'slack' ? 'SLACK_BOT_TOKEN' : 'SMTP_HOST' }}</code>). You can still
          edit and save templates; sending will work once it’s configured.
        </p>
      </div>

      <!-- Invitation -->
      <div class="card">
        <h3>Invitation</h3>
        <p class="muted">
          Sent when you press the button below — to every attendee not yet invited.
          Re-pressing only emails people added since the last send.
        </p>
        <label>
          Subject
          <input v-model="inviteSubject" type="text" :placeholder="defaults?.inviteSubject">
        </label>
        <label>
          Body
          <textarea v-model="inviteBody" rows="7" :placeholder="defaults?.inviteBody" />
        </label>
        <p class="muted placeholders">
          Placeholders: <code v-for="p in placeholderTokens" :key="p">{{ p }}</code>.
          Pre-filled with the default copy — edit freely, or clear a field to fall back to the default.
        </p>
        <div class="actions">
          <button class="btn secondary" :disabled="savingTemplates" @click="saveTemplates">
            {{ savingTemplates ? 'Saving…' : 'Save templates' }}
          </button>
          <button class="btn" :disabled="inviting || !selectedConfigured" @click="sendInvitation">
            {{ inviting ? 'Sending…' : `Send invitation (${notYetInvited} pending)` }}
          </button>
        </div>
      </div>

      <!-- Follow-up -->
      <div class="card">
        <h3>Follow-up reminders</h3>
        <p class="muted">
          Non-responders are reminded automatically on this event’s schedule
          (weekly and in the run-up to the deadline). Use the button to send an
          extra reminder right now. This copy is also what the scheduled reminders use.
        </p>
        <label>
          Subject
          <input v-model="reminderSubject" type="text" :placeholder="defaults?.reminderSubject">
        </label>
        <label>
          Body
          <textarea v-model="reminderBody" rows="7" :placeholder="defaults?.reminderBody" />
        </label>
        <div class="actions">
          <button class="btn secondary" :disabled="savingTemplates" @click="saveTemplates">
            {{ savingTemplates ? 'Saving…' : 'Save templates' }}
          </button>
          <button
            class="btn"
            :disabled="followingUp || !selectedConfigured || (stats?.nonResponders ?? 0) === 0"
            @click="sendFollowup"
          >
            {{ followingUp ? 'Sending…' : `Send follow-up now (${stats?.nonResponders ?? 0})` }}
          </button>
        </div>
      </div>

      <!-- Delivery failures -->
      <div v-if="failures.length" class="card failures">
        <h3>Delivery failures</h3>
        <p class="muted">
          {{ failures.length }} recent send(s) the server rejected (see the channel + error per row) —
          these were released for retry, so fixing the cause and resending will pick them up. A
          successful send means the channel accepted the message, not that it was delivered
          (email bounces and Slack delivery aren’t tracked).
        </p>
        <table>
          <thead>
            <tr><th>Recipient</th><th>Type</th><th>Channel</th><th>When</th><th>Error</th></tr>
          </thead>
          <tbody>
            <tr v-for="f in failures" :key="`${f.recipient}-${f.kind}-${f.createdAt}`">
              <td>{{ f.recipient }}</td>
              <td>{{ f.kind }}</td>
              <td>{{ f.channel }}</td>
              <td>{{ formatWhen(f.createdAt) }}</td>
              <td class="err">{{ f.error }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
    <p v-else class="muted">Loading…</p>
  </div>
</template>

<style scoped>
.messaging {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}
.card {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.25rem;
  background: var(--surface);
}
.card h3 {
  margin: 0 0 0.25rem;
}
.muted { color: var(--muted); }
.error { color: var(--danger); }
.notice {
  color: var(--ok);
  background: rgb(var(--success-rgb) / 0.1);
  padding: 0.5rem 0.75rem;
  border-radius: var(--radius);
}
.stats {
  display: flex;
  gap: 2rem;
  margin-bottom: 1rem;
}
.stats div {
  display: flex;
  flex-direction: column;
}
.stats strong {
  font-size: 1.5rem;
}
.stats span {
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--muted);
}
.channel {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.85rem;
  color: var(--muted);
}
.channel select {
  padding: 0.3rem 0.5rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  text-transform: capitalize;
}
.hint { margin: 0.5rem 0 0; }
label {
  display: block;
  margin: 0.75rem 0;
  font-size: 0.85rem;
  color: var(--muted);
}
label input,
label textarea {
  display: block;
  width: 100%;
  margin-top: 0.3rem;
  padding: 0.5rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font: inherit;
  color: var(--text);
  background: var(--bg);
}
label textarea {
  resize: vertical;
  font-family: var(--mono, monospace);
}
.placeholders code {
  background: var(--bg);
  padding: 0.05rem 0.3rem;
  border-radius: 4px;
  margin-right: 0.35rem;
}
.actions {
  display: flex;
  gap: 0.75rem;
  flex-wrap: wrap;
  margin-top: 0.5rem;
}
.failures table {
  width: 100%;
  border-collapse: collapse;
  margin-top: 0.5rem;
}
.failures th,
.failures td {
  text-align: left;
  padding: 0.4rem 0.5rem;
  border-bottom: 1px solid var(--border);
  font-size: 0.85rem;
  vertical-align: top;
}
.failures th {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--muted);
}
.failures .err {
  color: var(--danger);
  font-family: var(--mono, monospace);
  font-size: 0.78rem;
  word-break: break-word;
}
</style>
