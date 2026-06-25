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
      // The shareable attendee URL. Any signed-in @id5.io user.
      path: '/events/:slug',
      component: () => import('./views/AttendeeFormView.vue'),
      props: true,
      meta: { requiresAuth: true },
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
  try {
    await auth.ensureFreshUser()
  } catch (e: unknown) {
    if (!auth.token) {
      return { path: '/login', query: { redirect: to.fullPath } }
    }
    return { path: '/error', query: { code: String(errStatus(e) ?? 503) } }
  }
  if (!auth.token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.meta.requiresAdmin && !auth.user?.isAdmin) {
    return { path: '/error', query: { code: '403' } }
  }
})
