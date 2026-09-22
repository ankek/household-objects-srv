import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { createLabel, deleteLabel, listLabels, updateLabel } from '@/api'
import type { Label } from '@/api'

export const useLabelsStore = defineStore('labels', () => {
  const labels = ref<readonly Label[]>([])
  const loading = ref(false)

  const byID = computed(() => {
    const index = new Map<string, Label>()
    for (const label of labels.value) index.set(label.id, label)
    return index
  })

  async function refresh(): Promise<void> {
    loading.value = true
    try {
      labels.value = (await listLabels()).labels
    } finally {
      loading.value = false
    }
  }

  async function create(name: string, color: string): Promise<void> {
    await createLabel(name, color)
    await refresh()
  }

  async function update(label: Label, name: string, color: string): Promise<void> {
    await updateLabel(label.id, { name, color, version: label.version })
    await refresh()
  }

  async function remove(id: string): Promise<void> {
    await deleteLabel(id)
    await refresh()
  }

  return { labels, loading, byID, refresh, create, update, remove }
})
