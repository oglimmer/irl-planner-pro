<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { computed } from 'vue'
import { useAuthStore } from './stores/auth'
import ConfirmDialog from './components/ConfirmDialog.vue'
import ThemeSwitcher from './components/ThemeSwitcher.vue'
import Id5Logo from './components/Id5Logo.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const showChrome = computed(() => !route.meta.hideChrome)

function logout() {
  const navigated = auth.doLogout()
  if (!navigated) router.push('/login')
}
</script>

<template>
  <div class="app">
    <nav v-if="showChrome" class="top">
      <RouterLink to="/" class="brand" aria-label="ID5 IRL home">
        <Id5Logo class="brand-logo" />
        <em>IRL</em>
      </RouterLink>
      <div class="links">
        <template v-if="auth.user">
          <template v-if="auth.user.isAdmin">
            <RouterLink to="/admin/events">Events</RouterLink>
            <RouterLink to="/admin/users">Users</RouterLink>
          </template>
          <RouterLink to="/profile" class="who">
            <span class="who-at">@</span>{{ auth.user.name || auth.user.email }}
          </RouterLink>
          <button class="signout" @click="logout">Sign out</button>
        </template>
        <ThemeSwitcher />
      </div>
    </nav>
    <main>
      <RouterView />
    </main>
    <ConfirmDialog />
  </div>
</template>

<style scoped>
/* Editorial sticky header — blurred backdrop, mono uppercase links. */
nav.top {
  position: sticky;
  top: 0;
  z-index: 50;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 32px;
  background: rgb(var(--bg-rgb) / 0.82);
  -webkit-backdrop-filter: blur(10px) saturate(140%);
          backdrop-filter: blur(10px) saturate(140%);
  border-bottom: 1px solid var(--border);
}
.brand {
  display: inline-flex;
  align-items: baseline;
  gap: 9px;
  font-family: var(--serif);
  font-size: 20px;
  font-weight: 400;
  letter-spacing: -0.01em;
  color: var(--text);
}
.brand-logo {
  height: 22px;
  width: auto;
  display: block;
  align-self: center;
  color: var(--text);
}
.brand em {
  font-style: italic;
  color: var(--accent-2);
}
.brand:hover { color: var(--text); }

.links {
  display: flex;
  align-items: center;
  gap: 22px;
}
.links a {
  font-family: var(--mono);
  font-size: 11px;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--text-soft);
  padding: 4px 0;
  border-bottom: 1px solid transparent;
  transition: color 0.2s ease, border-color 0.2s ease;
}
.links a:hover,
.links a.router-link-active {
  color: var(--text);
  border-bottom-color: var(--accent);
}

/* Identity chip — the username links to the profile. */
.who {
  text-transform: none !important;
  letter-spacing: 0.04em !important;
  font-size: 11.5px !important;
  color: var(--text-soft);
  padding-left: 16px !important;
  border-left: 1px solid var(--border);
}
.who-at { color: var(--accent); margin-right: 1px; }

/* Sign-out — strip the global editorial button chrome down to a quiet link. */
button.signout {
  background: none;
  border: 0;
  border-bottom: 1px solid transparent;
  border-radius: 0;
  padding: 4px 0;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--text-soft);
  transition: color 0.2s ease, border-color 0.2s ease;
}
button.signout:hover {
  background: none;
  color: var(--text);
  border-bottom-color: var(--accent);
}

main {
  width: 100%;
  max-width: 1080px;
  margin: 0 auto;
  padding: 48px 32px 96px;
  position: relative;
  z-index: 1;
}

@media (max-width: 720px) {
  nav.top { padding: 14px 18px; flex-wrap: wrap; gap: 10px; }
  .links { gap: 14px; flex-wrap: wrap; }
  main { padding: 32px 18px 64px; }
}
</style>
