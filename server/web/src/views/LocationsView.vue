<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ApiError } from '@/api'
import type { Location, LocationTreeNode } from '@/api'
import { useLocationsStore } from '@/stores'

const locations = useLocationsStore()

const newName = ref('')
const newParent = ref('')
const busy = ref(false)
const error = ref('')

const editing = ref<Location | null>(null)
const editName = ref('')
const editParent = ref('')

const guard = ref<{ location: Location; childCount: number; itemCount: number } | null>(null)
const reassignTo = ref('')

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

interface Choice {
  id: string
  label: string
}

function flatten(nodes: readonly LocationTreeNode[], depth = 0, out: Choice[] = []): Choice[] {
  for (const node of nodes) {
    out.push({ id: node.id, label: `${'  '.repeat(depth)}${node.name}` })
    flatten(node.children, depth + 1, out)
  }
  return out
}

const choices = computed(() => flatten(locations.tree))

const editChoices = computed(() => {
  const target = editing.value
  if (target === null) return choices.value
  const banned = new Set<string>()
  const walk = (nodes: readonly LocationTreeNode[], inside: boolean): void => {
    for (const node of nodes) {
      const within = inside || node.id === target.id
      if (within) banned.add(node.id)
      walk(node.children, within)
    }
  }
  walk(locations.tree, false)
  return choices.value.filter((choice) => !banned.has(choice.id))
})

const reassignChoices = computed(() => {
  const target = guard.value?.location
  if (target === undefined) return choices.value
  const banned = new Set<string>()
  const walk = (nodes: readonly LocationTreeNode[], inside: boolean): void => {
    for (const node of nodes) {
      const within = inside || node.id === target.id
      if (within) banned.add(node.id)
      walk(node.children, within)
    }
  }
  walk(locations.tree, false)
  return choices.value.filter((choice) => !banned.has(choice.id))
})

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
    await locations.create(name, newParent.value || undefined)
    newName.value = ''
  })
}

function startEdit(location: Location): void {
  editing.value = location
  editName.value = location.name
  editParent.value = location.parent_id ?? ''
}

async function saveEdit(): Promise<void> {
  const target = editing.value
  if (target === null) return
  await run(async () => {
    await locations.update(target, editName.value.trim(), editParent.value || undefined)
    editing.value = null
  })
}

async function requestDelete(node: LocationTreeNode): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await locations.remove(node.id)
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      const problem = err.problem as { child_count?: number; item_count?: number } | null
      guard.value = {
        location: node,
        childCount: problem?.child_count ?? 0,
        itemCount: problem?.item_count ?? 0,
      }
      reassignTo.value = ''
    } else {
      error.value = describe(err)
    }
  } finally {
    busy.value = false
  }
}

async function confirmDelete(): Promise<void> {
  const pending = guard.value
  if (pending === null) return
  await run(async () => {
    await locations.remove(pending.location.id, reassignTo.value)
    guard.value = null
  })
}

onMounted(() => void run(() => locations.refresh()))
</script>

<template>
  <section class="locations v-stack">
    <h1 class="locations__title">Locations</h1>

    <p v-if="error" class="alert" role="alert">{{ error }}</p>

    <form class="card h-stack locations__new" @submit.prevent="create">
      <div class="field locations__new-name">
        <label for="new-location">New location</label>
        <input id="new-location" v-model="newName" class="input" :disabled="busy" />
      </div>
      <div class="field">
        <label for="new-location-parent">Inside</label>
        <select id="new-location-parent" v-model="newParent" class="select" :disabled="busy">
          <option value="">Nothing (top level)</option>
          <option v-for="choice in choices" :key="choice.id" :value="choice.id">
            {{ choice.label }}
          </option>
        </select>
      </div>
      <button class="btn btn--primary" type="submit" :disabled="busy || !newName.trim()">
        Add
      </button>
    </form>

    <p v-if="locations.tree.length === 0" class="empty">
      No locations yet. Add the first one above — a room, a cupboard, a shelf.
    </p>

    <ul v-else class="locations__list">
      <li v-for="choice in choices" :key="choice.id" class="locations__row">
        <template v-if="editing?.id === choice.id">
          <form class="h-stack locations__edit" @submit.prevent="saveEdit">
            <input v-model="editName" class="input" :disabled="busy" aria-label="Location name" />
            <select
              v-model="editParent"
              class="select"
              :disabled="busy"
              aria-label="Parent location"
            >
              <option value="">Nothing (top level)</option>
              <option v-for="option in editChoices" :key="option.id" :value="option.id">
                {{ option.label }}
              </option>
            </select>
            <button class="btn btn--primary btn--small" type="submit" :disabled="busy">Save</button>
            <button class="btn btn--small" type="button" :disabled="busy" @click="editing = null">
              Cancel
            </button>
          </form>
        </template>

        <template v-else>
          <span class="locations__name">{{ choice.label }}</span>
          <span class="h-stack">
            <button
              class="btn btn--ghost btn--small"
              type="button"
              :disabled="busy"
              @click="startEdit(locations.flat.find((l) => l.id === choice.id)!)"
            >
              Rename / move
            </button>
            <button
              class="btn btn--ghost btn--small btn--danger"
              type="button"
              :disabled="busy"
              @click="requestDelete({ id: choice.id } as LocationTreeNode)"
            >
              Delete
            </button>
          </span>
        </template>
      </li>
    </ul>

    <div
      v-if="guard"
      class="card v-stack locations__guard"
      role="dialog"
      aria-labelledby="guard-title"
    >
      <h2 id="guard-title" class="locations__subtitle">This location is not empty</h2>
      <p class="muted">
        It directly holds
        {{ guard.childCount }} sub-location{{ guard.childCount === 1 ? '' : 's' }} and
        {{ guard.itemCount }} item{{ guard.itemCount === 1 ? '' : 's' }}. Deleting it has to say
        where they go.
      </p>

      <div class="field">
        <label for="reassign">Move them to</label>
        <select id="reassign" v-model="reassignTo" class="select" :disabled="busy">
          <option value="">Nowhere — make them top-level / unplaced</option>
          <option v-for="choice in reassignChoices" :key="choice.id" :value="choice.id">
            {{ choice.label }}
          </option>
        </select>
      </div>

      <div class="h-stack">
        <button class="btn btn--danger" type="button" :disabled="busy" @click="confirmDelete">
          Delete and move
        </button>
        <button class="btn" type="button" :disabled="busy" @click="guard = null">Cancel</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.locations {
  max-width: 44rem;
}

.locations__title {
  margin: 0;
  font-size: 1.25rem;
}

.locations__subtitle {
  margin: 0;
  font-size: 1rem;
}

.locations__new {
  align-items: flex-end;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.locations__new-name {
  flex: 1;
  min-width: 10rem;
}

.locations__list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.locations__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.4rem 0.5rem;
  border-bottom: 1px solid var(--hho-border);
  min-height: 2.75rem;
}

.locations__name {
  white-space: pre;
}

.locations__edit {
  flex: 1;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.locations__guard {
  border-color: var(--hho-danger);
}
</style>
