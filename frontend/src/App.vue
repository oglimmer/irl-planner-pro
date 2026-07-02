<script setup lang="ts">
import { useRoute } from 'vue-router'
import { computed } from 'vue'
import { useAuthStore } from './stores/auth'
import ConfirmDialog from './components/ConfirmDialog.vue'
import UserMenu from './components/UserMenu.vue'
import ThemeSwitcher from './components/ThemeSwitcher.vue'
import BrandLogo from './components/BrandLogo.vue'
import SiteFooter from './components/SiteFooter.vue'

const route = useRoute()
const auth = useAuthStore()
const showChrome = computed(() => !route.meta.hideChrome)
</script>

<template>
  <div class="app">
    <nav v-if="showChrome" class="top">
      <div class="nav-left">
        <RouterLink to="/" class="brand" aria-label="ID5 IRL home">
          <BrandLogo class="brand-logo" />
        </RouterLink>
        <div v-if="auth.user?.isAdmin" class="links">
          <RouterLink to="/admin/events">Events</RouterLink>
          <RouterLink to="/admin/users">Users</RouterLink>
        </div>
      </div>
      <div class="nav-right">
        <UserMenu v-if="auth.user" />
        <ThemeSwitcher v-else />
      </div>
    </nav>
    <main>
      <RouterView />
    </main>
    <SiteFooter v-if="showChrome" />
    <ConfirmDialog />
  </div>
</template>

<style scoped>
/* Column layout so the footer is pushed to the bottom on short pages
   (SiteFooter uses margin-top:auto). */
.app {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
}

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
.nav-left {
  display: flex;
  align-items: center;
  gap: 30px;
}
.nav-right {
  display: flex;
  align-items: center;
}
.brand {
  display: inline-flex;
  align-items: center;
  color: var(--text);
}
.brand-logo {
  height: 24px;
  width: auto;
  display: block;
  color: var(--text);
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

main {
  width: 100%;
  max-width: 1080px;
  margin: 0 auto;
  padding: 48px 32px 96px;
  position: relative;
  z-index: 1;
}

@media (max-width: 720px) {
  nav.top { padding: 14px 18px; gap: 10px; }
  .nav-left { gap: 16px; }
  .links { gap: 14px; }
  main { padding: 32px 18px 64px; }
}
</style>
