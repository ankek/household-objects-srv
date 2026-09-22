import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import LocationsView from '@/views/LocationsView.vue'

const TREE = 'GET /api/v1/locations/tree'
const FLAT = 'GET /api/v1/locations'
const CREATE = 'POST /api/v1/locations'

function treeNode(overrides: Record<string, unknown> = {}) {
  return {
    id: 'garage',
    name: 'Garage',
    version: 1,
    item_count: 0,
    total_item_count: 0,
    children: [],
    ...overrides,
  }
}

function flatLocation(overrides: Record<string, unknown> = {}) {
  return { id: 'garage', name: 'Garage', version: 1, ...overrides }
}

function nestedFixture(): Routes {
  const shelfTree = treeNode({ id: 'shelf', name: 'Shelf', parent_id: 'garage' })
  const shedTree = treeNode({ id: 'shed', name: 'Shed' })
  return {
    [TREE]: () => jsonResponse(200, { tree: [treeNode({ children: [shelfTree] }), shedTree] }),
    [FLAT]: () =>
      jsonResponse(200, {
        locations: [
          flatLocation(),
          flatLocation({ id: 'shelf', name: 'Shelf', parent_id: 'garage' }),
          flatLocation({ id: 'shed', name: 'Shed' }),
        ],
      }),
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

async function mountLocations(routes: Routes) {
  const fetchMock = stubRoutedFetch(routes)
  const wrapper = mount(LocationsView, { global: { plugins: [pinia] } })
  await flush()
  await wrapper.vm.$nextTick()
  return { wrapper, fetchMock }
}

describe('LocationsView empty state', () => {
  it('invites adding the first location rather than showing a bare empty list', async () => {
    const { wrapper } = await mountLocations({
      [TREE]: () => jsonResponse(200, { tree: [] }),
      [FLAT]: () => jsonResponse(200, { locations: [] }),
    })

    expect(wrapper.text()).toContain('No locations yet.')
    expect(wrapper.find('.locations__list').exists()).toBe(false)
  })
})

describe('LocationsView recursive/flattened rendering', () => {
  it('indents a nested location under its parent in the flattened list', async () => {
    const { wrapper } = await mountLocations(nestedFixture())

    const names = wrapper.findAll('.locations__name').map((n) => n.element.textContent)
    expect(names).toEqual(['Garage', '  Shelf', 'Shed'])
  })
})

describe('LocationsView create', () => {
  it('disables Add until a name is typed, and resets the field after a successful create', async () => {
    const { wrapper } = await mountLocations({
      [TREE]: () => jsonResponse(200, { tree: [] }),
      [FLAT]: () => jsonResponse(200, { locations: [] }),
      [CREATE]: () => jsonResponse(201, flatLocation({ id: 'shed', name: 'Shed' })),
    })

    const add = wrapper.get('button[type="submit"]')
    expect(add.attributes('disabled')).toBeDefined()

    await wrapper.get('#new-location').setValue('Shed')
    expect(add.attributes('disabled')).toBeUndefined()

    await wrapper.get('form.locations__new').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect((wrapper.get('#new-location').element as HTMLInputElement).value).toBe('')
  })

  it('surfaces a genuine failure as an alert instead of silently doing nothing', async () => {
    const { wrapper } = await mountLocations({
      [TREE]: () => jsonResponse(200, { tree: [] }),
      [FLAT]: () => jsonResponse(200, { locations: [] }),
      [CREATE]: () => jsonResponse(500, { title: 'Internal Server Error', status: 500 }),
    })

    await wrapper.get('#new-location').setValue('Shed')
    await wrapper.get('form.locations__new').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[role="alert"]').text()).toContain('Internal Server Error')
  })
})

describe('LocationsView rename/move', () => {
  it('prefills the edit row and sends the new name plus the current version on save', async () => {
    const { wrapper, fetchMock } = await mountLocations({
      ...nestedFixture(),
      ['PUT /api/v1/locations/shelf']: () =>
        jsonResponse(200, flatLocation({ id: 'shelf', name: 'Top Shelf' })),
    })

    const shelfRow = wrapper.findAll('.locations__row')[1]!
    await shelfRow.get('button').trigger('click')

    const nameInput = wrapper.get<HTMLInputElement>('input[aria-label="Location name"]')
    expect(nameInput.element.value).toBe('Shelf')

    await nameInput.setValue('Top Shelf')
    await wrapper.get('form.locations__edit').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')!
    const body = JSON.parse(put[1]!.body as string)
    expect(body).toEqual({ name: 'Top Shelf', parent_id: 'garage', version: 1 })
  })
})

describe('LocationsView delete guard (G2)', () => {
  it('turns a 409 into a question with the reported counts, and Cancel deletes nothing', async () => {
    const { wrapper, fetchMock } = await mountLocations({
      ...nestedFixture(),
      ['DELETE /api/v1/locations/garage']: () =>
        jsonResponse(409, { title: 'Conflict', child_count: 1, item_count: 4 }),
    })

    const garageRow = wrapper.findAll('.locations__row')[0]!
    const deleteButton = garageRow.findAll('button').find((b) => b.text() === 'Delete')!
    await deleteButton.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.text()).toContain('1 sub-location')
    expect(dialog.text()).toContain('4 items')

    const callsBeforeCancel = fetchMock.mock.calls.length
    await wrapper
      .get('.locations__guard')
      .findAll('button')
      .find((b) => b.text() === 'Cancel')!
      .trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(fetchMock.mock.calls.length).toBe(callsBeforeCancel)
  })

  it('answering the guard with a reassignment target sends reassign_to=<id>, not a detach', async () => {
    const { wrapper, fetchMock } = await mountLocations({
      ...nestedFixture(),
      ['DELETE /api/v1/locations/garage']: () =>
        jsonResponse(409, { title: 'Conflict', child_count: 1, item_count: 4 }),
      ['DELETE /api/v1/locations/garage?reassign_to=shed']: () => jsonResponse(204),
    })

    const garageRow = wrapper.findAll('.locations__row')[0]!
    await garageRow
      .findAll('button')
      .find((b) => b.text() === 'Delete')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const select = wrapper.get('#reassign')
    await select.setValue('shed')
    await wrapper
      .get('.locations__guard')
      .findAll('button')
      .find((b) => b.text() === 'Delete and move')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(
      fetchMock.mock.calls.some(
        ([input, init]) =>
          input === '/api/v1/locations/garage?reassign_to=shed' && init?.method === 'DELETE',
      ),
    ).toBe(true)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('excludes the location being deleted and its own descendants from the reassignment choices', async () => {
    const { wrapper } = await mountLocations({
      ...nestedFixture(),
      ['DELETE /api/v1/locations/garage']: () =>
        jsonResponse(409, { title: 'Conflict', child_count: 1, item_count: 4 }),
    })

    const garageRow = wrapper.findAll('.locations__row')[0]!
    await garageRow
      .findAll('button')
      .find((b) => b.text() === 'Delete')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const options = wrapper
      .get('#reassign')
      .findAll('option')
      .map((o) => o.text())
    expect(options).toEqual(['Nowhere — make them top-level / unplaced', 'Shed'])
  })
})
