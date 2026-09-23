import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '@/router'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import JoinView from '@/views/JoinView.vue'

const ME = 'GET /api/v1/auth/me'
const REDEEM = 'POST /api/v1/invites/redeem'
const LOGIN = 'POST /api/v1/auth/login'

function baseRoutes(overrides: Routes = {}): Routes {
  return {
    [ME]: () => jsonResponse(401, { title: 'Unauthorized', status: 401 }),
    ...overrides,
  }
}

let pinia: ReturnType<typeof createPinia>
let router: ReturnType<typeof createAppRouter>

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  router = createAppRouter()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountJoin(path: string, routes: Routes) {
  const fetchMock = stubRoutedFetch(routes)
  await router.push(path)
  await router.isReady()
  const wrapper = mount(JoinView, { global: { plugins: [router, pinia] } })
  return { wrapper, fetchMock }
}

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function submit(
  wrapper: Awaited<ReturnType<typeof mountJoin>>['wrapper'],
  overrides: { token?: string; username?: string; password?: string } = {},
) {
  if (overrides.token !== undefined) await wrapper.get('#token').setValue(overrides.token)
  await wrapper.get('#username').setValue(overrides.username ?? 'newmember')
  await wrapper.get('#password').setValue(overrides.password ?? 'correct-horse-battery-staple')
  await wrapper.get('form').trigger('submit')
  await flush()
  await wrapper.vm.$nextTick()
}

describe('JoinView token prefill', () => {
  it('prefills the token field from ?token= in the URL', async () => {
    const { wrapper } = await mountJoin('/join?token=tok-abc-123', baseRoutes())

    expect((wrapper.get('#token').element as HTMLInputElement).value).toBe('tok-abc-123')
  })

  it('leaves the token field empty when the query carries none', async () => {
    const { wrapper } = await mountJoin('/join', baseRoutes())

    expect((wrapper.get('#token').element as HTMLInputElement).value).toBe('')
  })
})

describe('JoinView redeem request', () => {
  it('sends the token, username and password exactly as entered', async () => {
    const { wrapper, fetchMock } = await mountJoin(
      '/join?token=tok-abc-123',
      baseRoutes({
        [REDEEM]: () =>
          jsonResponse(201, { group_id: 'grp-1', user_id: 'usr-1', username: 'newmember' }),
        [LOGIN]: () =>
          jsonResponse(200, {
            group_id: 'grp-1',
            user_id: 'usr-1',
            username: 'newmember',
            role: 'member',
          }),
      }),
    )

    await submit(wrapper, { username: 'newmember', password: 'correct-horse-battery-staple' })

    const call = fetchMock.mock.calls.find(([input]) => input === '/api/v1/invites/redeem')
    expect(call).toBeDefined()
    const init = call![1] as RequestInit
    expect(JSON.parse(init.body as string)).toEqual({
      token: 'tok-abc-123',
      username: 'newmember',
      password: 'correct-horse-battery-staple',
    })
  })

  it('logs the new member in and lands on /items after a successful redeem', async () => {
    const { wrapper } = await mountJoin(
      '/join?token=tok-abc-123',
      baseRoutes({
        [REDEEM]: () =>
          jsonResponse(201, { group_id: 'grp-1', user_id: 'usr-1', username: 'newmember' }),
        [LOGIN]: () =>
          jsonResponse(200, {
            group_id: 'grp-1',
            user_id: 'usr-1',
            username: 'newmember',
            role: 'member',
          }),
      }),
    )

    await submit(wrapper)

    expect(router.currentRoute.value.path).toBe('/items')
  })
})

describe('JoinView redeem errors', () => {
  it('explains a 404 as an invalid/expired invite rather than showing "Not Found"', async () => {
    const { wrapper } = await mountJoin(
      '/join?token=tok-abc-123',
      baseRoutes({
        [REDEEM]: () => jsonResponse(404, { title: 'Not Found', status: 404 }),
      }),
    )

    await submit(wrapper)

    const alert = wrapper.get('[role="alert"]').text()
    expect(alert).toContain('no longer valid')
    expect(alert).not.toBe('Not Found')
  })

  it('explains a 409 as a taken username rather than showing "Conflict"', async () => {
    const { wrapper } = await mountJoin(
      '/join?token=tok-abc-123',
      baseRoutes({
        [REDEEM]: () => jsonResponse(409, { title: 'Conflict', status: 409 }),
      }),
    )

    await submit(wrapper)

    const alert = wrapper.get('[role="alert"]').text()
    expect(alert).toContain('already taken')
    expect(alert).not.toBe('Conflict')
  })

  it('passes a 400 validation detail through verbatim', async () => {
    const { wrapper } = await mountJoin(
      '/join?token=tok-abc-123',
      baseRoutes({
        [REDEEM]: () =>
          jsonResponse(400, {
            title: 'Bad Request',
            detail: 'invite: password is invalid',
            status: 400,
          }),
      }),
    )

    await submit(wrapper)

    expect(wrapper.get('[role="alert"]').text()).toContain('invite: password is invalid')
  })

  it('does not redeem the invite twice or route away after a failed attempt', async () => {
    const { wrapper, fetchMock } = await mountJoin(
      '/join?token=tok-abc-123',
      baseRoutes({
        [REDEEM]: () => jsonResponse(404, { title: 'Not Found', status: 404 }),
      }),
    )

    await submit(wrapper)

    expect(router.currentRoute.value.path).toBe('/join')
    expect(
      fetchMock.mock.calls.filter(([input]) => input === '/api/v1/invites/redeem'),
    ).toHaveLength(1)
  })
})
