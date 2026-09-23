import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '@/router'
import LoginView from '@/views/LoginView.vue'

let pinia: ReturnType<typeof createPinia>
let router: ReturnType<typeof createAppRouter>
function stubFetch(status: number, body: unknown) {
  const fetchMock = vi.fn<typeof fetch>().mockImplementation(() =>
    Promise.resolve(
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    ),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  router = createAppRouter()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountLogin() {
  const wrapper = mount(LoginView, { global: { plugins: [router, pinia] } })
  await router.isReady()
  return wrapper
}

async function selectMode(
  wrapper: Awaited<ReturnType<typeof mountLogin>>,
  label: string,
): Promise<void> {
  const button = wrapper.findAll('.login__mode').find((b) => b.text() === label)
  if (button === undefined) throw new Error(`no mode button labelled ${label}`)
  await button.trigger('click')
}

async function submit(wrapper: Awaited<ReturnType<typeof mountLogin>>) {
  await wrapper.get('#username').setValue('anton')
  await wrapper.get('#password').setValue('correct-horse-battery-staple')
  await wrapper.get('form').trigger('submit')
  await new Promise((resolve) => setTimeout(resolve, 0))
  await wrapper.vm.$nextTick()
}

describe('LoginView first-run registration', () => {
  it('explains a 409 as an existing account rather than showing "Conflict"', async () => {
    stubFetch(409, {
      type: 'urn:hho:problem:conflict',
      title: 'Conflict',
      status: 409,
    })
    const wrapper = await mountLogin()

    await selectMode(wrapper, 'Create the first account')
    await submit(wrapper)

    const alert = wrapper.get('[role="alert"]').text()
    expect(alert).toContain('already has an account')
    expect(alert).not.toBe('Conflict')
  })

  it('switches to sign-in and keeps the username after that conflict', async () => {
    stubFetch(409, { type: 'urn:hho:problem:conflict', title: 'Conflict', status: 409 })
    const wrapper = await mountLogin()

    await selectMode(wrapper, 'Create the first account')
    await submit(wrapper)

    const active = wrapper.get('.login__mode--on')
    expect(active.text()).toBe('Sign in')
    expect((wrapper.get('#username').element as HTMLInputElement).value).toBe('anton')
    expect((wrapper.get('#password').element as HTMLInputElement).value).toBe('')
  })

  it('still reports a genuine sign-in failure verbatim', async () => {
    stubFetch(401, {
      type: 'urn:hho:problem:unauthorized',
      title: 'Unauthorized',
      status: 401,
    })
    const wrapper = await mountLogin()

    await submit(wrapper)

    expect(wrapper.get('[role="alert"]').text()).toContain('Unauthorized')
  })
})

describe('LoginView invite entry point', () => {
  it('links to /join for someone who already holds an invite', async () => {
    stubFetch(200, { sessions: [] })
    const wrapper = await mountLogin()

    const link = wrapper.get('.login__join a')
    expect(link.attributes('href')).toBe('/join')
    expect(link.text()).toBe('Join a household')
  })
})

describe('LoginView mode discoverability', () => {
  it('offers both actions at equal weight from the first render', async () => {
    stubFetch(200, { sessions: [] })
    const wrapper = await mountLogin()

    const modes = wrapper.findAll('.login__mode')
    expect(modes.map((m) => m.text())).toEqual(['Sign in', 'Create the first account'])
  })

  it('clears a stale error when the mode changes', async () => {
    stubFetch(401, { type: 'urn:hho:problem:unauthorized', title: 'Unauthorized', status: 401 })
    const wrapper = await mountLogin()

    await submit(wrapper)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)

    await selectMode(wrapper, 'Create the first account')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
