<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { computed } from 'vue'
import { useAuthStore } from './stores/auth'
import ConfirmDialog from './components/ConfirmDialog.vue'

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
    <header v-if="showChrome" class="app-header">
      <RouterLink to="/" class="brand">ID5 IRL Attendance</RouterLink>
      <nav v-if="auth.user" class="nav">
        <template v-if="auth.user.isAdmin">
          <RouterLink to="/admin/events" class="nav-link">Events</RouterLink>
          <RouterLink to="/admin/users" class="nav-link">Users</RouterLink>
        </template>
        <span class="who">{{ auth.user.name || auth.user.email }}</span>
        <button class="link-btn" @click="logout">Sign out</button>
      </nav>
    </header>
    <main class="app-main">
      <RouterView />
    </main>
    <ConfirmDialog />
  </div>
</template>

<style scoped>
.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.85rem 1.5rem;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.brand {
  font-weight: 650;
  color: var(--text);
  text-decoration: none;
}
.nav {
  display: flex;
  align-items: center;
  gap: 1rem;
}
.nav-link {
  text-decoration: none;
  color: var(--accent);
}
.who {
  color: var(--muted);
  font-size: 0.9rem;
}
.link-btn {
  background: none;
  border: none;
  color: var(--muted);
  text-decoration: underline;
  padding: 0;
}
.app-main {
  max-width: 960px;
  margin: 0 auto;
  padding: 1.5rem;
}
</style>
