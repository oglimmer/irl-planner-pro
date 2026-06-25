<script setup lang="ts">
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
</script>

<template>
  <section>
    <h1>Welcome{{ auth.user ? `, ${auth.user.name || auth.user.email}` : '' }}</h1>

    <template v-if="auth.user?.isAdmin">
      <p class="muted">Manage offsites and attendance.</p>
      <div class="cards">
        <RouterLink to="/admin/events" class="card">
          <span class="card-title">Events</span>
          <span class="card-sub">Configure offsites, rosters, dashboards</span>
        </RouterLink>
        <RouterLink to="/admin/users" class="card">
          <span class="card-title">Users</span>
          <span class="card-sub">Manage admins</span>
        </RouterLink>
      </div>
    </template>

    <p v-else class="muted">
      When you receive an event link, open it to submit your attendance and
      travel details.
    </p>
  </section>
</template>

<style scoped>
.muted {
  color: var(--muted);
}
.cards {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  margin-top: 1rem;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 1.1rem 1.3rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  text-decoration: none;
  color: var(--text);
  min-width: 220px;
}
.card:hover {
  border-color: var(--accent);
}
.card-title {
  font-weight: 650;
}
.card-sub {
  color: var(--muted);
  font-size: 0.88rem;
}
</style>
