import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { createItem, deleteItem, getItem, listItems, updateItem } from '@/api'
import type { Item, ItemQuery, ItemWrite } from '@/api'

export const PAGE_SIZE = 50

export const useItemsStore = defineStore('items', () => {
  const items = ref<readonly Item[]>([])
  const loading = ref(false)
  const offset = ref(0)

  const q = ref('')
  const locationID = ref<string | undefined>(undefined)
  const descendants = ref(true)
  const labelIDs = ref<string[]>([])

  const hasFilters = computed(
    () => q.value.trim() !== '' || locationID.value !== undefined || labelIDs.value.length > 0,
  )

  const hasNextPage = computed(() => items.value.length === PAGE_SIZE)
  const hasPreviousPage = computed(() => offset.value > 0)

  function currentQuery(): ItemQuery {
    return {
      q: q.value,
      locationId: locationID.value,
      descendants: descendants.value,
      labelIds: labelIDs.value,
      limit: PAGE_SIZE,
      offset: offset.value,
    }
  }

  async function refresh(): Promise<void> {
    loading.value = true
    try {
      items.value = (await listItems(currentQuery())).items
    } finally {
      loading.value = false
    }
  }

  async function applyFilters(next: {
    q?: string
    locationID?: string | undefined
    descendants?: boolean
    labelIDs?: string[]
  }): Promise<void> {
    if (next.q !== undefined) q.value = next.q
    if (next.descendants !== undefined) descendants.value = next.descendants
    if (next.labelIDs !== undefined) labelIDs.value = next.labelIDs
    if ('locationID' in next) locationID.value = next.locationID
    offset.value = 0
    await refresh()
  }

  async function clearFilters(): Promise<void> {
    q.value = ''
    locationID.value = undefined
    labelIDs.value = []
    offset.value = 0
    await refresh()
  }

  async function toggleLabel(id: string): Promise<void> {
    const next = labelIDs.value.includes(id)
      ? labelIDs.value.filter((existing) => existing !== id)
      : [...labelIDs.value, id]
    await applyFilters({ labelIDs: next })
  }

  async function nextPage(): Promise<void> {
    if (!hasNextPage.value) return
    offset.value += PAGE_SIZE
    await refresh()
  }

  async function previousPage(): Promise<void> {
    if (!hasPreviousPage.value) return
    offset.value = Math.max(0, offset.value - PAGE_SIZE)
    await refresh()
  }

  function fetchOne(id: string): Promise<Item> {
    return getItem(id)
  }

  async function create(body: ItemWrite): Promise<Item> {
    const created = await createItem(body)
    await refresh()
    return created
  }

  async function save(item: Item, body: ItemWrite): Promise<Item> {
    const saved = await updateItem(item.id, { ...body, version: item.version })
    await refresh()
    return saved
  }

  async function remove(id: string): Promise<void> {
    await deleteItem(id)
    if (items.value.length === 1 && offset.value > 0) {
      offset.value = Math.max(0, offset.value - PAGE_SIZE)
    }
    await refresh()
  }

  return {
    items,
    loading,
    offset,
    q,
    locationID,
    descendants,
    labelIDs,
    hasFilters,
    hasNextPage,
    hasPreviousPage,
    refresh,
    applyFilters,
    clearFilters,
    toggleLabel,
    nextPage,
    previousPage,
    fetchOne,
    create,
    save,
    remove,
  }
})
