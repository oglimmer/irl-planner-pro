<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { errMsg } from '../api'

// First-login confirm step. The backend seeds the name from the sign-in account
// (often with the given/family split wrong) and never has dietary needs, so we
// ask the user to confirm or correct both once. Saving flips profileConfirmed
// server-side, so this screen is only ever shown again until they save.
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const firstName = ref(auth.user?.firstName ?? '')
const lastName = ref(auth.user?.lastName ?? '')
const allergies = ref(auth.user?.allergies ?? '')
const saving = ref(false)
const error = ref('')

async function confirm() {
  saving.value = true
  error.value = ''
  try {
    await auth.updateProfile(firstName.value.trim(), lastName.value.trim(), allergies.value.trim())
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    router.replace(redirect)
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="welcome">
    <div class="card">
      <p class="eyebrow">Welcome</p>
      <h1>Confirm your details</h1>
      <p class="lede">
        We pulled these from your sign-in account. Please check your name and add
        any allergies or dietary preferences — they're reused for every event, so
        you only enter them once.
      </p>

      <form @submit.prevent="confirm">
        <label>
          Email
          <input :value="auth.user?.email" type="email" disabled>
        </label>
        <div class="row">
          <label>First name <input v-model="firstName" type="text" required></label>
          <label>Last name <input v-model="lastName" type="text" required></label>
        </div>
        <label>
          Allergies / dietary preferences
          <textarea v-model="allergies" rows="2" placeholder="e.g. vegetarian, nut allergy — or leave blank" />
        </label>
        <button class="btn" type="submit" :disabled="saving">Confirm &amp; continue</button>
        <span v-if="error" class="error">{{ error }}</span>
      </form>
    </div>
  </div>
</template>

<style scoped>
.welcome {
  max-width: 480px;
  margin: 4rem auto;
}
.card {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  padding: 1.75rem;
}
.eyebrow {
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-size: 0.75rem;
  color: var(--muted);
  margin: 0;
}
h1 {
  margin: 0.25rem 0 0.75rem;
}
.lede {
  color: var(--muted);
  margin-bottom: 1.25rem;
}
form {
  display: flex;
  flex-direction: column;
  gap: 0.9rem;
}
.row {
  display: flex;
  gap: 1rem;
}
.row > label {
  flex: 1;
}
label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.9rem;
}
input,
textarea {
  padding: 0.55rem 0.7rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font: inherit;
}
input:disabled {
  background: var(--bg);
  color: var(--muted);
}
.btn {
  align-self: flex-start;
}
.error {
  color: var(--danger);
}
</style>
