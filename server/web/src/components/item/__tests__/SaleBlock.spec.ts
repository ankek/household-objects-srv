import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SaleBlock from '@/components/item/SaleBlock.vue'
import { jsonResponse, stubRoutedFetch } from '@/test-support/routedFetch'

const ITEM_ID = 'item-1'
const GET = `GET /api/v1/items/${ITEM_ID}/sale`
const PUT = `PUT /api/v1/items/${ITEM_ID}/sale`

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function mountBlock() {
  const wrapper = mount(SaleBlock, { props: { itemId: ITEM_ID } })
  await flush()
  return wrapper
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('SaleBlock, no data on file', () => {
  it('shows only the add affordance, no open form (FR-011)', async () => {
    stubRoutedFetch({ [GET]: () => jsonResponse(404, { title: 'Not Found', status: 404 }) })
    const wrapper = await mountBlock()

    const block = wrapper.get('[data-testid="sale-block"]')
    expect(block.text()).toContain('+ Add sale details')
    expect(wrapper.find('#sale-buyer').exists()).toBe(false)
  })
})

describe('SaleBlock, data on file', () => {
  function existing(overrides: Record<string, unknown> = {}) {
    return {
      item_id: ITEM_ID,
      buyer_name: 'Chris',
      sold_on: '2029-06-01',
      sale_price_minor: 2500,
      created_at: 0,
      updated_at: 0,
      version: 1,
      ...overrides,
    }
  }

  it('shows a compact summary, collapsed, expandable into the edit form', async () => {
    stubRoutedFetch({ [GET]: () => jsonResponse(200, existing()) })
    const wrapper = await mountBlock()

    expect(wrapper.find('#sale-buyer').exists()).toBe(false)
    const summary = wrapper.get('.detail-block__summary-text').text()
    expect(summary).toContain('25.00')
    expect(summary).toContain('Chris')

    const editButton = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editButton!.trigger('click')

    const buyerInput = wrapper.get<HTMLInputElement>('#sale-buyer')
    expect(buyerInput.element.value).toBe('Chris')
  })

  it('surfaces a 409 on save as a conflict, without discarding the edit or clobbering the data', async () => {
    stubRoutedFetch({
      [GET]: () => jsonResponse(200, existing()),
      [PUT]: () => jsonResponse(409, { title: 'Conflict', status: 409 }),
    })
    const wrapper = await mountBlock()

    const editButton = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editButton!.trigger('click')

    const buyerInput = wrapper.get<HTMLInputElement>('#sale-buyer')
    await buyerInput.setValue('Someone Else')
    await wrapper.get('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[role="alert"]').text()).toContain('Someone else changed')
    expect(wrapper.get<HTMLInputElement>('#sale-buyer').element.value).toBe('Someone Else')
    expect(wrapper.find('form').exists()).toBe(true)
  })
})
