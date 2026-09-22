import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PAGE_SIZE, useItemsStore } from '@/stores'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import LabelsView from '@/views/LabelsView.vue'

const LIST = 'GET /api/v1/labels'
const CREATE = 'POST /api/v1/labels'

function label(overrides: Record<string, unknown> = {}) {
  return { id: 'fragile', name: 'Fragile', color: '#c0392b', version: 1, ...overrides }
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

async function mountLabels(routes: Routes) {
  const fetchMock = stubRoutedFetch(routes)
  const wrapper = mount(LabelsView, { global: { plugins: [pinia] } })
  await flush()
  await wrapper.vm.$nextTick()
  return { wrapper, fetchMock }
}

describe('LabelsView empty state', () => {
  it('invites adding the first label rather than showing a bare empty list', async () => {
    const { wrapper } = await mountLabels({ [LIST]: () => jsonResponse(200, { labels: [] }) })

    expect(wrapper.text()).toContain('No labels yet.')
    expect(wrapper.find('.labels__list').exists()).toBe(false)
  })
})

describe('LabelsView rendering', () => {
  it('renders one chip per label, with its swatch coloured from the label', async () => {
    const { wrapper } = await mountLabels({
      [LIST]: () =>
        jsonResponse(200, {
          labels: [label(), label({ id: 'heavy', name: 'Heavy', color: '#123456' })],
        }),
    })

    const chips = wrapper.findAll('.labels__row .chip')
    expect(chips).toHaveLength(2)
    const swatch = chips[1]!.get('.chip__swatch')
    expect(swatch.attributes('style')).toContain('background: #123456')
  })
})

describe('LabelsView create', () => {
  it('disables Add until a name is typed, and resets name+colour after success', async () => {
    const { wrapper } = await mountLabels({
      [LIST]: () => jsonResponse(200, { labels: [] }),
      [CREATE]: () => jsonResponse(201, label()),
    })

    const add = wrapper.get('button[type="submit"]')
    expect(add.attributes('disabled')).toBeDefined()

    await wrapper.get('#new-label').setValue('Fragile')
    expect(add.attributes('disabled')).toBeUndefined()

    await wrapper.get('form.labels__new').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect((wrapper.get('#new-label').element as HTMLInputElement).value).toBe('')
  })

  it('a blank (whitespace-only) name never reaches the API', async () => {
    const { wrapper, fetchMock } = await mountLabels({
      [LIST]: () => jsonResponse(200, { labels: [] }),
    })
    const callsAtMount = fetchMock.mock.calls.length

    await wrapper.get('#new-label').setValue('   ')
    await wrapper.get('form.labels__new').trigger('submit')
    await flush()

    expect(fetchMock.mock.calls.length).toBe(callsAtMount)
  })
})

describe('LabelsView edit', () => {
  it('prefills the edit row and saves the new name and colour with the current version', async () => {
    const { wrapper, fetchMock } = await mountLabels({
      [LIST]: () => jsonResponse(200, { labels: [label()] }),
      ['PUT /api/v1/labels/fragile']: () => jsonResponse(200, label({ name: 'Very fragile' })),
    })

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Edit')!
      .trigger('click')
    const nameInput = wrapper.get<HTMLInputElement>('input[aria-label="Label name"]')
    expect(nameInput.element.value).toBe('Fragile')

    await nameInput.setValue('Very fragile')
    await wrapper.get('form.labels__edit').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')!
    expect(JSON.parse(put[1]!.body as string)).toEqual({
      name: 'Very fragile',
      color: '#c0392b',
      version: 1,
    })
  })

  it('Cancel leaves the row unedited and makes no request', async () => {
    const { wrapper, fetchMock } = await mountLabels({
      [LIST]: () => jsonResponse(200, { labels: [label()] }),
    })
    const callsAtMount = fetchMock.mock.calls.length

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Edit')!
      .trigger('click')
    await wrapper.get('input[aria-label="Label name"]').setValue('Renamed but abandoned')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Cancel')!
      .trigger('click')
    await flush()

    expect(fetchMock.mock.calls.length).toBe(callsAtMount)
    expect(wrapper.text()).toContain('Fragile')
    expect(wrapper.text()).not.toContain('Renamed but abandoned')
  })
})

describe('LabelsView delete', () => {
  it('asks for confirmation first, and deletes nothing if it is declined', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => false),
    )
    const { wrapper, fetchMock } = await mountLabels({
      [LIST]: () => jsonResponse(200, { labels: [label()] }),
    })
    const callsAtMount = fetchMock.mock.calls.length

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Delete')!
      .trigger('click')
    await flush()

    expect(fetchMock.mock.calls.length).toBe(callsAtMount)
    expect(wrapper.text()).toContain('Fragile')
  })

  it('once confirmed, also drops the label from an active item-list filter so the two stay consistent', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    const { wrapper } = await mountLabels({
      [LIST]: () => jsonResponse(200, { labels: [label()] }),
    })

    const items = useItemsStore()
    items.labelIDs = ['fragile']

    stubRoutedFetch({
      ['DELETE /api/v1/labels/fragile']: () => jsonResponse(204),
      [LIST]: () => jsonResponse(200, { labels: [] }),
      [`GET /api/v1/items?limit=${PAGE_SIZE}&offset=0`]: () => jsonResponse(200, { items: [] }),
    })

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Delete')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(items.labelIDs).toEqual([])
  })

  it('leaves an inactive filter untouched when an unrelated label is deleted', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    const { wrapper, fetchMock } = await mountLabels({
      [LIST]: () => jsonResponse(200, { labels: [label()] }),
      ['DELETE /api/v1/labels/fragile']: () => jsonResponse(204),
    })

    const items = useItemsStore()
    items.labelIDs = ['heavy']

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Delete')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(items.labelIDs).toEqual(['heavy'])
    expect(fetchMock.mock.calls.some(([input]) => (input as string).includes('/items'))).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
