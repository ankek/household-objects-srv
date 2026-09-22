import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '@/router'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import ItemEditView from '@/views/ItemEditView.vue'

const ITEM_ID = 'item-1'

const ITEM = {
  id: ITEM_ID,
  created_at: 0,
  updated_at: 0,
  version: 1,
  name: 'Cordless drill',
  description: '',
  location_id: '',
  quantity: 1,
}

function allVisible() {
  return { warranty_visible: true, sale_visible: true, purchase_visible: true }
}

function baseRoutes(overrides: Routes = {}): Routes {
  return {
    'GET /api/v1/auth/sessions': () => jsonResponse(200, { sessions: [] }),
    'GET /api/v1/locations/tree': () => jsonResponse(200, { tree: [] }),
    'GET /api/v1/locations': () => jsonResponse(200, { locations: [] }),
    'GET /api/v1/labels': () => jsonResponse(200, { labels: [] }),
    'GET /api/v1/groups/detail-visibility': () => jsonResponse(200, allVisible()),
    [`GET /api/v1/items/${ITEM_ID}`]: () => jsonResponse(200, ITEM),
    [`GET /api/v1/items/${ITEM_ID}/labels`]: () => jsonResponse(200, { labels: [] }),
    [`GET /api/v1/items/${ITEM_ID}/warranty`]: () =>
      jsonResponse(404, { title: 'Not Found', status: 404 }),
    [`GET /api/v1/items/${ITEM_ID}/sale`]: () =>
      jsonResponse(404, { title: 'Not Found', status: 404 }),
    [`GET /api/v1/items/${ITEM_ID}/purchase`]: () =>
      jsonResponse(404, { title: 'Not Found', status: 404 }),
    [`GET /api/v1/items/${ITEM_ID}/identifications`]: () =>
      jsonResponse(200, { identifications: [] }),
    [`GET /api/v1/items/${ITEM_ID}/custom-fields`]: () => jsonResponse(200, { custom_fields: [] }),
    'GET /api/v1/custom-field-defs': () => jsonResponse(200, { custom_field_defs: [] }),
    [`GET /api/v1/items/${ITEM_ID}/attachments`]: () => jsonResponse(200, { attachments: [] }),
    ...overrides,
  }
}

let pinia: ReturnType<typeof createPinia>
let router: ReturnType<typeof createAppRouter>

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  router = createAppRouter()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function mountEdit(routes: Routes) {
  stubRoutedFetch(routes)
  const wrapper = mount(ItemEditView, {
    props: { id: ITEM_ID },
    global: { plugins: [router, pinia] },
  })
  await flush()
  await wrapper.vm.$nextTick()
  return wrapper
}

describe('ItemEditView detail blocks, all visible', () => {
  it('mounts every detail block below the core form', async () => {
    const wrapper = await mountEdit(baseRoutes())

    expect(wrapper.find('#item-name').exists()).toBe(true)
    expect(wrapper.find('[data-testid="warranty-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="sale-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="purchase-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="identifications-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="custom-fields-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="attachments-gallery"]').exists()).toBe(true)
  })

  it('does not mount any detail block for a NEW item, which has no id to scope them to', async () => {
    stubRoutedFetch({
      'GET /api/v1/auth/sessions': () => jsonResponse(200, { sessions: [] }),
      'GET /api/v1/locations/tree': () => jsonResponse(200, { tree: [] }),
      'GET /api/v1/locations': () => jsonResponse(200, { locations: [] }),
      'GET /api/v1/labels': () => jsonResponse(200, { labels: [] }),
      'GET /api/v1/groups/detail-visibility': () => jsonResponse(200, allVisible()),
    })
    const wrapper = mount(ItemEditView, {
      props: {},
      global: { plugins: [router, pinia] },
    })
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="warranty-block"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="identifications-block"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="attachments-gallery"]').exists()).toBe(false)
  })
})

describe('ItemEditView detail blocks, group visibility (FR-011)', () => {
  it.each([
    ['warranty', 'warranty_visible', 'warranty-block', '+ Add warranty details'],
    ['sale', 'sale_visible', 'sale-block', '+ Add sale details'],
    ['purchase', 'purchase_visible', 'purchase-block', '+ Add purchase details'],
  ] as const)(
    'does not mount the %s block at all when the group has turned it off, not even its "add" affordance',
    async (_label, flag, testid, addText) => {
      const wrapper = await mountEdit(
        baseRoutes({
          'GET /api/v1/groups/detail-visibility': () =>
            jsonResponse(200, { ...allVisible(), [flag]: false }),
        }),
      )

      expect(wrapper.find(`[data-testid="${testid}"]`).exists()).toBe(false)
      expect(wrapper.text()).not.toContain(addText)
    },
  )

  it('mounts none of warranty/sale/purchase when every flag is off, but still mounts identifications and custom-fields', async () => {
    const wrapper = await mountEdit(
      baseRoutes({
        'GET /api/v1/groups/detail-visibility': () =>
          jsonResponse(200, {
            warranty_visible: false,
            sale_visible: false,
            purchase_visible: false,
          }),
      }),
    )

    expect(wrapper.find('[data-testid="warranty-block"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sale-block"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="purchase-block"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="identifications-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="custom-fields-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="attachments-gallery"]').exists()).toBe(true)
  })

  it('hides warranty/sale/purchase (treats "unknown" as "off") when the visibility fetch itself fails', async () => {
    const wrapper = await mountEdit(
      baseRoutes({
        'GET /api/v1/groups/detail-visibility': () => Promise.reject(new Error('network down')),
      }),
    )

    expect(wrapper.find('[data-testid="warranty-block"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sale-block"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="purchase-block"]').exists()).toBe(false)
    expect(wrapper.find('#item-name').exists()).toBe(true)
    expect(wrapper.find('[data-testid="identifications-block"]').exists()).toBe(true)
  })
})

describe('ItemEditView resilience to a failing detail block', () => {
  it('still renders the core form and every other block when one block fetch rejects', async () => {
    const wrapper = await mountEdit(
      baseRoutes({
        [`GET /api/v1/items/${ITEM_ID}/warranty`]: () => Promise.reject(new Error('network down')),
      }),
    )

    expect(wrapper.get<HTMLInputElement>('#item-name').element.value).toBe('Cordless drill')
    const warrantyBlock = wrapper.get('[data-testid="warranty-block"]')
    expect(warrantyBlock.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="sale-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="purchase-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="identifications-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="custom-fields-block"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="attachments-gallery"]').exists()).toBe(true)
  })
})
