<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { errMsg } from '../api'

const router = useRouter()
const auth = useAuthStore()
const ready = ref(false)
const error = ref('')

// Dev (password-mode) form state.
const email = ref('')
const firstName = ref('')
const lastName = ref('')
const allergies = ref('')
const submitting = ref(false)

// Sign-in email placeholder tracks the configured domain (falls back generic).
const emailPlaceholder = computed(() =>
  auth.signInDomain ? `you@${auth.signInDomain}` : 'you@company.com',
)

// Editorial issue-line date, e.g. "SUNDAY · 28 JUNE 2026" — mirrors HomeView.
const todayLine = computed(() =>
  new Intl.DateTimeFormat('en-GB', {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
    .format(new Date())
    .toUpperCase(),
)

onMounted(async () => {
  try {
    await auth.ensureMode()
  } catch {
    // leave mode null; default UI shows the Google button
  } finally {
    ready.value = true
  }
})

function signInGoogle() {
  auth.loginViaOIDC()
}

async function signInDev() {
  submitting.value = true
  error.value = ''
  try {
    await auth.devLogin(email.value, firstName.value, lastName.value, allergies.value)
    router.replace('/')
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="auth">
    <article class="spread">
      <!-- Cover story: editorial masthead, the publication's "front page". -->
      <section class="cover" aria-hidden="true">
        <header class="cover-head reveal">
          <span class="wordmark">ID5</span>
          <span class="rule" />
          <span class="kicker">People&nbsp;Team</span>
        </header>

        <div class="cover-lede">
          <p class="eyebrow reveal">The offsite desk</p>
          <h1 class="headline reveal">
            Where the<br>team is<br>headed <em>next</em>.
          </h1>
          <p class="standfirst reveal">
            Itineraries, travel and rooming for every ID5 gathering — confirmed
            in one place, ahead of the road.
          </p>
        </div>

        <!-- Boarding-pass meta strip along the foot of the cover. -->
        <footer class="boarding reveal">
          <div class="board-cell">
            <span class="board-key">Issue</span>
            <span class="board-val">{{ todayLine }}</span>
          </div>
          <div class="board-cell">
            <span class="board-key">Gate</span>
            <span class="board-val">IRL</span>
          </div>
          <div class="board-cell">
            <span class="board-key">Status</span>
            <span class="board-val board-val--live">Boarding</span>
          </div>
        </footer>

        <!-- Decorative passport stamp. -->
        <span class="stamp">
          <span class="stamp-top">★ ID5 ★</span>
          <span class="stamp-mid">IRL</span>
          <span class="stamp-bot">EST · 2026</span>
        </span>
      </section>

      <!-- The actual sign-in panel. -->
      <section class="panel">
        <p class="panel-eyebrow reveal">Members access</p>
        <h2 class="panel-title reveal">Sign in.</h2>

        <template v-if="ready && auth.mode === 'password'">
          <p class="panel-lede reveal">Local development sign-in.</p>
          <form class="reveal" @submit.prevent="signInDev">
            <label class="field">
              <span class="field-label">Email</span>
              <input v-model="email" type="email" :placeholder="emailPlaceholder" required>
            </label>
            <div class="field-row">
              <label class="field">
                <span class="field-label">First name</span>
                <input v-model="firstName" type="text" placeholder="Optional">
              </label>
              <label class="field">
                <span class="field-label">Last name</span>
                <input v-model="lastName" type="text" placeholder="Optional">
              </label>
            </div>
            <label class="field">
              <span class="field-label">Allergies / dietary</span>
              <textarea v-model="allergies" rows="2" placeholder="Optional — reused for every event" />
            </label>
            <button class="btn submit" type="submit" :disabled="submitting">
              {{ submitting ? 'Signing in…' : 'Sign in' }}
            </button>
          </form>
        </template>

        <template v-else>
          <p class="panel-lede reveal">
            <template v-if="auth.signInDomain">
              Use your <strong>@{{ auth.signInDomain }}</strong> Google account to continue to
              the attendance desk.
            </template>
            <template v-else>
              Use your Google account to continue to the attendance desk.
            </template>
          </p>
          <button class="btn google reveal" @click="signInGoogle">
            <svg class="g" viewBox="0 0 18 18" aria-hidden="true">
              <path fill="#4285F4" d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62z" />
              <path fill="#34A853" d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.02-3.7H.96v2.33A9 9 0 0 0 9 18z" />
              <path fill="#FBBC05" d="M3.98 10.72a5.4 5.4 0 0 1 0-3.44V4.95H.96a9 9 0 0 0 0 8.1l3.02-2.33z" />
              <path fill="#EA4335" d="M9 3.58c1.32 0 2.5.46 3.44 1.35l2.58-2.58A9 9 0 0 0 .96 4.95l3.02 2.33C4.68 5.16 6.66 3.58 9 3.58z" />
            </svg>
            Sign in with Google
          </button>
        </template>

        <p v-if="error" class="error">{{ error }}</p>

        <footer class="panel-foot reveal">
          <span class="lock" aria-hidden="true">●</span>
          <template v-if="auth.signInDomain">
            Restricted to verified <strong>@{{ auth.signInDomain }}</strong> accounts.
          </template>
          <template v-else> Restricted to verified company accounts. </template>
        </footer>
      </section>
    </article>
  </main>
</template>

<style scoped>
.auth {
  display: grid;
  place-items: center;
  min-height: 100vh;
  padding: clamp(16px, 4vw, 48px);
}

/* The spread — a single editorial sheet split into cover + sign-in. */
.spread {
  display: grid;
  grid-template-columns: 1.05fr 0.95fr;
  width: 100%;
  max-width: 940px;
  border: 1px solid var(--border);
  border-top: 3px solid var(--accent);
  background: var(--panel);
  box-shadow: 0 24px 70px rgb(var(--shadow-rgb) / 0.12);
}

/* ── Cover (left) ───────────────────────────────────────────── */
.cover {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 28px;
  padding: 40px 44px 34px;
  overflow: hidden;
  background:
    linear-gradient(180deg, rgb(var(--accent-rgb) / 0.07), transparent 42%),
    var(--panel-2);
  /* Ticket perforation seam. */
  border-right: 1px dashed var(--border);
}
/* Boarding-pass notches punched into the seam. */
.cover::before,
.cover::after {
  content: '';
  position: absolute;
  right: -8px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--bg);
  border: 1px solid var(--border);
}
.cover::before { top: -8px; }
.cover::after { bottom: -8px; }

.cover-head {
  display: flex;
  align-items: center;
  gap: 12px;
}
.wordmark {
  font-family: var(--serif);
  font-weight: 600;
  font-size: 17px;
  letter-spacing: 0.04em;
  color: var(--text);
}
.cover-head .rule {
  flex: 1;
  height: 1px;
  background: var(--border);
}
.kicker {
  font-family: var(--mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.24em;
  text-transform: uppercase;
  color: var(--muted);
}

.cover-lede {
  margin-top: auto;
}
.eyebrow {
  margin: 0 0 16px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.24em;
  text-transform: uppercase;
  color: var(--accent-2);
}
.headline {
  margin: 0;
  font-family: var(--serif);
  font-style: italic;
  font-weight: 360;
  font-size: clamp(34px, 4.6vw, 52px);
  line-height: 1.0;
  letter-spacing: -0.025em;
  color: var(--text);
}
.headline em {
  font-style: italic;
  color: var(--accent-2);
}
.standfirst {
  margin: 20px 0 0;
  max-width: 34ch;
  font-family: var(--mono);
  font-size: 12.5px;
  line-height: 1.7;
  color: var(--text-soft);
}

/* Boarding-pass foot strip. */
.boarding {
  display: grid;
  grid-template-columns: 1.6fr 0.7fr 1fr;
  margin-top: 28px;
  border-top: 1px solid var(--border);
}
.board-cell {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 14px 16px 0 0;
  border-right: 1px solid var(--border-soft);
}
.board-cell:last-child {
  border-right: 0;
  padding-right: 0;
}
.board-key {
  font-family: var(--mono);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--muted);
}
.board-val {
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.04em;
  color: var(--text);
  white-space: nowrap;
}
.board-val--live {
  color: var(--success);
}

/* Passport stamp. */
.stamp {
  position: absolute;
  top: 30px;
  right: 34px;
  display: grid;
  place-items: center;
  width: 96px;
  height: 96px;
  gap: 1px;
  border: 2px solid var(--accent-2);
  border-radius: 50%;
  transform: rotate(-11deg);
  color: var(--accent-2);
  opacity: 0.42;
  text-align: center;
  box-shadow: inset 0 0 0 1px rgb(var(--accent-rgb) / 0.35);
}
.stamp-top,
.stamp-bot {
  font-family: var(--mono);
  font-size: 8px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
.stamp-mid {
  font-family: var(--serif);
  font-style: italic;
  font-weight: 600;
  font-size: 24px;
  line-height: 1;
}

/* ── Sign-in panel (right) ──────────────────────────────────── */
.panel {
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding: 44px 46px;
}
.panel-eyebrow {
  margin: 0 0 14px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.24em;
  text-transform: uppercase;
  color: var(--accent-2);
}
.panel-title {
  margin: 0 0 10px;
  font-family: var(--serif);
  font-style: italic;
  font-weight: 360;
  font-size: clamp(30px, 4vw, 42px);
  line-height: 1.02;
  letter-spacing: -0.02em;
  color: var(--text);
}
.panel-lede {
  margin: 0 0 28px;
  max-width: 36ch;
  font-size: 13.5px;
  line-height: 1.65;
  color: var(--text-soft);
}
.panel-lede strong {
  color: var(--text);
  font-weight: 600;
}

form {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.field-label {
  font-family: var(--mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--muted);
}
.field-row {
  display: flex;
  gap: 18px;
}
.field-row > .field {
  flex: 1;
}

.submit {
  margin-top: 6px;
  align-self: stretch;
}

/* Google button — full width, keeps the colourful mark on the dark base. */
.google {
  width: 100%;
  letter-spacing: 0.16em;
}
.g {
  width: 16px;
  height: 16px;
  /* The 4-colour mark sits inside a white chip so it reads on any button state. */
  background: #fff;
  border-radius: 2px;
  padding: 2px;
  box-sizing: content-box;
}

.panel-foot {
  display: flex;
  align-items: center;
  gap: 9px;
  margin: 30px 0 0;
  padding-top: 18px;
  border-top: 1px solid var(--border-soft);
  font-family: var(--mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--muted);
}
.panel-foot strong {
  color: var(--text-soft);
  font-weight: 600;
}
.lock {
  font-size: 8px;
  color: var(--success);
}

/* ── Page-load reveal (staggered) ───────────────────────────── */
.reveal {
  opacity: 0;
  animation: rise 0.62s cubic-bezier(0.16, 1, 0.3, 1) forwards;
}
.cover-head { animation-delay: 0.02s; }
.cover .eyebrow { animation-delay: 0.10s; }
.headline { animation-delay: 0.16s; }
.standfirst { animation-delay: 0.24s; }
.boarding { animation-delay: 0.32s; }
.panel-eyebrow { animation-delay: 0.20s; }
.panel-title { animation-delay: 0.27s; }
.panel-lede { animation-delay: 0.34s; }
.panel form,
.panel .google { animation-delay: 0.41s; }
.panel-foot { animation-delay: 0.50s; }

@keyframes rise {
  from { opacity: 0; transform: translateY(16px); }
  to { opacity: 1; transform: none; }
}

/* ── Responsive: stack the spread, seam becomes horizontal ──── */
@media (max-width: 720px) {
  .spread {
    grid-template-columns: 1fr;
    max-width: 460px;
  }
  .cover {
    gap: 22px;
    padding: 32px 28px 26px;
    border-right: 0;
    border-bottom: 1px dashed var(--border);
  }
  .cover::before { top: auto; bottom: -8px; right: -8px; }
  .cover::after { bottom: -8px; right: auto; left: -8px; }
  .stamp {
    width: 72px;
    height: 72px;
    top: 24px;
    right: 24px;
  }
  .stamp-mid { font-size: 18px; }
  .panel {
    padding: 32px 28px 34px;
  }
}

@media (max-width: 380px) {
  .field-row { flex-direction: column; gap: 18px; }
  .stamp { display: none; }
}
</style>
