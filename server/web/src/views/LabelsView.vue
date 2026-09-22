<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ApiError } from '@/api'
import type { Label } from '@/api'
import { useItemsStore, useLabelsStore } from '@/stores'

const labels = useLabelsStore()
const items = useItemsStore()

const DEFAULT_COLOR = '#2f6f4f'

const newName = ref('')
const newColor = ref(DEFAULT_COLOR)
const busy = ref(false)
const error = ref('')

const editing = ref<Label | null>(null)
const editName = ref('')
const editColor = ref(DEFAULT_COLOR)

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

async function run(action: () => Promise<void>): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await action()
  } catch (err) {
    error.value = describe(err)
  } finally {
    busy.value = false
  }
}

async function create(): Promise<void> {
  const name = newName.value.trim()
  if (!name) return
  await run(async () => {
    await labels.create(name, newColor.value)
    newName.value = ''
    newColor.value = DEFAULT_COLOR
  })
}

function startEdit(label: Label): void {
  editing.value = label
  editName.value = label.name
  editColor.value = label.color
}

async function saveEdit(): Promise<void> {
  const target = editing.value
  if (target === null) return
  await run(async () => {
    await labels.update(target, editName.value.trim(), editColor.value)
    editing.value = null
  })
}

async function remove(label: Label): Promise<void> {
  if (!window.confirm(`Delete the label “${label.name}”? Items keep everything else.`)) return
  await run(async () => {
    await labels.remove(label.id)
    if (items.labelIDs.includes(label.id)) {
      await items.applyFilters({ labelIDs: items.labelIDs.filter((id) => id !== label.id) })
    }
  })
}

onMounted(() => void run(() => labels.refresh()))
</script>

<template>
  <section class="labels v-stack">
    <h1 class="labels__title">Labels</h1>

    <p v-if="error" class="alert" role="alert">{{ error }}</p>

    <form class="card h-stack labels__new" @submit.prevent="create">
      <div class="field labels__new-name">
        <label for="new-label">New label</label>
        <input id="new-label" v-model="newName" class="input" :disabled="busy" />
      </div>
      <div class="field">
        <label for="new-label-color">Colour</label>
        <input
          id="new-label-color"
          v-model="newColor"
          class="labels__color"
          type="color"
          :disabled="busy"
        />
      </div>
      <button class="btn btn--primary" type="submit" :disabled="busy || !newName.trim()">
        Add
      </button>
    </form>

    <p v-if="labels.labels.length === 0" class="empty">
      No labels yet. Labels cut across locations — “fragile”, “lend out”, “winter”.
    </p>

    <ul v-else class="labels__list">
      <li v-for="label in labels.labels" :key="label.id" class="labels__row">
        <template v-if="editing?.id === label.id">
          <form class="h-stack labels__edit" @submit.prevent="saveEdit">
            <input v-model="editName" class="input" :disabled="busy" aria-label="Label name" />
            <input
              v-model="editColor"
              class="labels__color"
              type="color"
              :disabled="busy"
              aria-label="Label colour"
            />
            <button class="btn btn--primary btn--small" type="submit" :disabled="busy">Save</button>
            <button class="btn btn--small" type="button" :disabled="busy" @click="editing = null">
              Cancel
            </button>
          </form>
        </template>

        <template v-else>
          <span class="chip">
            <span class="chip__swatch" :style="{ background: label.color }"></span>
            {{ label.name }}
          </span>
          <span class="h-stack">
            <button
              class="btn btn--ghost btn--small"
              type="button"
              :disabled="busy"
              @click="startEdit(label)"
            >
              Edit
            </button>
            <button
              class="btn btn--ghost btn--small btn--danger"
              type="button"
              :disabled="busy"
              @click="remove(label)"
            >
              Delete
            </button>
          </span>
        </template>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.labels {
  max-width: 34rem;
}

.labels__title {
  margin: 0;
  font-size: 1.25rem;
}

.labels__new {
  align-items: flex-end;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.labels__new-name {
  flex: 1;
  min-width: 10rem;
}

.labels__color {
  width: 3rem;
  height: 2.4rem;
  padding: 0.15rem;
  border: 1px solid var(--hho-border);
  border-radius: var(--hho-radius);
  background: var(--hho-bg);
}

.labels__list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.labels__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.4rem 0.5rem;
  border-bottom: 1px solid var(--hho-border);
  min-height: 2.75rem;
}

.labels__edit {
  flex: 1;
  gap: 0.5rem;
  flex-wrap: wrap;
}
</style>
