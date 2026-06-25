<script setup lang="ts">
import { onMounted, ref } from 'vue'
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
const submitting = ref(false)

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
    await auth.devLogin(email.value, firstName.value, lastName.value)
    router.replace('/')
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="login">
    <div class="card">
      <p class="eyebrow">ID5</p>
      <h1>IRL Attendance</h1>

      <template v-if="ready && auth.mode === 'password'">
        <p class="lede">Local dev sign-in.</p>
        <form @submit.prevent="signInDev">
          <input v-model="email" type="email" placeholder="you@id5.io" required>
          <input v-model="firstName" type="text" placeholder="First name (optional)">
          <input v-model="lastName" type="text" placeholder="Last name (optional)">
          <button class="btn" type="submit" :disabled="submitting">Sign in</button>
        </form>
      </template>

      <template v-else>
        <p class="lede">Sign in with your @id5.io Google account to continue.</p>
        <button class="btn" @click="signInGoogle">Sign in with Google</button>
      </template>

      <p v-if="error" class="error">{{ error }}</p>
    </div>
  </div>
</template>

<style scoped>
.login {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: 1.5rem;
}
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 2.5rem 2.75rem;
  max-width: 380px;
  width: 100%;
  text-align: center;
}
.eyebrow {
  font-size: 0.7rem;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--accent);
  margin: 0 0 0.5rem;
}
h1 {
  margin: 0 0 0.75rem;
}
.lede {
  color: var(--muted);
  margin: 0 0 1.5rem;
}
form {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
input {
  padding: 0.55rem 0.7rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font: inherit;
}
.error {
  color: var(--danger);
  margin-top: 1rem;
}
</style>
