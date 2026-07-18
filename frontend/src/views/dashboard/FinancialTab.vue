<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, errMsg } from '../../api'
import { formatMoney } from '../../lib/currencies'
import type { FinancialReport } from '../../types'

const props = defineProps<{
  eventId: string
  // True while the Financial tab is the visible one. The report makes a live FX
  // call, so we fetch lazily the first time the tab is opened rather than on every
  // dashboard load.
  active: boolean
}>()

const emit = defineEmits<{ error: [string] }>()

const report = ref<FinancialReport | null>(null)
const loading = ref(false)
const loaded = ref(false)

async function load() {
  loading.value = true
  try {
    report.value = await api.financial(props.eventId)
    loaded.value = true
  } catch (e) {
    emit('error', errMsg(e))
  } finally {
    loading.value = false
  }
}

// Fetch on first activation, and let the manual Refresh button re-pull afterwards.
watch(
  () => props.active,
  (isActive) => {
    if (isActive && !loaded.value && !loading.value) void load()
  },
  { immediate: true },
)

function cell(row: FinancialReport['rows'][number], target: string): string {
  if (!row.converted || row.converted[target] == null) return '—'
  return formatMoney(row.converted[target], target)
}
</script>

<template>
  <div class="financial">
    <p v-if="loading && !report" class="muted">Loading…</p>

    <template v-else-if="report">
      <div class="bar">
        <p class="muted note">
          <template v-if="report.ratesAvailable">
            Converted at exchange rates as of {{ report.ratesAsOf }}.
          </template>
          <template v-else>
            Live exchange rates are currently unavailable — showing original amounts only.
          </template>
        </p>
        <button class="btn secondary" :disabled="loading" @click="load">
          {{ loading ? 'Refreshing…' : 'Refresh' }}
        </button>
      </div>

      <div v-if="report.rows.length" class="table-scroll">
        <table class="grid">
          <thead>
            <tr>
              <th>Name</th>
              <th>Email</th>
              <th class="num">Original</th>
              <th v-for="t in report.targets" :key="t" class="num">{{ t }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in report.rows" :key="row.userId">
              <td>{{ row.name }}</td>
              <td class="muted">{{ row.email }}</td>
              <td class="num">{{ formatMoney(row.amount, row.currency) }}</td>
              <td v-for="t in report.targets" :key="t" class="num">{{ cell(row, t) }}</td>
            </tr>
          </tbody>
          <tfoot v-if="report.ratesAvailable">
            <tr>
              <td class="total-label">Total</td>
              <td />
              <td />
              <td v-for="t in report.targets" :key="t" class="num total">
                {{ report.totals[t] != null ? formatMoney(report.totals[t], t) : '—' }}
              </td>
            </tr>
          </tfoot>
        </table>
      </div>
      <p v-else class="muted">No flight costs reported yet.</p>
    </template>
  </div>
</template>

<style scoped>
.muted { color: var(--muted); }
.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
  margin: 0.25rem 0 1rem;
}
.note { margin: 0; font-size: 0.85rem; }
.table-scroll { overflow-x: auto; }
.grid {
  width: 100%;
  border-collapse: collapse;
}
.grid th, .grid td {
  text-align: left;
  padding: 0.55rem 0.6rem;
  border-bottom: 1px solid var(--border);
}
.grid th {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--muted);
  white-space: nowrap;
}
.num {
  text-align: right;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.grid tfoot td {
  border-top: 2px solid var(--border);
  border-bottom: none;
  font-weight: 600;
}
.total-label {
  text-transform: uppercase;
  font-size: 0.72rem;
  letter-spacing: 0.05em;
  color: var(--muted);
}
.total { color: var(--text); }
</style>
