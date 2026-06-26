<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { THEMES, applyTheme, getStoredTheme, setStoredTheme } from '../theme'

// The account menu consolidates everything identity-related — name/email,
// profile, theme, sign out — behind a single avatar trigger, so the top nav
// only carries primary navigation. Theme stays a purely local preference:
// seed from localStorage (already applied to <html> at boot) and write
// <html data-theme> + localStorage on change.
const router = useRouter()
const auth = useAuthStore()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const theme = ref<string>(getStoredTheme())

const displayName = computed(() => auth.user?.name || auth.user?.email || '')
const email = computed(() => auth.user?.email || '')

// Initials: first letter of up to two name words, falling back to the email.
const initials = computed(() => {
  const name = auth.user?.name?.trim()
  if (name) {
    const parts = name.split(/\s+/)
    const a = parts[0]?.[0] ?? ''
    const b = parts.length > 1 ? (parts[parts.length - 1][0] ?? '') : ''
    return (a + b).toUpperCase()
  }
  return (email.value[0] || '?').toUpperCase()
})

function toggle() {
  open.value = !open.value
}

function close() {
  open.value = false
}

function pickTheme(id: string) {
  theme.value = id
  applyTheme(id)
  setStoredTheme(id)
}

function logout() {
  close()
  const navigated = auth.doLogout()
  if (!navigated) router.push('/login')
}

function onPointerDown(e: PointerEvent) {
  if (root.value && !root.value.contains(e.target as Node)) close()
}
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') close()
}

onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown)
  document.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onPointerDown)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div ref="root" class="user-menu">
    <button
      class="trigger"
      :class="{ active: open }"
      type="button"
      aria-haspopup="menu"
      :aria-expanded="open"
      aria-label="Account menu"
      @click="toggle"
    >
      <span class="avatar">{{ initials }}</span>
    </button>

    <transition name="menu-fade">
      <div v-if="open" class="menu" role="menu">
        <div class="identity">
          <span class="avatar avatar--lg">{{ initials }}</span>
          <span class="identity-text">
            <span class="identity-name">{{ displayName }}</span>
            <span class="identity-email">{{ email }}</span>
          </span>
        </div>

        <div class="divider" />

        <RouterLink to="/profile" class="item" role="menuitem" @click="close">
          <span class="item-icon" aria-hidden="true">
            <svg
              viewBox="0 0 24 24" width="15" height="15" fill="none"
              stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"
            >
              <circle cx="12" cy="8" r="3.4" /><path d="M5 20a7 7 0 0 1 14 0" />
            </svg>
          </span>
          Profile
        </RouterLink>

        <div class="divider" />

        <div class="section-label">Theme</div>
        <button
          v-for="t in THEMES"
          :key="t.id"
          class="item item--theme"
          :class="{ selected: theme === t.id }"
          type="button"
          role="menuitemradio"
          :aria-checked="theme === t.id"
          @click="pickTheme(t.id)"
        >
          <span class="swatch" :data-theme="t.id" aria-hidden="true" />
          {{ t.label }}
          <span v-if="theme === t.id" class="check" aria-hidden="true">
            <svg
              viewBox="0 0 24 24" width="14" height="14" fill="none"
              stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
            >
              <path d="M5 12l4 4L19 6" />
            </svg>
          </span>
        </button>

        <div class="divider" />

        <button class="item item--danger" type="button" role="menuitem" @click="logout">
          <span class="item-icon" aria-hidden="true">
            <svg
              viewBox="0 0 24 24" width="15" height="15" fill="none"
              stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"
            >
              <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="M16 17l5-5-5-5" /><path d="M21 12H9" />
            </svg>
          </span>
          Sign out
        </button>
      </div>
    </transition>
  </div>
</template>

<style scoped>
.user-menu {
  position: relative;
  display: inline-flex;
}

/* Avatar trigger — a quiet circle that lifts on hover/open. */
.trigger {
  background: none;
  border: 0;
  border-radius: 999px;
  padding: 2px;
  cursor: pointer;
  line-height: 0;
  transition: box-shadow 0.2s ease;
}
.trigger:hover .avatar,
.trigger.active .avatar {
  border-color: var(--accent);
  color: var(--text);
}

.avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: rgb(var(--bg-rgb) / 0.6);
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--text-soft);
  transition: color 0.2s ease, border-color 0.2s ease;
}
.avatar--lg {
  width: 38px;
  height: 38px;
  font-size: 13px;
  color: var(--text);
  border-color: var(--accent);
}

/* Dropdown panel. */
.menu {
  position: absolute;
  top: calc(100% + 10px);
  right: 0;
  min-width: 232px;
  padding: 7px;
  background: rgb(var(--bg-rgb) / 0.97);
  -webkit-backdrop-filter: blur(12px) saturate(140%);
          backdrop-filter: blur(12px) saturate(140%);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 12px 32px rgb(0 0 0 / 0.18), 0 2px 8px rgb(0 0 0 / 0.08);
  z-index: 60;
}

.identity {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 9px 10px 11px;
}
.identity-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 2px;
}
.identity-name {
  font-family: var(--serif);
  font-size: 14px;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.identity-email {
  font-family: var(--mono);
  font-size: 10.5px;
  letter-spacing: 0.02em;
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.divider {
  height: 1px;
  margin: 5px 4px;
  background: var(--border);
}

.section-label {
  padding: 4px 11px 5px;
  font-family: var(--mono);
  font-size: 9.5px;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--muted);
}

/* Menu rows — links and buttons share one look. */
.item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 11px;
  border: 0;
  border-radius: 8px;
  background: none;
  font-family: var(--mono);
  font-size: 12px;
  letter-spacing: 0.01em;
  text-transform: none;
  color: var(--text-soft);
  text-align: left;
  cursor: pointer;
  transition: background 0.14s ease, color 0.14s ease;
}
.item:hover {
  background: rgb(var(--text-rgb) / 0.05);
  color: var(--text);
}
.item-icon {
  display: inline-flex;
  color: var(--muted);
}
.item:hover .item-icon { color: var(--text-soft); }

/* Theme rows settle by alignment: the active theme anchors left, the rest
   tuck to the right and recede — so the list reads as one choice, not five. */
.item--theme { justify-content: flex-end; color: var(--muted); }
.item--theme.selected { justify-content: flex-start; color: var(--text); }
.swatch {
  width: 14px;
  height: 14px;
  border-radius: 999px;
  border: 1px solid var(--border);
  flex: none;
}
/* Each swatch previews its palette's accent (mirrors styles.css per-theme). */
.swatch[data-theme='light']    { background: #f5ac11; }
.swatch[data-theme='dark']     { background: #f5a524; }
.swatch[data-theme='midnight'] { background: #38bdf8; }
.swatch[data-theme='sepia']    { background: #b5890a; }
.swatch[data-theme='contrast'] { background: #0b5fff; }
.check {
  color: var(--accent);
  display: inline-flex;
}

.item--danger:hover {
  background: color-mix(in srgb, var(--danger) 12%, transparent);
  color: var(--danger);
}
.item--danger:hover .item-icon { color: var(--danger); }

.menu-fade-enter-active,
.menu-fade-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.menu-fade-enter-from,
.menu-fade-leave-to {
  opacity: 0;
  transform: translateY(-6px) scale(0.98);
}
</style>
