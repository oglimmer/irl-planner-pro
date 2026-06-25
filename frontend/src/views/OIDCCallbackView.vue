<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import type { User } from '../types'

const router = useRouter()
const auth = useAuthStore()
const error = ref('')

const errorCopy: Record<string, string> = {
  domain_not_allowed: 'Your account isn\'t allowed. Sign in with your @id5.io Google account.',
  provider_error: 'Sign-in failed. Please try again.',
  account_error: 'We couldn\'t set up your account. Please contact the People team.',
}

onMounted(() => {
  // The backend redirects here with the session in the URL fragment:
  //   #token=<jwt>&user=<base64url-json>   or   #error=<code>
  const frag = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const errCode = frag.get('error')
  if (errCode) {
    error.value = errorCopy[errCode] ?? 'Sign-in failed. Please try again.'
    return
  }
  const token = frag.get('token')
  const userB64 = frag.get('user')
  if (!token || !userB64) {
    error.value = 'Sign-in failed. Please try again.'
    return
  }
  try {
    const json = atob(userB64.replace(/-/g, '+').replace(/_/g, '/'))
    const user = JSON.parse(json) as User
    auth.setSession(token, user)
    // Strip the fragment so the token doesn't linger in history.
    window.history.replaceState(null, '', window.location.pathname)
    router.replace('/')
  } catch {
    error.value = 'Sign-in failed. Please try again.'
  }
})
</script>

<template>
  <div class="callback">
    <template v-if="error">
      <h1>Sign-in problem</h1>
      <p class="lede">{{ error }}</p>
      <RouterLink to="/login" class="btn secondary">Back to sign in</RouterLink>
    </template>
    <p v-else class="lede">Signing you in…</p>
  </div>
</template>

<style scoped>
.callback {
  max-width: 420px;
  margin: 5rem auto;
  text-align: center;
}
.lede {
  color: var(--muted);
  margin-bottom: 1.5rem;
}
</style>
