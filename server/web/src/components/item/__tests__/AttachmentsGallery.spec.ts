import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AttachmentsGallery from '@/components/item/AttachmentsGallery.vue'
import { jsonResponse, stubRoutedFetch } from '@/test-support/routedFetch'

const ITEM_ID = 'item-1'
const LIST = `GET /api/v1/items/${ITEM_ID}/attachments`
const CREATE = `POST /api/v1/items/${ITEM_ID}/attachments`

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

function attachment(overrides: Record<string, unknown> = {}) {
  return {
    id: 'att-1',
    item_id: ITEM_ID,
    category: 'general',
    original_filename: 'notes.txt',
    content_type: 'text/plain',
    size_bytes: 1234,
    sha256: 'deadbeef',
    created_at: 0,
    updated_at: 0,
    version: 1,
    has_thumbnail: false,
    ...overrides,
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('AttachmentsGallery', () => {
  it('renders no category sections at all when the item has no attachments (FR-041 quiet absence)', async () => {
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { attachments: [] }),
    })
    const wrapper = mount(AttachmentsGallery, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.attachments__category').exists()).toBe(false)
    expect(wrapper.find('#attachment-file').exists()).toBe(true)
  })

  it('renders only the category a live attachment actually belongs to', async () => {
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { attachments: [attachment({ category: 'manual' })] }),
    })
    const wrapper = mount(AttachmentsGallery, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    const categories = wrapper.findAll('.attachments__category-title').map((n) => n.text())
    expect(categories).toEqual(['Manuals'])
  })

  it('renders an <img> pointed at the thumbnail route when has_thumbnail is true', async () => {
    stubRoutedFetch({
      [LIST]: () =>
        jsonResponse(200, {
          attachments: [
            attachment({
              id: 'att-img',
              category: 'image',
              original_filename: 'drill.jpg',
              content_type: 'image/jpeg',
              has_thumbnail: true,
            }),
          ],
        }),
    })
    const wrapper = mount(AttachmentsGallery, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    const img = wrapper.get('img.attachments__thumb-img')
    expect(img.attributes('src')).toBe(`/api/v1/items/${ITEM_ID}/attachments/att-img/thumbnail`)
    expect(wrapper.find('.attachments__thumb-fallback').exists()).toBe(false)
  })

  it('renders a filename fallback, never a broken <img>, when has_thumbnail is false', async () => {
    stubRoutedFetch({
      [LIST]: () =>
        jsonResponse(200, {
          attachments: [
            attachment({
              id: 'att-img2',
              category: 'image',
              original_filename: 'broken.png',
              content_type: 'image/png',
              has_thumbnail: false,
            }),
          ],
        }),
    })
    const wrapper = mount(AttachmentsGallery, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('img.attachments__thumb-img').exists()).toBe(false)
    const fallback = wrapper.get('.attachments__thumb-fallback')
    expect(fallback.text()).toContain('broken.png')
  })

  it('does not crash and renders nothing for a category the group has never touched', async () => {
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { attachments: [attachment({ category: 'receipt' })] }),
    })
    const wrapper = mount(AttachmentsGallery, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    const categories = wrapper.findAll('.attachments__category-title').map((n) => n.text())
    expect(categories).toEqual(['Receipts'])
  })

  it('adds a newly uploaded attachment to its category list without a full reload', async () => {
    const created = attachment({
      id: 'att-new',
      category: 'warranty',
      original_filename: 'card.pdf',
    })
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { attachments: [] }),
      [CREATE]: () => jsonResponse(201, created),
    })
    const wrapper = mount(AttachmentsGallery, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    await wrapper.get('#attachment-category').setValue('warranty')
    const fileInput = wrapper.get<HTMLInputElement>('#attachment-file')
    const file = new File(['%PDF-1.4'], 'card.pdf', { type: 'application/pdf' })
    Object.defineProperty(fileInput.element, 'files', { value: [file] })
    await fileInput.trigger('change')
    await wrapper.get('form.attachments__upload').trigger('submit')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('.attachments__category-title').text()).toBe('Warranty documents')
    expect(wrapper.text()).toContain('card.pdf')
  })

  it('removes an attachment from local state on delete, after confirm', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { attachments: [attachment({ id: 'att-del' })] }),
      [`DELETE /api/v1/items/${ITEM_ID}/attachments/att-del`]: () => jsonResponse(204),
    })
    const wrapper = mount(AttachmentsGallery, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.attachments__category').exists()).toBe(true)

    const deleteButton = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteButton!.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.attachments__category').exists()).toBe(false)
  })

  it('surfaces a failed delete rather than removing the row from local state', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    stubRoutedFetch({
      [LIST]: () => jsonResponse(200, { attachments: [attachment({ id: 'att-del' })] }),
      [`DELETE /api/v1/items/${ITEM_ID}/attachments/att-del`]: () =>
        jsonResponse(404, { title: 'Not Found', status: 404 }),
    })
    const wrapper = mount(AttachmentsGallery, { props: { itemId: ITEM_ID } })
    await flush()
    await wrapper.vm.$nextTick()

    const deleteButton = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteButton!.trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.attachments__category').exists()).toBe(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })
})
