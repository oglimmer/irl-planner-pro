<script setup lang="ts">
import { ref } from 'vue'
import { THEMES, applyTheme, getStoredTheme, setStoredTheme } from '../theme'

// Theme is a purely local preference here (no server-side sync): the ref seeds
// from localStorage — already applied to <html> at boot — and each change writes
// <html data-theme> + localStorage.
const current = ref<string>(getStoredTheme())

function onChange() {
  applyTheme(current.value)
  setStoredTheme(current.value)
}
</script>

<template>
  <label class="theme-switcher">
    <span class="theme-switcher__label">Theme</span>
    <select
      v-model="current"
      class="theme-switcher__select"
      aria-label="Theme"
      title="Choose a theme"
      @change="onChange"
    >
      <option v-for="t in THEMES" :key="t.id" :value="t.id">{{ t.label }}</option>
    </select>
  </label>
</template>

<style scoped>
.theme-switcher {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin: 0;
}
.theme-switcher__label {
  font-family: var(--mono);
  font-size: 10.5px;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--muted);
}
/* Compact, inline variant of the global <select> — auto width, no full-row
   bottom-border block. */
.theme-switcher__select {
  width: auto;
  padding: 4px 4px 5px;
  border-bottom: 1px solid var(--border);
  font-family: var(--mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  color: var(--text-soft);
  cursor: pointer;
  transition: color 0.2s ease, border-color 0.2s ease;
}
.theme-switcher__select:hover { color: var(--text); }
.theme-switcher__select:focus { border-bottom-color: var(--accent); }
</style>
