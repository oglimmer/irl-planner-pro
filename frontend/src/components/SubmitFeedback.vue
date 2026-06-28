<script setup lang="ts">
// Prominent save/submit feedback used by the attendee RSVP form and the profile
// page. Success is celebratory — a confetti burst and a drawn checkmark — while
// an error is a clear, hard-to-miss alert. Both animations replay on every fresh
// result (keyed by an internal counter) and fall back to a static banner under
// `prefers-reduced-motion`.
import { ref, watch } from 'vue'

const props = defineProps<{
  /** Non-empty string shows the error banner (takes precedence over success). */
  error?: string
  /** True shows the joyful success banner. */
  success?: boolean
  /** Headline for the success banner, e.g. "You're all set!". */
  successTitle?: string
  /** Optional supporting line under the success headline. */
  successMessage?: string
}>()

// Bumping these keys remounts the banner so its entrance animation replays each
// time a new result arrives — even when going success → success or error → error.
const successKey = ref(0)
const errorKey = ref(0)
watch(
  () => props.success,
  (v) => {
    if (v) successKey.value++
  },
)
watch(
  () => props.error,
  (v) => {
    if (v) errorKey.value++
  },
)

// Deterministic confetti: pieces fan out evenly with per-index variety in
// distance, colour, spin and delay so the burst looks organic without RNG.
const COLORS = ['var(--accent)', 'var(--blue)', 'var(--success)', 'var(--accent-2)']
const CONFETTI = Array.from({ length: 18 }, (_, i) => {
  const angle = (i / 18) * 360 + (i % 2 ? 10 : -10)
  const dist = 70 + (i % 5) * 16
  const rad = (angle * Math.PI) / 180
  return {
    x: `${Math.round(Math.cos(rad) * dist)}px`,
    y: `${Math.round(Math.sin(rad) * dist)}px`,
    color: COLORS[i % COLORS.length],
    spin: `${(i % 2 ? 1 : -1) * (180 + (i % 4) * 90)}deg`,
    delay: `${(i % 6) * 28}ms`,
    size: i % 3 === 0 ? '11px' : '7px',
    round: i % 4 === 0,
  }
})
</script>

<template>
  <Transition name="feedback">
    <div
      v-if="error"
      :key="errorKey"
      class="fb fb--error"
      role="alert"
      aria-live="assertive"
    >
      <span class="fb-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="24" height="24">
          <path
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 3 1.5 21h21L12 3Z"
          />
          <path
            fill="none"
            stroke="currentColor"
            stroke-width="2.2"
            stroke-linecap="round"
            d="M12 10v4.5"
          />
          <circle cx="12" cy="18" r="1.2" fill="currentColor" />
        </svg>
      </span>
      <div class="fb-text">
        <strong class="fb-title">Couldn't save your response</strong>
        <span class="fb-msg">{{ error }}</span>
      </div>
    </div>

    <div
      v-else-if="success"
      :key="successKey"
      class="fb fb--success"
      role="status"
      aria-live="polite"
    >
      <span class="confetti" aria-hidden="true">
        <span
          v-for="(c, i) in CONFETTI"
          :key="i"
          class="piece"
          :class="{ round: c.round }"
          :style="{
            '--x': c.x,
            '--y': c.y,
            '--color': c.color,
            '--spin': c.spin,
            '--delay': c.delay,
            '--size': c.size,
          }"
        />
      </span>
      <span class="fb-icon check" aria-hidden="true">
        <svg viewBox="0 0 36 36" width="30" height="30">
          <circle class="check-ring" cx="18" cy="18" r="16" />
          <path class="check-mark" d="M11 18.5 16 23.5 25.5 13" />
        </svg>
      </span>
      <div class="fb-text">
        <strong class="fb-title">{{ successTitle || "You're all set!" }}</strong>
        <span v-if="successMessage" class="fb-msg">{{ successMessage }}</span>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.fb {
  position: relative;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 20px;
  border-radius: var(--radius);
  overflow: visible;
}

.fb-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.fb-title {
  font-size: 15px;
  font-weight: 650;
  letter-spacing: -0.01em;
  line-height: 1.25;
}
.fb-msg {
  font-size: 13px;
  line-height: 1.45;
}
.fb-icon {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* ── Success — joyful ─────────────────────────────────────────── */
.fb--success {
  border: 1px solid rgb(var(--success-rgb) / 0.45);
  border-left: 3px solid var(--success);
  background:
    linear-gradient(180deg, rgb(var(--success-rgb) / 0.16), rgb(var(--success-rgb) / 0.06));
}
.fb--success .fb-title {
  color: var(--success);
}
.fb--success .fb-msg {
  color: var(--text-soft);
}
.fb--success .check {
  color: var(--success);
}

.check-ring {
  fill: none;
  stroke: currentColor;
  stroke-width: 2.4;
  opacity: 0.35;
  stroke-dasharray: 101;
  stroke-dashoffset: 101;
  animation: draw-ring 0.5s ease-out 0.05s forwards;
}
.check-mark {
  fill: none;
  stroke: currentColor;
  stroke-width: 3;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-dasharray: 30;
  stroke-dashoffset: 30;
  animation: draw-mark 0.4s cubic-bezier(0.65, 0, 0.45, 1) 0.32s forwards;
}
.check {
  animation: pop-icon 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) both;
}

/* Confetti burst, anchored to the icon, fanning outward once on mount. */
.confetti {
  position: absolute;
  left: 32px;
  top: 50%;
  width: 0;
  height: 0;
  pointer-events: none;
}
.piece {
  position: absolute;
  left: 0;
  top: 0;
  width: var(--size);
  height: var(--size);
  background: var(--color);
  opacity: 0;
  transform: translate(0, 0) scale(0.2);
  animation: burst 0.9s cubic-bezier(0.2, 0.7, 0.3, 1) var(--delay) forwards;
}
.piece.round {
  border-radius: 50%;
}

@keyframes burst {
  0% {
    opacity: 0;
    transform: translate(0, 0) scale(0.2) rotate(0deg);
  }
  18% {
    opacity: 1;
  }
  100% {
    opacity: 0;
    transform: translate(var(--x), var(--y)) scale(1) rotate(var(--spin));
  }
}
@keyframes pop-icon {
  0% {
    transform: scale(0);
  }
  100% {
    transform: scale(1);
  }
}
@keyframes draw-ring {
  to {
    stroke-dashoffset: 0;
  }
}
@keyframes draw-mark {
  to {
    stroke-dashoffset: 0;
  }
}

/* ── Error — clear ────────────────────────────────────────────── */
.fb--error {
  border: 1px solid rgb(var(--rust-rgb) / 0.5);
  border-left: 3px solid var(--danger);
  background: rgb(var(--rust-rgb) / 0.1);
  animation: shake 0.4s ease-in-out both;
}
.fb--error .fb-icon {
  color: var(--danger);
}
.fb--error .fb-title {
  color: var(--error-text);
}
.fb--error .fb-msg {
  color: var(--error-text);
  opacity: 0.85;
}

@keyframes shake {
  0%,
  100% {
    transform: translateX(0);
  }
  20% {
    transform: translateX(-7px);
  }
  40% {
    transform: translateX(6px);
  }
  60% {
    transform: translateX(-4px);
  }
  80% {
    transform: translateX(2px);
  }
}

/* Entrance for both banners. */
.feedback-enter-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.feedback-enter-from {
  opacity: 0;
  transform: translateY(-6px);
}

@media (prefers-reduced-motion: reduce) {
  .fb--error,
  .check,
  .check-ring,
  .check-mark,
  .piece,
  .feedback-enter-active {
    animation: none !important;
    transition: none !important;
  }
  .check-ring,
  .check-mark {
    stroke-dashoffset: 0;
  }
  .confetti {
    display: none;
  }
}
</style>
