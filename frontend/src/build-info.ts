// Frontend build metadata, baked in at build time by Vite's `define` (see
// vite.config.ts). These globals are replaced with string literals during the
// build, so there is no runtime cost and no network call.
import type { BuildInfo } from './types'

declare const __APP_VERSION__: string
declare const __GIT_COMMIT__: string
declare const __BUILD_TIME__: string

export const frontendBuildInfo: BuildInfo = {
  name: 'irl-planner-pro-frontend',
  version: typeof __APP_VERSION__ !== 'undefined' ? __APP_VERSION__ : 'dev',
  gitCommit: typeof __GIT_COMMIT__ !== 'undefined' ? __GIT_COMMIT__ : 'unknown',
  buildTime: typeof __BUILD_TIME__ !== 'undefined' ? __BUILD_TIME__ : 'unknown',
}
