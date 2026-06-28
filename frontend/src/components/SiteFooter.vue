<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useBuildInfo } from '../composables/useBuildInfo'

const { frontend, backend, load } = useBuildInfo()

onMounted(() => {
  load()
})

function shortCommit(commit: string): string {
  return commit && commit !== 'unknown' ? commit.slice(0, 7) : commit
}

// Render an ISO build time as a compact, locale-independent UTC date; leave
// non-timestamps ('unknown', 'dev') untouched.
function shortTime(t: string): string {
  if (!t || t === 'unknown') return t
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? t : d.toISOString().slice(0, 16).replace('T', ' ') + ' UTC'
}

function line(version: string, commit: string, buildTime: string): string {
  return `v${version} · ${shortCommit(commit)} · ${shortTime(buildTime)}`
}

const frontendLine = computed(() => line(frontend.version, frontend.gitCommit, frontend.buildTime))
const backendLine = computed(() =>
  backend.value
    ? line(backend.value.version, backend.value.gitCommit, backend.value.buildTime)
    : '…',
)
</script>

<template>
  <footer class="site-footer">
    <dl class="versions" aria-label="Build information">
      <div class="row">
        <dt>Frontend</dt>
        <dd>{{ frontendLine }}</dd>
      </div>
      <div class="row">
        <dt>Backend</dt>
        <dd>{{ backendLine }}</dd>
      </div>
    </dl>
  </footer>
</template>

<style scoped>
.site-footer {
  border-top: 1px solid var(--border);
  padding: 18px 32px;
  margin-top: auto;
}
.versions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 28px;
  margin: 0;
  max-width: 1080px;
  margin-inline: auto;
}
.row {
  display: flex;
  align-items: baseline;
  gap: 9px;
}
dt {
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--text-soft);
}
dd {
  margin: 0;
  font-family: var(--mono);
  font-size: 11px;
  color: var(--text-soft);
}
@media (max-width: 720px) {
  .site-footer { padding: 16px 18px; }
}
</style>
