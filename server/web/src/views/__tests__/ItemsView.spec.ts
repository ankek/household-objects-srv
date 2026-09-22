import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '@/router'
import { PAGE_SIZE } from '@/stores'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import ItemsView from '@/views/ItemsView.vue'

const TREE = 'GET /api/v1/locations/tree'
const FLAT = 'GET /api/v1/locations'
const LABELS = 'GET /api/v1/labels'

function item(overrides: Record<string, unknown> = {}) {
  return {
    id: 'item-1',
    created_at: 0,
    updated_at: 0,
    version: 1,
    name: 'Cordless drill',
    description: '',
    location_id: '',
    quantity: 1,
    ...overrides,
  }
}

function baseRoutes(overrides: Routes = {}): Routes {
  return {
    [TREE]: () => jsonResponse(200, { tree: [] }),
    [FLAT]: () => jsonResponse(200, { locations: [] }),
    [LABELS]: () => jsonResponse(200, { labels: [] }),
    [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () => jsonResponse(200, { items: [] }),
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
  vi.useRealTimers()
})

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function mountItems(routes: Routes) {
  const fetchMock = stubRoutedFetch(routes)
  const wrapper = mount(ItemsView, { global: { plugins: [router, pinia] } })
  await flush()
  await wrapper.vm.$nextTick()
  return { wrapper, fetchMock }
}

describe('ItemsView empty states', () => {
  it('offers to add the first item when the inventory is empty and unfiltered', async () => {
    const { wrapper } = await mountItems(baseRoutes())

    expect(wrapper.text()).toContain('Nothing in the inventory yet.')
    expect(wrapper.find('a[href="/items/new"]').exists()).toBe(true)
    expect(wrapper.findAll('button').some((b) => b.text() === 'Clear')).toBe(false)
  })

  it('shows a distinct message, with its own Clear affordance, when filters match nothing', async () => {
    const { wrapper } = await mountItems(
      baseRoutes({
        [`GET /api/v1/items?q=zzz&limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [] }),
      }),
    )

    await wrapper.get('#item-search').setValue('zzz')
    await wrapper.get('form.inventory__search').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('No items match these filters.')
    expect(wrapper.text()).not.toContain('Nothing in the inventory yet.')
    expect(wrapper.findAll('button').some((b) => b.text() === 'Clear filters')).toBe(true)
  })
})

describe('ItemsView rendering an actual list', () => {
  it('shows each item’s location name, falls back to "Unplaced", and only badges quantity > 1', async () => {
    const { wrapper } = await mountItems(
      baseRoutes({
        [FLAT]: () =>
          jsonResponse(200, { locations: [{ id: 'garage', name: 'Garage', version: 1 }] }),
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, {
            items: [
              item({ id: 'a', name: 'Drill', location_id: 'garage', quantity: 3 }),
              item({ id: 'b', name: 'Tape', location_id: '', quantity: 1 }),
            ],
          }),
      }),
    )

    const rows = wrapper.findAll('.inventory__list li')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('Garage')
    expect(rows[0]!.text()).toContain('3×')
    expect(rows[1]!.text()).toContain('Unplaced')
    expect(rows[1]!.text()).not.toContain('×')
  })
})

describe('ItemsView search', () => {
  it('debounces rapid typing into exactly one request, 250ms after the last keystroke', async () => {
    const { wrapper, fetchMock } = await mountItems(
      baseRoutes({
        [`GET /api/v1/items?q=drill&limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [] }),
      }),
    )
    const callsAtMount = fetchMock.mock.calls.length

    vi.useFakeTimers()
    const search = wrapper.get('#item-search')
    await search.setValue('d')
    await search.setValue('dr')
    await search.setValue('drill')

    await vi.advanceTimersByTimeAsync(249)
    expect(fetchMock.mock.calls.length).toBe(callsAtMount)

    await vi.advanceTimersByTimeAsync(1)
    expect(fetchMock.mock.calls.length).toBe(callsAtMount + 1)
    expect(fetchMock.mock.calls.at(-1)![0]).toContain('q=drill')
  })

  it('submitting the search form searches immediately, bypassing the debounce', async () => {
    const { wrapper, fetchMock } = await mountItems(
      baseRoutes({
        [`GET /api/v1/items?q=drill&limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [] }),
      }),
    )
    const callsAtMount = fetchMock.mock.calls.length

    await wrapper.get('#item-search').setValue('drill')
    await wrapper.get('form.inventory__search').trigger('submit')
    await flush()

    expect(fetchMock.mock.calls.length).toBe(callsAtMount + 1)
    expect(fetchMock.mock.calls.at(-1)![0]).toContain('q=drill')
  })
})

describe('ItemsView label filter (G4: AND, not OR)', () => {
  const LABELS_DATA = {
    labels: [
      { id: 'fragile', name: 'Fragile', color: '#f00', version: 1 },
      { id: 'heavy', name: 'Heavy', color: '#00f', version: 1 },
    ],
  }

  it('toggles a label chip on click, marks it pressed, and refetches filtered', async () => {
    const { wrapper, fetchMock } = await mountItems(
      baseRoutes({
        [LABELS]: () => jsonResponse(200, LABELS_DATA),
        [`GET /api/v1/items?label_id=fragile&limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [] }),
      }),
    )

    const chip = wrapper.findAll('.inventory__label').find((b) => b.text().includes('Fragile'))!
    expect(chip.attributes('aria-pressed')).toBe('false')

    await chip.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(chip.attributes('aria-pressed')).toBe('true')
    expect(fetchMock.mock.calls.at(-1)![0]).toContain('label_id=fragile')
  })

  it('shows the "all N labels" hint only once more than one label is active', async () => {
    const { wrapper } = await mountItems(
      baseRoutes({
        [LABELS]: () => jsonResponse(200, LABELS_DATA),
        [`GET /api/v1/items?label_id=fragile&limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [] }),
        [`GET /api/v1/items?label_id=fragile&label_id=heavy&limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [] }),
      }),
    )

    const fragile = wrapper.findAll('.inventory__label').find((b) => b.text().includes('Fragile'))!
    await fragile.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).not.toContain('Showing items with all')

    const heavy = wrapper.findAll('.inventory__label').find((b) => b.text().includes('Heavy'))!
    await heavy.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('Showing items with all 2 labels.')
  })
})

describe('ItemsView Clear', () => {
  it('only offers Clear once a filter is active, and it empties the search box too', async () => {
    const { wrapper } = await mountItems(
      baseRoutes({
        [`GET /api/v1/items?q=drill&limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [item()] }),
      }),
    )

    await wrapper.get('#item-search').setValue('drill')
    await wrapper.get('form.inventory__search').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    const clear = wrapper.findAll('button').find((b) => b.text() === 'Clear')
    expect(clear).toBeDefined()

    await clear!.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect((wrapper.get('#item-search').element as HTMLInputElement).value).toBe('')
    expect(wrapper.findAll('button').some((b) => b.text() === 'Clear')).toBe(false)
  })
})

describe('ItemsView pagination', () => {
  it('offers Next only for a full page, and paging changes the offset it requests', async () => {
    const fullPage = Array.from({ length: PAGE_SIZE }, (_, i) =>
      item({ id: `i${i}`, name: `Item ${i}` }),
    )
    const { wrapper, fetchMock } = await mountItems(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: fullPage }),
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=${PAGE_SIZE}`]: () =>
          jsonResponse(200, { items: [item({ id: 'last', name: 'Last item' })] }),
      }),
    )

    const previous = () => wrapper.findAll('button').find((b) => b.text() === 'Previous')!
    const next = () => wrapper.findAll('button').find((b) => b.text() === 'Next')!

    expect(previous().attributes('disabled')).toBeDefined()
    expect(next().attributes('disabled')).toBeUndefined()

    await next().trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(fetchMock.mock.calls.at(-1)![0]).toContain(`offset=${PAGE_SIZE}`)
    expect(previous().attributes('disabled')).toBeUndefined()
    expect(next().attributes('disabled')).toBeDefined()

    await previous().trigger('click')
    await flush()
    await wrapper.vm.$nextTick()
    expect(fetchMock.mock.calls.at(-1)![0]).toContain('offset=0')
  })
})

describe('ItemsView location filter', () => {
  it('filters by the clicked location and reveals the descendants checkbox only once one is picked', async () => {
    const { wrapper, fetchMock } = await mountItems(
      baseRoutes({
        [TREE]: () =>
          jsonResponse(200, {
            tree: [
              {
                id: 'garage',
                name: 'Garage',
                version: 1,
                item_count: 0,
                total_item_count: 0,
                children: [],
              },
            ],
          }),
        [`GET /api/v1/items?location_id=garage&descendants=true&limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [] }),
      }),
    )

    expect(wrapper.find('.inventory__descendants').exists()).toBe(false)

    await wrapper.get('.tree__row').trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(fetchMock.mock.calls.at(-1)![0]).toContain('location_id=garage')
    expect(wrapper.find('.inventory__descendants').exists()).toBe(true)
  })
})
