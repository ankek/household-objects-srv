import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useLocationsStore } from '@/stores'

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input
  if (input instanceof URL) return input.href
  return input.url
}

function stubFetch() {
  const fetchMock = vi.fn<typeof fetch>().mockImplementation((input) => {
    const url = urlOf(input)
    const body = url.includes('/locations/tree')
      ? { tree: [] }
      : url.includes('/locations')
        ? { locations: [] }
        : {}
    return Promise.resolve(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function deleteURL(fetchMock: ReturnType<typeof stubFetch>): string {
  const call = fetchMock.mock.calls.find(([, init]) => init?.method === 'DELETE')
  return urlOf(call![0])
}

beforeEach(() => {
  setActivePinia(createPinia())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('locations store delete guard', () => {
  it('omits reassign_to entirely when no reassignment was chosen', async () => {
    const fetchMock = stubFetch()
    await useLocationsStore().remove('garage')

    expect(deleteURL(fetchMock)).toBe('/api/v1/locations/garage')
  })

  it('sends a present-but-empty reassign_to to detach', async () => {
    const fetchMock = stubFetch()
    await useLocationsStore().remove('garage', '')

    expect(deleteURL(fetchMock)).toBe('/api/v1/locations/garage?reassign_to=')
  })

  it('sends the target id when reassigning', async () => {
    const fetchMock = stubFetch()
    await useLocationsStore().remove('garage', 'shed')

    expect(deleteURL(fetchMock)).toBe('/api/v1/locations/garage?reassign_to=shed')
  })
})
