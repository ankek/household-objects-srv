import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import WarrantyBlock from '@/components/item/WarrantyBlock.vue'
import { jsonResponse, stubRoutedFetch } from '@/test-support/routedFetch'

const ITEM_ID = 'item-1'
const GET = `GET /api/v1/items/${ITEM_ID}/warranty`
const PUT = `PUT /api/v1/items/${ITEM_ID}/warranty`

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function mountBlock() {
  const wrapper = mount(WarrantyBlock, { props: { itemId: ITEM_ID } })
  await flush()
  return wrapper
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('WarrantyBlock, no data on file', () => {
  it('shows only the add affordance, no open form (FR-011)', async () => {
    stubRoutedFetch({ [GET]: () => jsonResponse(404, { title: 'Not Found', status: 404 }) })
    const wrapper = await mountBlock()

    const block = wrapper.get('[data-testid="warranty-block"]')
    expect(block.text()).toContain('+ Add warranty details')
    expect(wrapper.find('#warranty-provider').exists()).toBe(false)
  })
})

describe('WarrantyBlock, data on file', () => {
  function existing(overrides: Record<string, unknown> = {}) {
    return {
      item_id: ITEM_ID,
      provider: 'Acme',
      holder: 'Household',
      is_lifetime: false,
      expires_on: '2030-01-01',
      created_at: 0,
      updated_at: 0,
      version: 1,
      ...overrides,
    }
  }

  it('shows a compact summary, collapsed, expandable into the edit form', async () => {
    stubRoutedFetch({ [GET]: () => jsonResponse(200, existing()) })
    const wrapper = await mountBlock()

    expect(wrapper.find('#warranty-provider').exists()).toBe(false)
    const summary = wrapper.get('.detail-block__summary-text').text()
    expect(summary).toContain('Acme')
    expect(summary).toContain('2030-01-01')

    const editButton = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    expect(editButton).toBeDefined()
    await editButton!.trigger('click')

    const providerInput = wrapper.get<HTMLInputElement>('#warranty-provider')
    expect(providerInput.element.value).toBe('Acme')
  })

  it('surfaces a 409 on save as a conflict, without discarding the edit or clobbering the data', async () => {
    const fetchMock = stubRoutedFetch({
      [GET]: () => jsonResponse(200, existing()),
      [PUT]: () => jsonResponse(409, { title: 'Conflict', status: 409 }),
    })
    const wrapper = await mountBlock()

    const editButton = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editButton!.trigger('click')

    const providerInput = wrapper.get<HTMLInputElement>('#warranty-provider')
    await providerInput.setValue('Beta Corp')
    await wrapper.get('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[role="alert"]').text()).toContain('Someone else changed')
    expect(wrapper.get<HTMLInputElement>('#warranty-provider').element.value).toBe('Beta Corp')
    expect(wrapper.find('form').exists()).toBe(true)
    expect(
      fetchMock.mock.calls.some(([, init]) => (init?.method ?? 'GET').toUpperCase() === 'POST'),
    ).toBe(false)
  })
})
