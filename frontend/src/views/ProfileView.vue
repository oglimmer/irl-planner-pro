<script setup lang="ts">
import { ref } from 'vue'
import { useAuthStore } from '../stores/auth'
import { errMsg } from '../api'

const auth = useAuthStore()

const firstName = ref(auth.user?.firstName ?? '')
const lastName = ref(auth.user?.lastName ?? '')
const allergies = ref(auth.user?.allergies ?? '')
const saving = ref(false)
const saved = ref(false)
const error = ref('')

async function save() {
  saving.value = true
  saved.value = false
  error.value = ''
  try {
    await auth.updateProfile(firstName.value.trim(), lastName.value.trim(), allergies.value.trim())
    saved.value = true
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="profile">
    <h1>Your profile</h1>
    <p class="muted">
      Your name and dietary needs are shown to admins on event dashboards and
      exports, and reused for every event so you only enter them once. Your name
      was set from your sign-in account; you can change anything here at any time.
    </p>

    <form @submit.prevent="save">
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
        <textarea v-model="allergies" rows="2" />
      </label>
      <button class="btn" type="submit" :disabled="saving">Save</button>
      <span v-if="saved" class="ok">Saved.</span>
      <span v-if="error" class="error">{{ error }}</span>
    </form>
  </section>
</template>

<style scoped>
.profile {
  max-width: 480px;
}
.muted {
  color: var(--muted);
}
form {
  display: flex;
  flex-direction: column;
  gap: 0.9rem;
  margin-top: 1.25rem;
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
.ok {
  color: var(--accent);
}
.error {
  color: var(--danger);
}
</style>
