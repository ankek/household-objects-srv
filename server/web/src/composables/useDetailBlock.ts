import { ref, type Ref } from 'vue'
import { ApiError } from '@/api'

export interface DetailBlockData {
  readonly version: number
}

export interface DetailBlockOps<T extends DetailBlockData, W extends object> {
  get(itemID: string): Promise<T>
  create(itemID: string, body: W): Promise<T>
  update(itemID: string, body: W & { version: number }): Promise<T>
  remove(itemID: string): Promise<void>
}

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

export function useDetailBlock<T extends DetailBlockData, W extends object>(
  itemID: Ref<string> | string,
  ops: DetailBlockOps<T, W>,
) {
  const id = typeof itemID === 'string' ? itemID : itemID.value

  const data = ref<T | null>(null) as Ref<T | null>
  const loading = ref(true)
  const busy = ref(false)
  const error = ref('')
  const conflict = ref(false)
  const editing = ref(false)

  async function load(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      data.value = await ops.get(id)
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        data.value = null
      } else {
        error.value = describe(err)
      }
    } finally {
      loading.value = false
    }
  }

  function startAdd(): void {
    error.value = ''
    conflict.value = false
    editing.value = true
  }

  function startEdit(): void {
    error.value = ''
    conflict.value = false
    editing.value = true
  }

  function cancel(): void {
    conflict.value = false
    editing.value = false
  }

  async function save(body: W): Promise<void> {
    busy.value = true
    error.value = ''
    conflict.value = false
    try {
      data.value =
        data.value === null
          ? await ops.create(id, body)
          : await ops.update(id, { ...body, version: data.value.version })
      editing.value = false
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        conflict.value = true
      } else {
        error.value = describe(err)
      }
    } finally {
      busy.value = false
    }
  }

  async function discardAndReload(): Promise<void> {
    conflict.value = false
    await load()
  }

  async function remove(): Promise<void> {
    busy.value = true
    error.value = ''
    try {
      await ops.remove(id)
      data.value = null
      editing.value = false
    } catch (err) {
      error.value = describe(err)
    } finally {
      busy.value = false
    }
  }

  return {
    data,
    loading,
    busy,
    error,
    conflict,
    editing,
    load,
    startAdd,
    startEdit,
    cancel,
    save,
    discardAndReload,
    remove,
  }
}
