<script setup lang="ts">
import { ref } from 'vue'
import { api, errMsg } from '../api'
import { useAuthStore } from '../stores/auth'
import { useAsyncData } from '../composables/useAsyncData'
import { useConfirm } from '../composables/useConfirm'
import type { UserSummary } from '../types'

const auth = useAuthStore()
const { confirm } = useConfirm()
const { data: users, loading, error, reload } = useAsyncData<UserSummary[]>(
  () => api.listUsers(),
  [],
)
const busyId = ref<string | null>(null)

async function setAdmin(u: UserSummary, makeAdmin: boolean) {
  const who = u.name || u.email
  const ok = await confirm({
    title: makeAdmin ? 'Grant admin access?' : 'Remove admin access?',
    message: makeAdmin
      ? `${who} will be able to create and edit events, upload rosters, view all responses, and manage other users.`
      : `${who} will lose access to event management and all admin tools.`,
    confirmLabel: makeAdmin ? 'Make admin' : 'Remove admin',
    danger: !makeAdmin,
  })
  if (!ok) return
  busyId.value = u.id
  error.value = ''
  try {
    if (makeAdmin) await api.promoteUser(u.id)
    else await api.demoteUser(u.id)
    await reload()
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    busyId.value = null
  }
}
</script>

<template>
  <section>
    <h1>Users</h1>
    <p class="muted">
      The first person to sign in is an admin. Promote or demote others here.
    </p>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="loading" class="muted">Loading…</p>

    <table v-else class="users">
      <thead>
        <tr>
          <th>Name</th>
          <th>Email</th>
          <th>Role</th>
          <th />
        </tr>
      </thead>
      <tbody>
        <tr v-for="u in users" :key="u.id">
          <td>{{ u.name || '—' }}</td>
          <td>{{ u.email }}</td>
          <td>
            <span :class="['badge', u.isAdmin ? 'admin' : 'member']">
              {{ u.isAdmin ? 'Admin' : 'Member' }}
            </span>
          </td>
          <td class="actions">
            <button
              v-if="!u.isAdmin"
              class="btn secondary sm"
              :disabled="busyId === u.id"
              @click="setAdmin(u, true)"
            >
              Make admin
            </button>
            <button
              v-else
              class="btn secondary sm"
              :disabled="busyId === u.id || u.id === auth.user?.id"
              :title="u.id === auth.user?.id ? 'You cannot demote yourself' : ''"
              @click="setAdmin(u, false)"
            >
              Remove admin
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
.muted {
  color: var(--muted);
}
.error {
  color: var(--danger);
}
.users {
  width: 100%;
  border-collapse: collapse;
  margin-top: 1rem;
}
.users th,
.users td {
  text-align: left;
  padding: 0.6rem 0.5rem;
  border-bottom: 1px solid var(--border);
}
.users th {
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--muted);
}
.badge {
  font-size: 0.75rem;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
}
.badge.admin {
  background: rgb(var(--accent-rgb) / 0.07);
  color: var(--accent);
}
.badge.member {
  background: var(--bg-2);
  color: var(--muted);
}
.actions {
  text-align: right;
}
.btn.sm {
  padding: 0.3rem 0.6rem;
  font-size: 0.85rem;
}
</style>
