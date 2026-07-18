import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from './stores/auth'
import { errStatus, isJwtExpired } from './api'

declare module 'vue-router' {
  interface RouteMeta {
    requiresAuth?: boolean
    requiresAdmin?: boolean
    hideChrome?: boolean
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: () => import('./views/HomeView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/login',
      component: () => import('./views/LoginView.vue'),
      meta: { hideChrome: true },
    },
    {
      path: '/auth/callback',
      component: () => import('./views/OIDCCallbackView.vue'),
      meta: { hideChrome: true },
    },
    {
      // The shareable attendee URL. Any signed-in @oglimmer.com user.
      path: '/events/:slug',
      component: () => import('./views/AttendeeFormView.vue'),
      props: true,
      meta: { requiresAuth: true },
    },
    {
      // First-login confirm step. requiresAuth so the guard loads a fresh user;
      // hideChrome to keep the onboarding screen focused.
      path: '/welcome',
      component: () => import('./views/WelcomeView.vue'),
      meta: { requiresAuth: true, hideChrome: true },
    },
    {
      path: '/profile',
      component: () => import('./views/ProfileView.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/admin/users',
      component: () => import('./views/UsersView.vue'),
      meta: { requiresAuth: true, requiresAdmin: true },
    },
    {
      path: '/admin/events',
      component: () => import('./views/EventListView.vue'),
      meta: { requiresAuth: true, requiresAdmin: true },
    },
    {
      path: '/admin/events/new',
      component: () => import('./views/EventEditView.vue'),
      meta: { requiresAuth: true, requiresAdmin: true },
    },
    {
      path: '/admin/events/:id',
      component: () => import('./views/EventDashboardView.vue'),
      props: true,
      meta: { requiresAuth: true, requiresAdmin: true },
    },
    {
      path: '/admin/events/:id/edit',
      component: () => import('./views/EventEditView.vue'),
      props: true,
      meta: { requiresAuth: true, requiresAdmin: true },
    },
    {
      // Programmatic error target: router.push({ path: '/error', query: { code: 403 } }).
      path: '/error',
      component: () => import('./views/ErrorView.vue'),
      props: (route) => ({ code: Number(route.query.code) || 500 }),
    },
    {
      // Catch-all 404.
      path: '/:pathMatch(.*)*',
      component: () => import('./views/ErrorView.vue'),
      props: { code: 404 },
    },
  ],
})

router.beforeEach(async (to) => {
  if (!to.meta.requiresAuth) return
  const auth = useAuthStore()
  if (!auth.token || isJwtExpired(auth.token)) {
    auth.logout()
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  // Seed app-wide config (auth mode, default tz, people-team email) in parallel
  // with the freshness check — otherwise a user who lands straight on a guarded
  // page (bookmark, email link, OIDC redirect) never fetches /api/auth/config.
  // Best-effort: the store keeps sensible defaults, so a config failure must not
  // block navigation — only ensureFreshUser can.
  const configReady = auth.ensureMode().catch(() => {})
  try {
    await auth.ensureFreshUser()
  } catch (e: unknown) {
    if (!auth.token) {
      return { path: '/login', query: { redirect: to.fullPath } }
    }
    return { path: '/error', query: { code: String(errStatus(e) ?? 503) } }
  }
  await configReady
  if (!auth.token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  // First-login profile confirmation: a user who hasn't reviewed their
  // IdP-seeded name/allergies is sent to /welcome before anything else.
  if (auth.user && !auth.user.profileConfirmed && to.path !== '/welcome') {
    return { path: '/welcome', query: { redirect: to.fullPath } }
  }
  if (to.meta.requiresAdmin && !auth.user?.isAdmin) {
    return { path: '/error', query: { code: '403' } }
  }
})
