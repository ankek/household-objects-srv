import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IdentificationsBlock from '@/components/item/IdentificationsBlock.vue'
import { jsonResponse, stubRoutedFetch } from '@/test-support/routedFetch'

const ITEM_ID = 'item-1'
const LIST = `GET /api/v1/items/${ITEM_ID}/identifications`
const CREATE = `POST /api/v1/items/${ITEM_ID}/identifications`
const ROW_ID = 'ident-1'
const UPDATE = `PUT /api/v1/items/${ITEM_ID}/identifications/${ROW_ID}`
const DELETE = `DELETE /api/v1/items/${ITEM_ID}/identifications/${ROW_ID}`

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

function row(overrides: Record<string, unknown> = {}) {
  return {
    id: ROW_ID,
    item_id: ITEM_ID,
    kind: 'serial',
    value: 'SN-0001',
    created_at: 0,
    updated_at: 0,
    version: 1,
    ...overrides,
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('IdentificationsBlock', () => {
  it('shows only the add affordance when there are no rows, and adds one', async () => {
    const created = row()
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { identifications: [] }),
      [CREATE]: () => jsonResponse(201, created),
    })
    const wrapper = mount(IdentificationsBlock, { props: { itemId: ITEM_ID } })
    await flush()

    expect(wrapper.text()).toContain('+ Add identification')
    expect(wrapper.findAll('li')).toHaveLength(0)

    await wrapper.get('button').trigger('click')
    await wrapper.get('input[aria-label="Value"]').setValue('SN-0001')
    await wrapper.get('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    const items = wrapper.findAll('li')
    expect(items).toHaveLength(1)
    expect(items[0]!.text()).toContain('SN-0001')
  })

  it('edits an existing row', async () => {
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { identifications: [row()] }),
      [UPDATE]: () => jsonResponse(200, row({ value: 'SN-0002', version: 2 })),
    })
    const wrapper = mount(IdentificationsBlock, { props: { itemId: ITEM_ID } })
    await flush()

    const editButton = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editButton!.trigger('click')
    await wrapper.get('input[aria-label="Value"]').setValue('SN-0002')
    await wrapper.get('form').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('li').text()).toContain('SN-0002')
  })

  it('removes a row', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { identifications: [row()] }),
      [DELETE]: () => jsonResponse(204),
    })
    const wrapper = mount(IdentificationsBlock, { props: { itemId: ITEM_ID } })
    await flush()
    expect(wrapper.findAll('li')).toHaveLength(1)

    const deleteButton = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteButton!.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('li')).toHaveLength(0)
    expect(wrapper.text()).toContain('+ Add identification')
  })
})
