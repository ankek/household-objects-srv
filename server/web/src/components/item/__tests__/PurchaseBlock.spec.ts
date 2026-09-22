import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PurchaseBlock from '@/components/item/PurchaseBlock.vue'
import { jsonResponse, stubRoutedFetch } from '@/test-support/routedFetch'

const ITEM_ID = 'item-1'
const GET = `GET /api/v1/items/${ITEM_ID}/purchase`
const PUT = `PUT /api/v1/items/${ITEM_ID}/purchase`

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function mountBlock() {
  const wrapper = mount(PurchaseBlock, { props: { itemId: ITEM_ID } })
  await flush()
  return wrapper
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('PurchaseBlock, no data on file', () => {
  it('shows only the add affordance, no open form (FR-011)', async () => {
    stubRoutedFetch({ [GET]: () => jsonResponse(404, { title: 'Not Found', status: 404 }) })
    const wrapper = await mountBlock()

    const block = wrapper.get('[data-testid="purchase-block"]')
    expect(block.text()).toContain('+ Add purchase details')
    expect(wrapper.find('#purchase-vendor').exists()).toBe(false)
  })
})

describe('PurchaseBlock, data on file', () => {
  function existing(overrides: Record<string, unknown> = {}) {
    return {
      item_id: ITEM_ID,
      vendor: 'Widget Co',
      purchased_on: '2028-03-15',
      purchase_price_minor: 4999,
      order_reference: 'ORD-42',
      created_at: 0,
      updated_at: 0,
      version: 1,
      ...overrides,
    }
  }

  it('shows a compact summary, collapsed, expandable into the edit form', async () => {
    stubRoutedFetch({ [GET]: () => jsonResponse(200, existing()) })
    const wrapper = await mountBlock()

    expect(wrapper.find('#purchase-vendor').exists()).toBe(false)
    const summary = wrapper.get('.detail-block__summary-text').text()
    expect(summary).toContain('49.99')
    expect(summary).toContain('Widget Co')

    const editButton = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editButton!.trigger('click')

    const vendorInput = wrapper.get<HTMLInputElement>('#purchase-vendor')
    expect(vendorInput.element.value).toBe('Widget Co')
  })

  it('surfaces a 409 on save as a conflict, without discarding the edit or clobbering the data', async () => {
    stubRoutedFetch({
      [GET]: () => jsonResponse(200, existing()),
      [PUT]: () => jsonResponse(409, { title: 'Conflict', status: 409 }),
    })
    const wrapper = await mountBlock()

    const editButton = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editButton!.trigger('click')

    const vendorInput = wrapper.get<HTMLInputElement>('#purchase-vendor')
    await vendorInput.setValue('Different Vendor')
    await wrapper.get('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[role="alert"]').text()).toContain('Someone else changed')
    expect(wrapper.get<HTMLInputElement>('#purchase-vendor').element.value).toBe('Different Vendor')
    expect(wrapper.find('form').exists()).toBe(true)
  })
})
