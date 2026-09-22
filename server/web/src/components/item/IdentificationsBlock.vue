<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import {
  ApiError,
  createItemIdentification,
  deleteItemIdentification,
  listItemIdentifications,
  updateItemIdentification,
} from '@/api'
import type { Identification, IdentificationKind } from '@/api'

const props = defineProps<{ itemId: string }>()

const KIND_LABELS: Record<IdentificationKind, string> = {
  serial: 'Serial number',
  model: 'Model',
  asset_tag: 'Asset tag',
  barcode: 'Barcode',
  other: 'Other',
}
const KINDS = Object.keys(KIND_LABELS) as IdentificationKind[]

const rows = ref<Identification[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')

const adding = ref(false)
const addDraft = reactive<{ kind: IdentificationKind; value: string }>({
  kind: 'serial',
  value: '',
})

const editingID = ref<string | null>(null)
const editDraft = reactive<{ kind: IdentificationKind; value: string }>({
  kind: 'serial',
  value: '',
})
const conflict = ref(false)

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    rows.value = [...(await listItemIdentifications(props.itemId)).identifications]
  } catch (err) {
    error.value = describe(err)
  } finally {
    loading.value = false
  }
}

function startAdd(): void {
  addDraft.kind = 'serial'
  addDraft.value = ''
  adding.value = true
}

async function submitAdd(): Promise<void> {
  const value = addDraft.value.trim()
  if (!value) return
  busy.value = true
  error.value = ''
  try {
    const created = await createItemIdentification(props.itemId, { kind: addDraft.kind, value })
    rows.value = [...rows.value, created]
    adding.value = false
  } catch (err) {
    error.value = describe(err)
  } finally {
    busy.value = false
  }
}

function startEdit(row: Identification): void {
  editingID.value = row.id
  editDraft.kind = row.kind
  editDraft.value = row.value
  conflict.value = false
  error.value = ''
}

function cancelEdit(): void {
  editingID.value = null
  conflict.value = false
}

async function submitEdit(): Promise<void> {
  const row = rows.value.find((r) => r.id === editingID.value)
  const value = editDraft.value.trim()
  if (row === undefined || !value) return
  busy.value = true
  error.value = ''
  conflict.value = false
  try {
    const updated = await updateItemIdentification(props.itemId, row.id, {
      kind: editDraft.kind,
      value,
      version: row.version,
    })
    rows.value = rows.value.map((r) => (r.id === updated.id ? updated : r))
    editingID.value = null
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

async function remove(row: Identification): Promise<void> {
  if (!window.confirm(`Delete this ${KIND_LABELS[row.kind].toLowerCase()}?`)) return
  busy.value = true
  error.value = ''
  try {
    await deleteItemIdentification(props.itemId, row.id)
    rows.value = rows.value.filter((r) => r.id !== row.id)
  } catch (err) {
    error.value = describe(err)
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="card v-stack detail-block" data-testid="identifications-block">
    <h2 class="detail-block__title">Identifications</h2>

    <p v-if="loading" class="muted">Loading…</p>

    <template v-else>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>

      <button
        v-if="rows.length === 0 && !adding"
        class="btn btn--ghost detail-block__add"
        type="button"
        @click="startAdd"
      >
        + Add identification
      </button>

      <ul v-if="rows.length > 0" class="detail-block__rows">
        <li v-for="row in rows" :key="row.id" class="detail-block__row">
          <template v-if="editingID === row.id">
            <div v-if="conflict" class="alert detail-block__row-conflict" role="alert">
              Someone else changed this row while you were editing it.
            </div>
            <form class="h-stack detail-block__edit-row" @submit.prevent="submitEdit">
              <select v-model="editDraft.kind" class="select" :disabled="busy">
                <option v-for="kind in KINDS" :key="kind" :value="kind">
                  {{ KIND_LABELS[kind] }}
                </option>
              </select>
              <input v-model="editDraft.value" class="input" :disabled="busy" aria-label="Value" />
              <button class="btn btn--primary btn--small" type="submit" :disabled="busy">
                Save
              </button>
              <button class="btn btn--small" type="button" :disabled="busy" @click="cancelEdit">
                Cancel
              </button>
            </form>
          </template>
          <template v-else>
            <span class="detail-block__row-text">
              <strong>{{ KIND_LABELS[row.kind] }}:</strong> {{ row.value }}
            </span>
            <span class="h-stack">
              <button
                class="btn btn--ghost btn--small"
                type="button"
                :disabled="busy"
                @click="startEdit(row)"
              >
                Edit
              </button>
              <button
                class="btn btn--ghost btn--small btn--danger"
                type="button"
                :disabled="busy"
                @click="remove(row)"
              >
                Delete
              </button>
            </span>
          </template>
        </li>
      </ul>

      <form v-if="adding" class="h-stack detail-block__edit-row" @submit.prevent="submitAdd">
        <select v-model="addDraft.kind" class="select" :disabled="busy">
          <option v-for="kind in KINDS" :key="kind" :value="kind">{{ KIND_LABELS[kind] }}</option>
        </select>
        <input
          v-model="addDraft.value"
          class="input"
          :disabled="busy"
          placeholder="Value"
          aria-label="Value"
        />
        <button
          class="btn btn--primary btn--small"
          type="submit"
          :disabled="busy || !addDraft.value.trim()"
        >
          Add
        </button>
        <button class="btn btn--small" type="button" :disabled="busy" @click="adding = false">
          Cancel
        </button>
      </form>
      <button
        v-else-if="rows.length > 0"
        class="btn btn--ghost btn--small detail-block__add"
        type="button"
        @click="startAdd"
      >
        + Add another
      </button>
    </template>
  </section>
</template>

<style scoped>
.detail-block__title {
  margin: 0;
  font-size: 0.8125rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--hho-muted);
}

.detail-block__add {
  align-self: flex-start;
}

.detail-block__rows {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

.detail-block__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.35rem 0;
  border-bottom: 1px solid var(--hho-border);
}

.detail-block__row-text {
  flex: 1;
}

.detail-block__edit-row {
  flex-wrap: wrap;
}

.detail-block__edit-row .input,
.detail-block__edit-row .select {
  min-width: 0;
  flex: 1;
}

.detail-block__row-conflict {
  padding: 0.4rem 0.6rem;
}

.alert {
  margin: 0;
}
</style>
