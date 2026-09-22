<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  ApiError,
  createItemCustomField,
  deleteItemCustomField,
  listCustomFieldDefs,
  listItemCustomFields,
  updateItemCustomField,
} from '@/api'
import type { CustomFieldDef, CustomFieldType, ItemCustomField, ItemCustomFieldWrite } from '@/api'

const props = defineProps<{ itemId: string }>()

const FIELD_TYPES: CustomFieldType[] = ['text', 'number', 'boolean', 'date']

const rows = ref<ItemCustomField[]>([])
const defs = ref<CustomFieldDef[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')

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
    rows.value = [...(await listItemCustomFields(props.itemId)).custom_fields]
  } catch (err) {
    error.value = describe(err)
  } finally {
    loading.value = false
  }
  try {
    defs.value = [...(await listCustomFieldDefs()).custom_field_defs]
  } catch {
    defs.value = []
  }
}

function valueText(row: ItemCustomField): string {
  switch (row.field_type) {
    case 'text':
      return row.text_value ?? ''
    case 'number':
      return String(row.number_value ?? '')
    case 'boolean':
      return row.bool_value ? 'Yes' : 'No'
    case 'date':
      return row.date_value ?? ''
  }
}

const adding = ref(false)
const addDefID = ref('')
const addName = ref('')
const addType = ref<CustomFieldType>('text')
const addText = ref('')
const addNumber = ref(0)
const addBool = ref(false)
const addDate = ref('')

const addDefChosen = computed(() => defs.value.find((d) => d.id === addDefID.value))

function startAdd(): void {
  addDefID.value = ''
  addName.value = ''
  addType.value = 'text'
  addText.value = ''
  addNumber.value = 0
  addBool.value = false
  addDate.value = ''
  adding.value = true
}

function writeFor(defID: string, name: string, type: CustomFieldType): ItemCustomFieldWrite {
  return {
    field_def_id: defID,
    name,
    field_type: type,
    text_value: type === 'text' ? addText.value : null,
    number_value: type === 'number' ? addNumber.value : null,
    bool_value: type === 'boolean' ? addBool.value : null,
    date_value: type === 'date' ? addDate.value : null,
  }
}

async function submitAdd(): Promise<void> {
  const chosen = addDefChosen.value
  const name = (chosen?.name ?? addName.value).trim()
  const type = chosen?.field_type ?? addType.value
  if (!name) return
  busy.value = true
  error.value = ''
  try {
    const created = await createItemCustomField(props.itemId, writeFor(addDefID.value, name, type))
    rows.value = [...rows.value, created]
    adding.value = false
  } catch (err) {
    error.value = describe(err)
  } finally {
    busy.value = false
  }
}

const editingID = ref<string | null>(null)
const editText = ref('')
const editNumber = ref(0)
const editBool = ref(false)
const editDate = ref('')
const conflict = ref(false)

function startEdit(row: ItemCustomField): void {
  editingID.value = row.id
  editText.value = row.text_value ?? ''
  editNumber.value = row.number_value ?? 0
  editBool.value = row.bool_value ?? false
  editDate.value = row.date_value ?? ''
  conflict.value = false
  error.value = ''
}

function cancelEdit(): void {
  editingID.value = null
  conflict.value = false
}

async function submitEdit(): Promise<void> {
  const row = rows.value.find((r) => r.id === editingID.value)
  if (row === undefined) return
  busy.value = true
  error.value = ''
  conflict.value = false
  try {
    const updated = await updateItemCustomField(props.itemId, row.id, {
      field_def_id: row.field_def_id ?? '',
      name: row.name,
      field_type: row.field_type,
      text_value: row.field_type === 'text' ? editText.value : null,
      number_value: row.field_type === 'number' ? editNumber.value : null,
      bool_value: row.field_type === 'boolean' ? editBool.value : null,
      date_value: row.field_type === 'date' ? editDate.value : null,
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

async function remove(row: ItemCustomField): Promise<void> {
  if (!window.confirm(`Delete the "${row.name}" field from this item?`)) return
  busy.value = true
  error.value = ''
  try {
    await deleteItemCustomField(props.itemId, row.id)
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
  <section class="card v-stack detail-block" data-testid="custom-fields-block">
    <h2 class="detail-block__title">Custom fields</h2>

    <p v-if="loading" class="muted">Loading…</p>

    <template v-else>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>

      <button
        v-if="rows.length === 0 && !adding"
        class="btn btn--ghost detail-block__add"
        type="button"
        @click="startAdd"
      >
        + Add custom field
      </button>

      <ul v-if="rows.length > 0" class="detail-block__rows">
        <li v-for="row in rows" :key="row.id" class="detail-block__row">
          <template v-if="editingID === row.id">
            <div v-if="conflict" class="alert detail-block__row-conflict" role="alert">
              Someone else changed this field while you were editing it.
            </div>
            <form class="h-stack detail-block__edit-row" @submit.prevent="submitEdit">
              <span class="detail-block__row-label">{{ row.name }}</span>
              <input
                v-if="row.field_type === 'text'"
                v-model="editText"
                class="input"
                :disabled="busy"
                aria-label="Value"
              />
              <input
                v-else-if="row.field_type === 'number'"
                v-model.number="editNumber"
                class="input"
                type="number"
                :disabled="busy"
                aria-label="Value"
              />
              <label v-else-if="row.field_type === 'boolean'" class="h-stack">
                <input v-model="editBool" type="checkbox" :disabled="busy" />
                Yes
              </label>
              <input
                v-else
                v-model="editDate"
                class="input"
                type="date"
                :disabled="busy"
                aria-label="Value"
              />
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
              <strong>{{ row.name }}:</strong> {{ valueText(row) }}
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

      <form v-if="adding" class="v-stack detail-block__add-form" @submit.prevent="submitAdd">
        <div v-if="defs.length > 0" class="field">
          <label for="custom-field-def">Definition</label>
          <select id="custom-field-def" v-model="addDefID" class="select" :disabled="busy">
            <option value="">Custom (not standardized)</option>
            <option v-for="def in defs" :key="def.id" :value="def.id">{{ def.name }}</option>
          </select>
        </div>

        <template v-if="!addDefChosen">
          <div class="field">
            <label for="custom-field-name">Name</label>
            <input id="custom-field-name" v-model="addName" class="input" :disabled="busy" />
          </div>
          <div class="field">
            <label for="custom-field-type">Type</label>
            <select id="custom-field-type" v-model="addType" class="select" :disabled="busy">
              <option v-for="type in FIELD_TYPES" :key="type" :value="type">{{ type }}</option>
            </select>
          </div>
        </template>

        <div class="field">
          <label for="custom-field-value">Value</label>
          <input
            v-if="(addDefChosen?.field_type ?? addType) === 'text'"
            id="custom-field-value"
            v-model="addText"
            class="input"
            :disabled="busy"
          />
          <input
            v-else-if="(addDefChosen?.field_type ?? addType) === 'number'"
            id="custom-field-value"
            v-model.number="addNumber"
            class="input"
            type="number"
            :disabled="busy"
          />
          <label v-else-if="(addDefChosen?.field_type ?? addType) === 'boolean'" class="h-stack">
            <input id="custom-field-value" v-model="addBool" type="checkbox" :disabled="busy" />
            Yes
          </label>
          <input
            v-else
            id="custom-field-value"
            v-model="addDate"
            class="input"
            type="date"
            :disabled="busy"
          />
        </div>

        <div class="h-stack">
          <button
            class="btn btn--primary btn--small"
            type="submit"
            :disabled="busy || (!addDefChosen && !addName.trim())"
          >
            Add
          </button>
          <button class="btn btn--small" type="button" :disabled="busy" @click="adding = false">
            Cancel
          </button>
        </div>
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

.detail-block__add-form {
  max-width: 20rem;
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

.detail-block__row-label {
  font-weight: 600;
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
