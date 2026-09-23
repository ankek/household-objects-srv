import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useSessionStore } from '@/stores'
import { jsonResponse, stubRoutedFetch } from '@/test-support/routedFetch'

const ME = 'GET /api/v1/auth/me'

beforeEach(() => {
  setActivePinia(createPinia())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('session store boot()', () => {
  it('sets user from GET /auth/me and makes isOwner true for an owner', async () => {
    stubRoutedFetch({
      [ME]: () =>
        jsonResponse(200, { group_id: 'g', user_id: 'u', username: 'anton', role: 'owner' }),
    })
    const session = useSessionStore()

    await session.boot()

    expect(session.status).toBe('authenticated')
    expect(session.user).toEqual({ group_id: 'g', user_id: 'u', username: 'anton', role: 'owner' })
    expect(session.isOwner).toBe(true)
  })

  it('leaves isOwner false for a member', async () => {
    stubRoutedFetch({
      [ME]: () =>
        jsonResponse(200, { group_id: 'g', user_id: 'u', username: 'jamie', role: 'member' }),
    })
    const session = useSessionStore()

    await session.boot()

    expect(session.status).toBe('authenticated')
    expect(session.isOwner).toBe(false)
  })

  it('resolves to anonymous with user null on a 401 (no cookie, or an expired one)', async () => {
    stubRoutedFetch({
      [ME]: () => jsonResponse(401, { title: 'Unauthorized', status: 401 }),
    })
    const session = useSessionStore()

    await session.boot()

    expect(session.status).toBe('anonymous')
    expect(session.user).toBeNull()
    expect(session.isOwner).toBe(false)
  })

  it('resolves to anonymous with user null on any other failure (server down, bad proxy)', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockRejectedValue(new TypeError('network error')))
    const session = useSessionStore()

    await session.boot()

    expect(session.status).toBe('anonymous')
    expect(session.user).toBeNull()
  })

  it('only probes the server once, however many times it is called', async () => {
    const fetchMock = stubRoutedFetch({
      [ME]: () =>
        jsonResponse(200, { group_id: 'g', user_id: 'u', username: 'anton', role: 'owner' }),
    })
    const session = useSessionStore()

    await session.boot()
    await session.boot()

    expect(fetchMock.mock.calls.filter(([input]) => input === '/api/v1/auth/me')).toHaveLength(1)
  })
})
