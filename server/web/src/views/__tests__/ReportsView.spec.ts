import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PAGE_SIZE } from '@/stores'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import ReportsView from '@/views/ReportsView.vue'

const VALUATION_LOCATION = 'GET /api/v1/reports/valuation?group_by=location'
const VALUATION_LABEL = 'GET /api/v1/reports/valuation?group_by=label'
const WARRANTY_30 = 'GET /api/v1/reports/warranty-expiring?within_days=30'
const ITEM_COUNT = 'GET /api/v1/reports/item-count-by-location'
const LOC_TREE = 'GET /api/v1/locations/tree'
const LOC_FLAT = 'GET /api/v1/locations'
const LABELS = 'GET /api/v1/labels'
const ITEMS_UNFILTERED = `GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`

function location(overrides: Record<string, unknown> = {}) {
  return {
    id: 'loc-1',
    created_at: 0,
    updated_at: 0,
    version: 1,
    name: 'Garage',
    ...overrides,
  }
}

function label(overrides: Record<string, unknown> = {}) {
  return {
    id: 'lab-1',
    created_at: 0,
    updated_at: 0,
    version: 1,
    name: 'Fragile',
    color: '#ff0000',
    ...overrides,
  }
}

function item(overrides: Record<string, unknown> = {}) {
  return {
    id: 'item-1',
    created_at: 0,
    updated_at: 0,
    version: 1,
    name: 'Drill',
    description: '',
    location_id: '',
    quantity: 1,
    ...overrides,
  }
}

function baseRoutes(overrides: Routes = {}): Routes {
  return {
    [VALUATION_LOCATION]: () => jsonResponse(200, { rows: [] }),
    [WARRANTY_30]: () => jsonResponse(200, { rows: [] }),
    [ITEM_COUNT]: () => jsonResponse(200, { rows: [] }),
    [LOC_TREE]: () => jsonResponse(200, { tree: [] }),
    [LOC_FLAT]: () => jsonResponse(200, { locations: [] }),
    [LABELS]: () => jsonResponse(200, { labels: [] }),
    ...overrides,
  }
}

let pinia: ReturnType<typeof createPinia>

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function mountReports(routes: Routes) {
  const fetchMock = stubRoutedFetch(routes)
  const wrapper = mount(ReportsView, { global: { plugins: [pinia] } })
  await flush()
  await wrapper.vm.$nextTick()
  return { wrapper, fetchMock }
}

function selectTab(wrapper: Awaited<ReturnType<typeof mountReports>>['wrapper'], label: string) {
  return wrapper
    .findAll('[role="tab"]')
    .find((t) => t.text() === label)!
    .trigger('click')
}

describe('ReportsView tabs', () => {
  it('renders all five report tabs, with Valuation active by default', async () => {
    const { wrapper } = await mountReports(baseRoutes())

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs.map((t) => t.text())).toEqual([
      'Valuation',
      'Warranty expiring',
      'Purchases',
      'Item count by location',
      'Bill of materials',
    ])
    expect(wrapper.get('[role="tab"][aria-selected="true"]').text()).toBe('Valuation')
  })

  it('fetches the JSON side of every report and the reference lists on mount, never the CSV side', async () => {
    const { fetchMock } = await mountReports(baseRoutes())

    const urls = fetchMock.mock.calls.map(([input]) => input)
    expect(urls).toContain('/api/v1/reports/valuation?group_by=location')
    expect(urls).toContain('/api/v1/reports/warranty-expiring?within_days=30')
    expect(urls).toContain('/api/v1/reports/item-count-by-location')
    expect(urls.some((u) => (u as string).includes('format=csv'))).toBe(false)
  })
})

describe('ReportsView error and empty states', () => {
  it('shows an alert on the Valuation tab when its report fails to load on mount', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        [VALUATION_LOCATION]: () => jsonResponse(500, { title: 'Internal Server Error' }),
      }),
    )

    expect(wrapper.get('.alert').text()).toContain('Internal Server Error')
    expect(wrapper.find('.reports__table').exists()).toBe(false)
  })

  it('renders the empty-state message on the Valuation tab when there are no rows', async () => {
    const { wrapper } = await mountReports(baseRoutes())

    expect(wrapper.text()).toContain('No data for this report yet.')
    expect(wrapper.find('.reports__table').exists()).toBe(false)
  })

  it('shows an alert (and no crash) when running the Purchases report fails', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        'GET /api/v1/reports/purchases?from=2026-01-01&to=2026-12-31': () =>
          jsonResponse(500, { title: 'Internal Server Error' }),
      }),
    )
    await selectTab(wrapper, 'Purchases')

    await wrapper.get('#purchases-from').setValue('2026-01-01')
    await wrapper.get('#purchases-to').setValue('2026-12-31')
    await wrapper.find('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('.alert').text()).toContain('Internal Server Error')
    expect(wrapper.find('.reports__table').exists()).toBe(false)
  })

  it('renders the empty-state message on Purchases only after Run comes back with no matches', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        'GET /api/v1/reports/purchases?from=2026-01-01&to=2026-12-31': () =>
          jsonResponse(200, { rows: [] }),
      }),
    )
    await selectTab(wrapper, 'Purchases')

    expect(wrapper.text()).not.toContain('Nothing purchased in this range.')

    await wrapper.get('#purchases-from').setValue('2026-01-01')
    await wrapper.get('#purchases-to').setValue('2026-12-31')
    await wrapper.find('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Nothing purchased in this range.')
  })
})

describe('ReportsView valuation (FR-060)', () => {
  it('renders the Unassigned bucket for an empty group_key, in location mode, without crashing', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        [VALUATION_LOCATION]: () =>
          jsonResponse(200, {
            rows: [
              { group_key: 'loc-1', group_label: 'Garage', item_count: 3, total_value_minor: 1000 },
              { group_key: '', group_label: '', item_count: 2, total_value_minor: 0 },
            ],
          }),
      }),
    )

    const rows = wrapper.findAll('.reports__table tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('Garage')
    expect(rows[1]!.text()).toContain('Unassigned')
  })

  it('a download link points at the JSON route with &format=csv appended, matching the current group_by', async () => {
    const { wrapper } = await mountReports(baseRoutes())

    const csvLink = wrapper
      .findAll('a')
      .find(
        (a) => a.text() === 'Download CSV' && a.attributes('href')?.includes('/reports/valuation'),
      )!
    expect(csvLink.attributes('href')).toBe(
      '/api/v1/reports/valuation?group_by=location&format=csv',
    )
  })

  it('switching to group_by=label refetches JSON and updates the CSV link, never conflating the two modes', async () => {
    const { wrapper, fetchMock } = await mountReports(
      baseRoutes({
        [VALUATION_LABEL]: () =>
          jsonResponse(200, {
            rows: [
              { group_key: 'lab-1', group_label: 'Fragile', item_count: 1, total_value_minor: 500 },
            ],
          }),
      }),
    )

    await wrapper.get('#valuation-group-by').setValue('label')
    await flush()
    await wrapper.vm.$nextTick()

    expect(
      fetchMock.mock.calls.some(([input]) => input === '/api/v1/reports/valuation?group_by=label'),
    ).toBe(true)
    const csvLink = wrapper
      .findAll('a')
      .find(
        (a) => a.text() === 'Download CSV' && a.attributes('href')?.includes('/reports/valuation'),
      )!
    expect(csvLink.attributes('href')).toBe('/api/v1/reports/valuation?group_by=label&format=csv')
    expect(wrapper.get('.reports__table').text()).toContain('Fragile')
  })
})

describe('ReportsView warranty expiring (FR-060, within_days)', () => {
  it('defaults to within_days=30 on load, sent explicitly rather than omitted', async () => {
    const { fetchMock } = await mountReports(baseRoutes())

    expect(
      fetchMock.mock.calls.some(
        ([input]) => input === '/api/v1/reports/warranty-expiring?within_days=30',
      ),
    ).toBe(true)
  })

  it('sending within_days=0 reaches the server as a literal 0, not treated as "no limit"', async () => {
    const { wrapper, fetchMock } = await mountReports(
      baseRoutes({
        'GET /api/v1/reports/warranty-expiring?within_days=0': () =>
          jsonResponse(200, {
            rows: [
              { item_id: 'i1', item_name: 'Drill', expires_on: '2026-09-17', days_remaining: 0 },
            ],
          }),
      }),
    )
    await selectTab(wrapper, 'Warranty expiring')

    await wrapper.get('#within-days').setValue(0)
    await wrapper.get('#within-days').trigger('change')
    await wrapper.find('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(
      fetchMock.mock.calls.some(
        ([input]) => input === '/api/v1/reports/warranty-expiring?within_days=0',
      ),
    ).toBe(true)
    const csvLink = wrapper
      .findAll('a')
      .find(
        (a) => a.text() === 'Download CSV' && a.attributes('href')?.includes('/warranty-expiring'),
      )!
    expect(csvLink.attributes('href')).toBe(
      '/api/v1/reports/warranty-expiring?within_days=0&format=csv',
    )
  })
})

describe('ReportsView purchases (FR-060, from/to both required)', () => {
  it('disables the CSV link and does not fetch until both dates are filled', async () => {
    const { wrapper, fetchMock } = await mountReports(baseRoutes())
    await selectTab(wrapper, 'Purchases')

    const csvLink = wrapper
      .findAll('a')
      .find((a) => a.text() === 'Download CSV' && a.attributes('href') === undefined)
    expect(csvLink).toBeTruthy()
    expect(
      fetchMock.mock.calls.some(([input]) => (input as string).includes('/reports/purchases')),
    ).toBe(false)
  })

  it('running the report with a valid inclusive range fetches JSON and builds the matching CSV link', async () => {
    const { wrapper, fetchMock } = await mountReports(
      baseRoutes({
        'GET /api/v1/reports/purchases?from=2026-01-01&to=2026-12-31': () =>
          jsonResponse(200, {
            rows: [
              {
                item_id: 'i1',
                item_name: 'Drill',
                purchased_on: '2026-03-14',
                vendor: 'Acme',
                purchase_price_minor: 8999,
              },
            ],
          }),
      }),
    )
    await selectTab(wrapper, 'Purchases')

    await wrapper.get('#purchases-from').setValue('2026-01-01')
    await wrapper.get('#purchases-to').setValue('2026-12-31')
    await wrapper.find('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(
      fetchMock.mock.calls.some(
        ([input]) => input === '/api/v1/reports/purchases?from=2026-01-01&to=2026-12-31',
      ),
    ).toBe(true)
    const csvLink = wrapper
      .findAll('a')
      .find(
        (a) => a.text() === 'Download CSV' && a.attributes('href')?.includes('/reports/purchases'),
      )!
    expect(csvLink.attributes('href')).toBe(
      '/api/v1/reports/purchases?from=2026-01-01&to=2026-12-31&format=csv',
    )
    expect(wrapper.text()).toContain('Acme')
  })
})

describe('ReportsView item count by location (FR-060, rolled-up count)', () => {
  it('shows the server-provided rolled-up item_count without recomputing or relabeling it as direct-only', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        [ITEM_COUNT]: () =>
          jsonResponse(200, {
            rows: [{ location_id: 'loc-1', location_name: 'Garage', item_count: 12 }],
          }),
      }),
    )
    await selectTab(wrapper, 'Item count by location')

    expect(wrapper.get('.reports__table').text()).toContain('Garage')
    expect(wrapper.get('.reports__table').text()).toContain('12')
    const csvLink = wrapper
      .findAll('a')
      .find(
        (a) =>
          a.text() === 'Download CSV' &&
          a.attributes('href')?.includes('/reports/item-count-by-location'),
      )!
    expect(csvLink.attributes('href')).toBe('/api/v1/reports/item-count-by-location?format=csv')
  })
})

describe('ReportsView Bill of Materials (FR-063, D2.6)', () => {
  it('exports a header-only selection (no params at all) until a mode is chosen', async () => {
    const { wrapper } = await mountReports(baseRoutes())
    await selectTab(wrapper, 'Bill of materials')

    const csvLink = wrapper.get('a.reports__csv-link.btn--primary')
    expect(csvLink.attributes('href')).toBe('/api/v1/export/bom')
    expect(wrapper.text()).toContain('No selection mode chosen')
  })

  it('by-location mode selects multiple locations as repeated location_id params', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        [LOC_FLAT]: () =>
          jsonResponse(200, {
            locations: [
              location({ id: 'loc-1', name: 'Garage' }),
              location({ id: 'loc-2', name: 'Attic' }),
            ],
          }),
      }),
    )
    await selectTab(wrapper, 'Bill of materials')

    await wrapper.findAll('input[type="radio"]')[0]!.trigger('change')
    await wrapper.vm.$nextTick()

    const checkboxes = wrapper.findAll('.reports__bom-list input[type="checkbox"]')
    await checkboxes[0]!.setValue(true)
    await checkboxes[1]!.setValue(true)

    const csvLink = wrapper.get('a.reports__csv-link.btn--primary')
    expect(csvLink.attributes('href')).toBe(
      '/api/v1/export/bom?location_id=loc-1&location_id=loc-2',
    )
  })

  it('switching selection mode clears the previous mode picks, keeping the three mutually exclusive', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        [LOC_FLAT]: () =>
          jsonResponse(200, { locations: [location({ id: 'loc-1', name: 'Garage' })] }),
        [LABELS]: () => jsonResponse(200, { labels: [label({ id: 'lab-1', name: 'Fragile' })] }),
      }),
    )
    await selectTab(wrapper, 'Bill of materials')

    const radios = wrapper.findAll('input[type="radio"]')
    await radios[0]!.trigger('change')
    await wrapper.vm.$nextTick()
    await wrapper.get('.reports__bom-list input[type="checkbox"]').setValue(true)
    expect(wrapper.get('a.reports__csv-link.btn--primary').attributes('href')).toBe(
      '/api/v1/export/bom?location_id=loc-1',
    )

    await radios[1]!.trigger('change')
    await wrapper.vm.$nextTick()

    expect(wrapper.get('a.reports__csv-link.btn--primary').attributes('href')).toBe(
      '/api/v1/export/bom',
    )
  })

  it('by-label mode is OR: selecting two labels sends both as repeated label_id params, never intersected client-side', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        [LABELS]: () =>
          jsonResponse(200, {
            labels: [
              label({ id: 'lab-1', name: 'Fragile' }),
              label({ id: 'lab-2', name: 'Lend out' }),
            ],
          }),
      }),
    )
    await selectTab(wrapper, 'Bill of materials')

    const radios = wrapper.findAll('input[type="radio"]')
    await radios[1]!.trigger('change')
    await wrapper.vm.$nextTick()

    const chips = wrapper.findAll('.reports__bom-labelchip')
    await chips[0]!.trigger('click')
    await chips[1]!.trigger('click')

    const csvLink = wrapper.get('a.reports__csv-link.btn--primary')
    expect(csvLink.attributes('href')).toBe('/api/v1/export/bom?label_id=lab-1&label_id=lab-2')
    expect(wrapper.text()).toContain('Selecting items with any of these 2 labels')
  })

  it('the label picker here is independent of the AND-semantics label filter used elsewhere (ItemsView, print-labels)', async () => {
    const { wrapper, fetchMock } = await mountReports(
      baseRoutes({
        [LABELS]: () => jsonResponse(200, { labels: [label({ id: 'lab-1', name: 'Fragile' })] }),
      }),
    )
    await selectTab(wrapper, 'Bill of materials')
    const radios = wrapper.findAll('input[type="radio"]')
    await radios[1]!.trigger('change')
    await wrapper.vm.$nextTick()
    await wrapper.get('.reports__bom-labelchip').trigger('click')

    expect(
      fetchMock.mock.calls.some(
        ([input]) =>
          (input as string).includes('/items?') && (input as string).includes('label_id'),
      ),
    ).toBe(false)
  })

  it('by-item mode loads the item picker and selects specific items as repeated item_id params', async () => {
    const { wrapper } = await mountReports(
      baseRoutes({
        [ITEMS_UNFILTERED]: () =>
          jsonResponse(200, {
            items: [item({ id: 'item-1', name: 'Drill' }), item({ id: 'item-2', name: 'Saw' })],
          }),
      }),
    )
    await selectTab(wrapper, 'Bill of materials')

    const radios = wrapper.findAll('input[type="radio"]')
    await radios[2]!.trigger('change')
    await flush()
    await wrapper.vm.$nextTick()

    const checkboxes = wrapper.findAll('.reports__bom-list input[type="checkbox"]')
    expect(checkboxes).toHaveLength(2)
    await checkboxes[1]!.setValue(true)

    const csvLink = wrapper.get('a.reports__csv-link.btn--primary')
    expect(csvLink.attributes('href')).toBe('/api/v1/export/bom?item_id=item-2')
  })
})
