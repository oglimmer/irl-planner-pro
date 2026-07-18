<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ code: number }>()

const copy = computed(() => {
  switch (props.code) {
    case 403:
      return { title: 'Not allowed', message: "You don't have access to this page." }
    case 404:
      return { title: 'Page not found', message: "That page doesn't exist." }
    case 503:
      return { title: 'Service unavailable', message: 'Please try again in a moment.' }
    default:
      return { title: 'Something went wrong', message: 'An unexpected error occurred.' }
  }
})
</script>

<template>
  <div class="error">
    <p class="code">Error {{ code }}</p>
    <h1>{{ copy.title }}</h1>
    <p class="lede">{{ copy.message }}</p>
    <RouterLink to="/" class="btn secondary">← Back home</RouterLink>
  </div>
</template>

<style scoped>
.error {
  max-width: 460px;
  margin: 4rem auto;
  text-align: center;
}
.code {
  font-size: 0.7rem;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--accent);
}
.lede {
  color: var(--muted);
  margin-bottom: 1.5rem;
}
</style>
