import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, isJwtExpired, errStatus } from '../api'
import type { AuthMode, User } from '../types'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(loadToken())
  const user = ref<User | null>(loadUser())
  const mode = ref<AuthMode | null>(null)
  const defaultEventTimezone = ref<string>('Europe/Paris')
  let modePromise: Promise<AuthMode> | null = null
  let freshUserPromise: Promise<void> | null = null

  // loadToken reads the stored JWT but discards it (and the cached user) if it
  // has already expired, so a tab reopened weeks later starts clean.
  function loadToken(): string | null {
    const t = localStorage.getItem('token')
    if (t && isJwtExpired(t)) {
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      return null
    }
    return t
  }

  function loadUser(): User | null {
    const raw = localStorage.getItem('user')
    if (!raw) return null
    try {
      const u = JSON.parse(raw) as Partial<User> | null
      if (!u || typeof u !== 'object' || !u.id) return null
      return u as User
    } catch {
      return null
    }
  }

  function setSession(t: string, u: User) {
    localStorage.setItem('token', t)
    localStorage.setItem('user', JSON.stringify(u))
    token.value = t
    user.value = u
    freshUserPromise = null
  }

  // ensureMode fetches /api/auth/config once and caches the in-flight promise.
  async function ensureMode(): Promise<AuthMode> {
    if (mode.value) return mode.value
    if (!modePromise) {
      modePromise = api
        .authConfig()
        .then((c) => {
          mode.value = c.mode
          defaultEventTimezone.value = c.defaultEventTimezone
          return c.mode
        })
        .catch((e) => {
          modePromise = null
          throw e
        })
    }
    return modePromise
  }

  // ensureFreshUser refreshes /api/me at most once per app load, so a demoted
  // or revoked user can't keep navigating on stale localStorage claims.
  function ensureFreshUser(): Promise<void> {
    if (!token.value) return Promise.resolve()
    if (!freshUserPromise) {
      freshUserPromise = refreshUser().catch((e: unknown) => {
        if (errStatus(e) === 401) {
          logout()
        } else {
          freshUserPromise = null
          throw e
        }
      })
    }
    return freshUserPromise
  }

  async function refreshUser() {
    if (!token.value) return
    const u = await api.me()
    localStorage.setItem('user', JSON.stringify(u))
    user.value = u
  }

  function loginViaOIDC() {
    window.location.href = '/api/auth/oidc/login'
  }

  // devLogin is the password-mode (local dev) sign-in.
  async function devLogin(email: string, name: string) {
    const r = await api.devLogin(email, name)
    setSession(r.token, r.user)
  }

  function logout() {
    token.value = null
    user.value = null
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    freshUserPromise = null
  }

  // doLogout clears local state, then in OIDC mode bounces through RP-initiated
  // logout (tearing down the upstream session). Returns true when the browser
  // has been navigated away (caller should NOT push a route).
  function doLogout(): boolean {
    const oidc = mode.value === 'oidc'
    logout()
    if (oidc) {
      window.location.href = '/api/auth/oidc/logout'
      return true
    }
    return false
  }

  return {
    user,
    token,
    mode,
    defaultEventTimezone,
    ensureMode,
    ensureFreshUser,
    refreshUser,
    loginViaOIDC,
    devLogin,
    logout,
    doLogout,
    setSession,
  }
})
