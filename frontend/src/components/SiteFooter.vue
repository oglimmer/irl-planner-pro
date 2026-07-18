<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useBuildInfo } from '../composables/useBuildInfo'

const { frontend, backend, load } = useBuildInfo()

const year = new Date().getFullYear()
const open = ref(false)
const popRef = ref<HTMLElement | null>(null)

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

// Lazy: only hit /api/version the first time the popover is opened.
function toggle() {
  open.value = !open.value
  if (open.value) load()
}

function onDocClick(e: MouseEvent) {
  if (open.value && popRef.value && !popRef.value.contains(e.target as Node)) {
    open.value = false
  }
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKey)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKey)
})
</script>

<template>
  <footer class="site-footer">
    <div class="bar">
      <p class="credit">
        © {{ year }} Oli Zimpasser ·
        <a
          class="lic-link"
          href="https://github.com/oglimmer/irl-planner-pro/blob/main/LICENSE"
          target="_blank"
          rel="noopener noreferrer"
        >MIT License</a>
      </p>

      <div class="actions">
        <div ref="popRef" class="version-popover">
          <button
            type="button"
            class="icon-btn"
            :aria-expanded="open"
            aria-label="Build version info"
            title="Build version info"
            @click="toggle"
          >
            <svg
              viewBox="0 0 24 24"
              width="16"
              height="16"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
              aria-hidden="true"
            >
              <circle cx="12" cy="12" r="10" />
              <line x1="12" y1="16" x2="12" y2="12" />
              <line x1="12" y1="8" x2="12.01" y2="8" />
            </svg>
          </button>
          <div v-if="open" class="popover" role="dialog" aria-label="Build information">
            <dl class="versions">
              <div class="row">
                <dt>Frontend</dt>
                <dd>{{ frontendLine }}</dd>
              </div>
              <div class="row">
                <dt>Backend</dt>
                <dd>{{ backendLine }}</dd>
              </div>
            </dl>
          </div>
        </div>

        <a
          class="gh-link"
          href="https://github.com/oglimmer/irl-planner-pro"
          target="_blank"
          rel="noopener noreferrer"
          aria-label="View source on GitHub"
          title="View source on GitHub"
        >
          <svg class="gh-icon" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
            <path
              fill="currentColor"
              d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.65 7.65 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z"
            />
          </svg>
          <span>GitHub</span>
        </a>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.site-footer {
  border-top: 1px solid var(--border);
  padding: 14px 32px;
  margin-top: auto;
}
.bar {
  max-width: 1080px;
  margin-inline: auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px 20px;
}
.credit {
  margin: 0;
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--text-soft);
}
.lic-link {
  color: var(--text-soft);
  text-decoration: underline;
  text-underline-offset: 2px;
  transition: color 0.2s ease;
}
.lic-link:hover {
  color: var(--text);
}

.actions {
  display: flex;
  align-items: center;
  gap: 14px;
}

/* Info icon + popover */
.version-popover {
  position: relative;
  display: inline-flex;
}
.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-soft);
  border-radius: 4px;
  transition: color 0.2s ease;
}
.icon-btn:hover,
.icon-btn[aria-expanded='true'] {
  color: var(--text);
}
.popover {
  position: absolute;
  bottom: calc(100% + 8px);
  right: 0;
  z-index: 60;
  min-width: 260px;
  padding: 12px 14px;
  background: rgb(var(--bg-rgb) / 0.96);
  -webkit-backdrop-filter: blur(10px) saturate(140%);
          backdrop-filter: blur(10px) saturate(140%);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgb(0 0 0 / 0.18);
}
.versions {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.row {
  display: flex;
  align-items: baseline;
  gap: 10px;
}
dt {
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--text-soft);
  min-width: 64px;
}
dd {
  margin: 0;
  font-family: var(--mono);
  font-size: 11px;
  color: var(--text);
}

.gh-link {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--text-soft);
  transition: color 0.2s ease;
}
.gh-link:hover {
  color: var(--text);
}
.gh-icon {
  display: block;
}

@media (max-width: 720px) {
  .site-footer { padding: 12px 18px; }
  .gh-link span { display: none; }
}
</style>
