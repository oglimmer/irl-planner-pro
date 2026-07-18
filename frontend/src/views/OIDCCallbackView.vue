<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import type { User } from '../types'

const router = useRouter()
const auth = useAuthStore()
const error = ref('')

// errorCopyFor builds the failure message. The domain-not-allowed case names the
// configured sign-in domain (falls back generic when none is configured).
function errorCopyFor(code: string, domain: string): string {
  switch (code) {
    case 'domain_not_allowed':
      return domain
        ? `Your account isn't allowed. Sign in with your @${domain} Google account.`
        : "Your account isn't allowed. Sign in with an authorized Google account."
    case 'account_error':
      return "We couldn't set up your account. Please contact the IRL team."
    default:
      return 'Sign-in failed. Please try again.'
  }
}

onMounted(async () => {
  // The backend redirects here with the session in the URL fragment:
  //   #token=<jwt>&user=<base64url-json>   or   #error=<code>
  const frag = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const errCode = frag.get('error')
  if (errCode) {
    // Fetch config so the domain-not-allowed copy names the right domain.
    await auth.ensureMode().catch(() => {})
    error.value = errorCopyFor(errCode, auth.signInDomain)
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
