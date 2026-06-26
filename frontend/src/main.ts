import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { applyTheme, getStoredTheme } from './theme'
import '@fontsource-variable/fraunces/wght.css'
import '@fontsource-variable/fraunces/wght-italic.css'
import '@fontsource-variable/jetbrains-mono/wght.css'
import './styles.css'

// Apply the persisted theme before mount. The inline script in index.html has
// usually already set this from localStorage (avoiding any flash); this makes
// the app robust if that script didn't run and normalizes any stale value.
applyTheme(getStoredTheme())

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
