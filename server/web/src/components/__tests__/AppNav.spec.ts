import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AppNav from '@/components/AppNav.vue'
import { createAppRouter } from '@/router'
import { useSessionStore } from '@/stores'

let pinia: ReturnType<typeof createPinia>
let router: ReturnType<typeof createAppRouter>

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  router = createAppRouter()
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ sessions: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    ),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountNav() {
  await router.push('/items')
  await router.isReady()
  return mount(AppNav, { global: { plugins: [router, pinia] } })
}

describe('AppNav', () => {
  it('shows no navigation to an anonymous visitor', async () => {
    const wrapper = await mountNav()
    useSessionStore().status = 'anonymous'
    await wrapper.vm.$nextTick()

    expect(wrapper.get('.app-nav__brand').text()).toBe('HHO')
    expect(wrapper.findAll('nav a')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('Sign out')
  })

  it('renders one link per section once authenticated', async () => {
    const wrapper = await mountNav()
    useSessionStore().status = 'authenticated'
    await wrapper.vm.$nextTick()

    const links = wrapper.findAll('nav a')
    expect(links.map((link) => link.text())).toEqual([
      'Items',
      'Locations',
      'Labels',
      'Print labels',
      'Settings',
    ])
    expect(links.map((link) => link.attributes('href'))).toEqual([
      '/items',
      '/locations',
      '/labels',
      '/print-labels',
      '/settings',
    ])
    expect(links.map((link) => router.resolve(link.attributes('href')!).name)).toEqual([
      'items',
      'locations',
      'labels',
      'print-labels',
      'settings',
    ])
  })

  it('marks the active route so the nav reflects navigation', async () => {
    const wrapper = await mountNav()
    useSessionStore().status = 'authenticated'
    await wrapper.vm.$nextTick()

    const [items, locations] = wrapper.findAll('nav a')
    expect(items!.classes()).toContain('router-link-active')
    expect(locations!.classes()).not.toContain('router-link-active')

    await router.push('/locations')
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('nav a')[1]!.classes()).toContain('router-link-active')
  })

  it('renders the username only when the session knows it', async () => {
    const wrapper = await mountNav()
    const session = useSessionStore()

    session.status = 'authenticated'
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.app-nav__user').exists()).toBe(false)
    expect(wrapper.text()).toContain('Sign out')

    session.user = { group_id: 'g', user_id: 'u', username: 'anton', role: 'owner' }
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.app-nav__user').text()).toBe('anton')
  })
})
