import { ref } from 'vue'
import { api } from '../api'
import type { BackendBuildInfo } from '../types'
import { frontendBuildInfo } from '../build-info'

// The frontend version is baked in at build time (build-info.ts); the backend
// version is fetched once at runtime from /api/version. They can legitimately
// differ during a rolling deploy, so the footer shows both. The backend value
// is cached module-level so it is fetched at most once per page load even if
// load() is called from several places.
const backend = ref<BackendBuildInfo | null>(null)
let pending: Promise<BackendBuildInfo | null> | null = null

export function useBuildInfo() {
  function load() {
    if (backend.value || pending) return pending
    pending = api
      .version()
      .then((info) => {
        backend.value = info
        return info
      })
      .catch(() => null)
    return pending
  }
  return { frontend: frontendBuildInfo, backend, load }
}
