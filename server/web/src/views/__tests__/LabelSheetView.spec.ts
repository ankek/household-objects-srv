import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PAGE_SIZE } from '@/stores'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import LabelSheetView from '@/views/LabelSheetView.vue'

const TREE = 'GET /api/v1/locations/tree'
const FLAT = 'GET /api/v1/locations'
const LABELS = 'GET /api/v1/labels'
const BATCH = 'POST /api/v1/labels/qr/batch'

function item(overrides: Record<string, unknown> = {}) {
  return {
    id: 'item-1',
    created_at: 0,
    updated_at: 0,
    version: 1,
    name: 'Item',
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

async function mountSheet(routes: Routes) {
  const fetchMock = stubRoutedFetch(routes)
  const wrapper = mount(LabelSheetView, { global: { plugins: [pinia] } })
  await flush()
  await wrapper.vm.$nextTick()
  return { wrapper, fetchMock }
}

async function toggleRow(wrapper: Awaited<ReturnType<typeof mountSheet>>['wrapper'], name: string) {
  const row = wrapper
    .findAll('.print-labels__row')
    .find((r) => r.find('.print-labels__row-name').text() === name)!
  await row.get('input[type="checkbox"]').setValue(true)
}

function selectionSummary(wrapper: Awaited<ReturnType<typeof mountSheet>>['wrapper']): string {
  return wrapper.get('[role="status"].muted').text()
}

describe('LabelSheetView item selection', () => {
  it('lists items with a checkbox each and keeps a running selected count', async () => {
    const { wrapper } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, {
            items: [item({ id: 'a', name: 'Drill' }), item({ id: 'b', name: 'Tape' })],
          }),
      }),
    )

    expect(selectionSummary(wrapper)).toContain('0 selected')

    await toggleRow(wrapper, 'Drill')
    expect(selectionSummary(wrapper)).toContain('1 selected')

    await toggleRow(wrapper, 'Tape')
    expect(selectionSummary(wrapper)).toContain('2 selected')
  })

  it('Clear selection empties the count and drops any already-generated sheet', async () => {
    const { wrapper } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [item({ id: 'a', name: 'Drill' })] }),
        [BATCH]: () =>
          jsonResponse(200, {
            items: [
              {
                id: 'a',
                short_code: 'AAA111',
                name: 'Drill',
                qr_svg: '<svg data-test="qr-a"></svg>',
              },
            ],
          }),
      }),
    )

    await toggleRow(wrapper, 'Drill')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('.sheet__cell')).toHaveLength(1)

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Clear selection')!
      .trigger('click')
    await wrapper.vm.$nextTick()

    expect(selectionSummary(wrapper)).toContain('0 selected')
    expect(wrapper.find('.sheet').exists()).toBe(false)
    expect(wrapper.find('.sheet__cell').exists()).toBe(false)
  })
})

describe('LabelSheetView sheet generation', () => {
  it('renders one grid cell per resolved item, wired to its own returned QR markup, for a 30-item 5160 sheet', async () => {
    const thirty = Array.from({ length: 30 }, (_, i) => item({ id: `i${i}`, name: `Item ${i}` }))
    const batchItems = thirty.map((it) => ({
      id: it.id,
      short_code: `CODE${it.id}`,
      name: it.name,
      qr_svg: `<svg data-test="qr-${it.id}"></svg>`,
    }))

    const { wrapper, fetchMock } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: thirty }),
        [BATCH]: () => jsonResponse(200, { items: batchItems }),
      }),
    )

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Select page')!
      .trigger('click')
    expect(selectionSummary(wrapper)).toContain('30 selected')

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const batchCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')!
    expect(JSON.parse(batchCall[1]!.body as string)).toEqual({
      item_ids: thirty.map((it) => it.id),
    })

    const cells = wrapper.findAll('.sheet__cell')
    expect(cells).toHaveLength(30)
    for (const [index, entry] of batchItems.entries()) {
      const cell = cells[index]!
      expect(cell.text()).toContain(entry.name)
      expect(cell.html()).toContain(`qr-${entry.id}`)
    }
    expect(wrapper.findAll('.sheet')).toHaveLength(1)
  })

  it('re-chunks an already-generated sheet across preset changes without a second request', async () => {
    const thirty = Array.from({ length: 30 }, (_, i) => item({ id: `i${i}`, name: `Item ${i}` }))
    const batchItems = thirty.map((it) => ({
      id: it.id,
      short_code: `CODE${it.id}`,
      name: it.name,
      qr_svg: '<svg></svg>',
    }))

    const { wrapper, fetchMock } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: thirty }),
        [BATCH]: () => jsonResponse(200, { items: batchItems }),
      }),
    )

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Select page')!
      .trigger('click')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('.sheet')).toHaveLength(1)
    const callsAfterGenerate = fetchMock.mock.calls.length

    await wrapper.get('#label-preset').setValue('avery-5163')
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('.sheet')).toHaveLength(3)
    expect(fetchMock.mock.calls.length).toBe(callsAfterGenerate)
  })

  it('an omitted id is dropped, never shifted onto the wrong label: the count and the cells both say so', async () => {
    const three = [
      item({ id: 'a', name: 'Drill' }),
      item({ id: 'b', name: 'Tape' }),
      item({ id: 'c', name: 'Level' }),
    ]
    const { wrapper, fetchMock } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: three }),
        [BATCH]: () =>
          jsonResponse(200, {
            items: [
              { id: 'a', short_code: 'AAA', name: 'Drill', qr_svg: '<svg data-test="qr-a"></svg>' },
              { id: 'c', short_code: 'CCC', name: 'Level', qr_svg: '<svg data-test="qr-c"></svg>' },
            ],
          }),
      }),
    )

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Select page')!
      .trigger('click')
    expect(selectionSummary(wrapper)).toContain('3 selected')

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const batchCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')!
    expect(JSON.parse(batchCall[1]!.body as string)).toEqual({ item_ids: ['a', 'b', 'c'] })

    expect(wrapper.text()).toContain('1 selected item could not be printed')

    const cells = wrapper.findAll('.sheet__cell')
    expect(cells).toHaveLength(2)
    expect(cells[0]!.text()).toContain('Drill')
    expect(cells[0]!.html()).toContain('qr-a')
    expect(cells[1]!.text()).toContain('Level')
    expect(cells[1]!.html()).toContain('qr-c')
    expect(wrapper.get('.print-labels__preview').text()).not.toContain('Tape')
  })

  it('Generate sheet stays disabled with nothing selected, and enables once something is', async () => {
    const { wrapper } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [item({ id: 'a', name: 'Drill' })] }),
      }),
    )

    const generate = () => wrapper.findAll('button').find((b) => b.text() === 'Generate sheet')!
    expect(generate().attributes('disabled')).toBeDefined()

    await toggleRow(wrapper, 'Drill')
    expect(generate().attributes('disabled')).toBeUndefined()
  })
})

describe('LabelSheetView preset geometry', () => {
  it('sets the sheet grid geometry from the chosen preset, and updates it when the preset changes', async () => {
    const batchItems = [{ id: 'a', short_code: 'AAA', name: 'Drill', qr_svg: '<svg></svg>' }]
    const { wrapper } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [item({ id: 'a', name: 'Drill' })] }),
        [BATCH]: () => jsonResponse(200, { items: batchItems }),
      }),
    )

    await toggleRow(wrapper, 'Drill')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const style5160 = wrapper.get('.sheet').attributes('style')!
    expect(style5160).toContain('--sheet-columns: 3')
    expect(style5160).toContain('--sheet-rows: 10')
    expect(style5160).toContain('--cell-w: 2.625in')
    expect(style5160).toContain('--cell-h: 1in')

    await wrapper.get('#label-preset').setValue('avery-5163')
    await wrapper.vm.$nextTick()

    const style5163 = wrapper.get('.sheet').attributes('style')!
    expect(style5163).toContain('--sheet-columns: 2')
    expect(style5163).toContain('--sheet-rows: 5')
    expect(style5163).toContain('--cell-w: 4in')
    expect(style5163).toContain('--cell-h: 2in')
  })

  it('chunks one item over the 30-per-sheet 5160 boundary into a second, single-item sheet', async () => {
    const thirtyOne = Array.from({ length: 31 }, (_, i) => item({ id: `i${i}`, name: `Item ${i}` }))
    const batchItems = thirtyOne.map((it) => ({
      id: it.id,
      short_code: `CODE${it.id}`,
      name: it.name,
      qr_svg: `<svg data-test="qr-${it.id}"></svg>`,
    }))

    const { wrapper } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: thirtyOne }),
        [BATCH]: () => jsonResponse(200, { items: batchItems }),
      }),
    )

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Select page')!
      .trigger('click')
    expect(selectionSummary(wrapper)).toContain('31 selected')

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const sheets = wrapper.findAll('.sheet')
    expect(sheets).toHaveLength(2)
    expect(sheets[0]!.findAll('.sheet__cell')).toHaveLength(30)
    expect(sheets[1]!.findAll('.sheet__cell')).toHaveLength(1)
    expect(sheets[1]!.find('.sheet__cell').text()).toContain('Item 30')
  })
})

describe('LabelSheetView batch request count and print', () => {
  it('issues exactly one batch request per Generate click', async () => {
    const { wrapper, fetchMock } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [item({ id: 'a', name: 'Drill' })] }),
        [BATCH]: () =>
          jsonResponse(200, {
            items: [{ id: 'a', short_code: 'AAA', name: 'Drill', qr_svg: '<svg></svg>' }],
          }),
      }),
    )

    await toggleRow(wrapper, 'Drill')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const batchCalls = fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')
    expect(batchCalls).toHaveLength(1)
  })

  it('Print stays disabled until a sheet exists, then calls window.print', async () => {
    const printSpy = vi.fn()
    vi.stubGlobal('print', printSpy)

    const { wrapper } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [item({ id: 'a', name: 'Drill' })] }),
        [BATCH]: () =>
          jsonResponse(200, {
            items: [{ id: 'a', short_code: 'AAA', name: 'Drill', qr_svg: '<svg></svg>' }],
          }),
      }),
    )

    const printButton = () => wrapper.findAll('button').find((b) => b.text() === 'Print')!
    expect(printButton().attributes('disabled')).toBeDefined()

    await toggleRow(wrapper, 'Drill')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(printButton().attributes('disabled')).toBeUndefined()
    await printButton().trigger('click')
    expect(printSpy).toHaveBeenCalledTimes(1)
  })

  it('shows the server error and no sheet when Generate sheet fails', async () => {
    const { wrapper } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: [item({ id: 'a', name: 'Drill' })] }),
        [BATCH]: () => jsonResponse(500, { title: 'Internal Server Error' }),
      }),
    )

    await toggleRow(wrapper, 'Drill')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Generate sheet')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('.alert').text()).toContain('Internal Server Error')
    expect(wrapper.find('.sheet').exists()).toBe(false)
  })
})

describe('LabelSheetView batch-size ceiling', () => {
  it('disables Generate and explains why once the selection passes 200, the batch endpoint’s own ceiling', async () => {
    const page = (offset: number, count: number) =>
      Array.from({ length: count }, (_, i) =>
        item({ id: `p${offset}-${i}`, name: `Item ${offset}-${i}` }),
      )

    const { wrapper, fetchMock } = await mountSheet(
      baseRoutes({
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () =>
          jsonResponse(200, { items: page(0, PAGE_SIZE) }),
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=${PAGE_SIZE}`]: () =>
          jsonResponse(200, { items: page(PAGE_SIZE, PAGE_SIZE) }),
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=${2 * PAGE_SIZE}`]: () =>
          jsonResponse(200, { items: page(2 * PAGE_SIZE, PAGE_SIZE) }),
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=${3 * PAGE_SIZE}`]: () =>
          jsonResponse(200, { items: page(3 * PAGE_SIZE, PAGE_SIZE) }),
        [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=${4 * PAGE_SIZE}`]: () =>
          jsonResponse(200, { items: page(4 * PAGE_SIZE, 1) }),
      }),
    )

    const selectPage = async () => {
      await wrapper
        .findAll('button')
        .find((b) => b.text() === 'Select page')!
        .trigger('click')
    }
    const next = async () => {
      await wrapper
        .findAll('button')
        .find((b) => b.text() === 'Next')!
        .trigger('click')
      await flush()
      await wrapper.vm.$nextTick()
    }

    await selectPage()
    await next()
    await selectPage()
    await next()
    await selectPage()
    await next()
    await selectPage()
    await next()
    await selectPage()

    expect(selectionSummary(wrapper)).toContain('201 selected')
    expect(wrapper.text()).toContain('over the 200-label limit')

    const generate = wrapper.findAll('button').find((b) => b.text() === 'Generate sheet')!
    expect(generate.attributes('disabled')).toBeDefined()

    const callsBeforeGenerateAttempt = fetchMock.mock.calls.length
    await generate.trigger('click')
    await flush()
    expect(fetchMock.mock.calls.length).toBe(callsBeforeGenerateAttempt)
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'POST')).toBe(false)
  })
})
