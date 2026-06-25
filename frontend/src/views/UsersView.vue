<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, errMsg } from '../api'
import { useAuthStore } from '../stores/auth'
import type { UserSummary } from '../types'

const auth = useAuthStore()
const users = ref<UserSummary[]>([])
const loading = ref(true)
const error = ref('')
const busyId = ref<string | null>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    users.value = await api.listUsers()
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    loading.value = false
  }
}

async function setAdmin(u: UserSummary, makeAdmin: boolean) {
  busyId.value = u.id
  error.value = ''
  try {
    if (makeAdmin) await api.promoteUser(u.id)
    else await api.demoteUser(u.id)
    await load()
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    busyId.value = null
  }
}

onMounted(load)
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
  background: #eef0ff;
  color: var(--accent);
}
.badge.member {
  background: #f0f1f4;
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
