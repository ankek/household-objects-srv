import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CustomFieldsBlock from '@/components/item/CustomFieldsBlock.vue'
import { jsonResponse, stubRoutedFetch } from '@/test-support/routedFetch'

const ITEM_ID = 'item-1'
const LIST = `GET /api/v1/items/${ITEM_ID}/custom-fields`
const DEFS = 'GET /api/v1/custom-field-defs'
const CREATE = `POST /api/v1/items/${ITEM_ID}/custom-fields`
const ROW_ID = 'cf-1'
const UPDATE = `PUT /api/v1/items/${ITEM_ID}/custom-fields/${ROW_ID}`
const DELETE = `DELETE /api/v1/items/${ITEM_ID}/custom-fields/${ROW_ID}`

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

function row(overrides: Record<string, unknown> = {}) {
  return {
    id: ROW_ID,
    item_id: ITEM_ID,
    name: 'Colour',
    field_type: 'text',
    text_value: 'Red',
    created_at: 0,
    updated_at: 0,
    version: 1,
    ...overrides,
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('CustomFieldsBlock', () => {
  it('shows only the add affordance with no rows, and adds an ad hoc text field', async () => {
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { custom_fields: [] }),
      [DEFS]: () => jsonResponse(200, { custom_field_defs: [] }),
      [CREATE]: () => jsonResponse(201, row()),
    })
    const wrapper = mount(CustomFieldsBlock, { props: { itemId: ITEM_ID } })
    await flush()

    expect(wrapper.text()).toContain('+ Add custom field')
    expect(wrapper.findAll('li')).toHaveLength(0)

    await wrapper.get('button').trigger('click')
    await wrapper.get('#custom-field-name').setValue('Colour')
    await wrapper.get('#custom-field-value').setValue('Red')
    await wrapper.get('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    const items = wrapper.findAll('li')
    expect(items).toHaveLength(1)
    expect(items[0]!.text()).toContain('Colour')
    expect(items[0]!.text()).toContain('Red')
  })

  it('edits the value of an existing row', async () => {
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { custom_fields: [row()] }),
      [DEFS]: () => jsonResponse(200, { custom_field_defs: [] }),
      [UPDATE]: () => jsonResponse(200, row({ text_value: 'Blue', version: 2 })),
    })
    const wrapper = mount(CustomFieldsBlock, { props: { itemId: ITEM_ID } })
    await flush()

    const editButton = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editButton!.trigger('click')
    await wrapper.get('input[aria-label="Value"]').setValue('Blue')
    await wrapper.get('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('li').text()).toContain('Blue')
  })

  it('removes a row', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { custom_fields: [row()] }),
      [DEFS]: () => jsonResponse(200, { custom_field_defs: [] }),
      [DELETE]: () => jsonResponse(204),
    })
    const wrapper = mount(CustomFieldsBlock, { props: { itemId: ITEM_ID } })
    await flush()
    expect(wrapper.findAll('li')).toHaveLength(1)

    const deleteButton = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteButton!.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('li')).toHaveLength(0)
    expect(wrapper.text()).toContain('+ Add custom field')
  })

  it('does not let a failed definitions fetch block the values already on the item', async () => {
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { custom_fields: [row()] }),
      [DEFS]: () => Promise.reject(new Error('network down')),
    })
    const wrapper = mount(CustomFieldsBlock, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('li').text()).toContain('Colour')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
