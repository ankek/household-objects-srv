import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { createLocation, deleteLocation, listLocations, locationTree, updateLocation } from '@/api'
import type { Location, LocationTreeNode } from '@/api'

export const useLocationsStore = defineStore('locations', () => {
  const tree = ref<readonly LocationTreeNode[]>([])
  const flat = ref<readonly Location[]>([])
  const loading = ref(false)

  const nameByID = computed(() => {
    const index = new Map<string, string>()
    for (const location of flat.value) index.set(location.id, location.name)
    return index
  })

  async function refresh(): Promise<void> {
    loading.value = true
    try {
      tree.value = (await locationTree()).tree
      flat.value = (await listLocations()).locations
    } finally {
      loading.value = false
    }
  }

  async function create(name: string, parentID?: string): Promise<void> {
    await createLocation(name, parentID)
    await refresh()
  }

  async function update(location: Location, name: string, parentID?: string): Promise<void> {
    await updateLocation(location.id, {
      name,
      parent_id: parentID ?? '',
      version: location.version,
    })
    await refresh()
  }

  async function remove(id: string, reassignTo?: string): Promise<void> {
    await deleteLocation(id, reassignTo)
    await refresh()
  }

  return { tree, flat, loading, nameByID, refresh, create, update, remove }
})
