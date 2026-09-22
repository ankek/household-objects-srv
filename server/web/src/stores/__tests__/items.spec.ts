import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PAGE_SIZE, useItemsStore } from '@/stores'

function stubFetch(items: unknown[] = []) {
  const fetchMock = vi.fn<typeof fetch>().mockImplementation(() =>
    Promise.resolve(
      new Response(JSON.stringify({ items }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input
  if (input instanceof URL) return input.href
  return input.url
}

function lastURL(fetchMock: ReturnType<typeof stubFetch>): string {
  return urlOf(fetchMock.mock.calls.at(-1)![0])
}

beforeEach(() => {
  setActivePinia(createPinia())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('items store filters', () => {
  it('omits every filter that is not set', async () => {
    const fetchMock = stubFetch()
    await useItemsStore().refresh()

    const url = lastURL(fetchMock)
    expect(url).toContain('/api/v1/items?')
    expect(url).toContain(`limit=${PAGE_SIZE}`)
    expect(url).toContain('offset=0')
    expect(url).not.toContain('q=')
    expect(url).not.toContain('location_id=')
    expect(url).not.toContain('label_id=')
  })

  it('repeats label_id once per label, which the server reads as AND', async () => {
    const fetchMock = stubFetch()
    const items = useItemsStore()

    await items.applyFilters({ labelIDs: ['a', 'b', 'c'] })

    const url = new URL(lastURL(fetchMock), 'http://localhost')
    expect(url.searchParams.getAll('label_id')).toEqual(['a', 'b', 'c'])
  })

  it('sends descendants only alongside a location', async () => {
    const fetchMock = stubFetch()
    const items = useItemsStore()

    await items.refresh()
    expect(lastURL(fetchMock)).not.toContain('descendants')

    await items.applyFilters({ locationID: 'garage' })
    expect(lastURL(fetchMock)).toContain('location_id=garage')
    expect(lastURL(fetchMock)).toContain('descendants=true')
  })

  it('returns to the first page whenever a filter changes', async () => {
    const fetchMock = stubFetch(Array.from({ length: PAGE_SIZE }, (_, i) => ({ id: String(i) })))
    const items = useItemsStore()

    await items.refresh()
    await items.nextPage()
    expect(items.offset).toBe(PAGE_SIZE)

    await items.applyFilters({ q: 'drill' })
    expect(items.offset).toBe(0)
    expect(lastURL(fetchMock)).toContain('offset=0')
  })

  it('distinguishes clearing a location from leaving it alone', async () => {
    const fetchMock = stubFetch()
    const items = useItemsStore()

    await items.applyFilters({ locationID: 'garage' })
    expect(items.locationID).toBe('garage')

    await items.applyFilters({ q: 'drill' })
    expect(items.locationID).toBe('garage')

    await items.applyFilters({ locationID: undefined })
    expect(items.locationID).toBeUndefined()
    expect(lastURL(fetchMock)).not.toContain('location_id')
  })

  it('toggles a label into and back out of the filter set', async () => {
    stubFetch()
    const items = useItemsStore()

    await items.toggleLabel('fragile')
    expect(items.labelIDs).toEqual(['fragile'])
    await items.toggleLabel('heavy')
    expect(items.labelIDs).toEqual(['fragile', 'heavy'])
    await items.toggleLabel('fragile')
    expect(items.labelIDs).toEqual(['heavy'])
  })

  it('offers a next page only when the current one is full', async () => {
    stubFetch(Array.from({ length: PAGE_SIZE - 1 }, (_, i) => ({ id: String(i) })))
    const items = useItemsStore()

    await items.refresh()
    expect(items.hasNextPage).toBe(false)
    expect(items.hasPreviousPage).toBe(false)
  })
})
