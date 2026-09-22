import {
  createRouter,
  createWebHistory,
  type NavigationGuard,
  type RouteRecordRaw,
} from 'vue-router'
import { useSessionStore } from '@/stores'
import ItemsView from '@/views/ItemsView.vue'

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/items' },
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { public: true },
  },
  { path: '/items', name: 'items', component: ItemsView },
  {
    path: '/items/new',
    name: 'item-new',
    component: () => import('@/views/ItemEditView.vue'),
  },
  {
    path: '/items/:id',
    name: 'item',
    props: true,
    component: () => import('@/views/ItemEditView.vue'),
  },
  {
    path: '/locations',
    name: 'locations',
    component: () => import('@/views/LocationsView.vue'),
  },
  { path: '/labels', name: 'labels', component: () => import('@/views/LabelsView.vue') },
  {
    path: '/print-labels',
    name: 'print-labels',
    component: () => import('@/views/LabelSheetView.vue'),
  },
  {
    path: '/settings',
    name: 'settings',
    component: () => import('@/views/SettingsView.vue'),
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/views/NotFoundView.vue'),
    meta: { public: true },
  },
]

const authGuard: NavigationGuard = async (to) => {
  const session = useSessionStore()
  await session.boot()

  if (to.meta.public === true) {
    if (to.name === 'login' && session.isAuthenticated) return { path: '/items' }
    return true
  }

  if (!session.isAuthenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  return true
}

export function createAppRouter() {
  const router = createRouter({
    history: createWebHistory(import.meta.env.BASE_URL),
    routes,
  })
  router.beforeEach(authGuard)
  return router
}

export const router = createAppRouter()
