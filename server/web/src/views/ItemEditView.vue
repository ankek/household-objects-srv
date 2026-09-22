<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, attachLabel, detachLabel, getGroupDetailVisibility, listItemLabels } from '@/api'
import type { GroupVisibility, Item } from '@/api'
import AttachmentsGallery from '@/components/item/AttachmentsGallery.vue'
import CustomFieldsBlock from '@/components/item/CustomFieldsBlock.vue'
import IdentificationsBlock from '@/components/item/IdentificationsBlock.vue'
import PurchaseBlock from '@/components/item/PurchaseBlock.vue'
import SaleBlock from '@/components/item/SaleBlock.vue'
import WarrantyBlock from '@/components/item/WarrantyBlock.vue'
import { useItemsStore, useLabelsStore, useLocationsStore } from '@/stores'

const props = defineProps<{ id?: string }>()

const router = useRouter()
const items = useItemsStore()
const locations = useLocationsStore()
const labels = useLabelsStore()

const isNew = computed(() => props.id === undefined)

const original = ref<Item | null>(null)

const name = ref('')
const description = ref('')
const locationID = ref('')
const quantity = ref(1)
const attached = ref<Set<string>>(new Set())

const visibility = ref<GroupVisibility | null>(null)

const loading = ref(true)
const busy = ref(false)
const error = ref('')
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
    const [, , visibilityResult] = await Promise.allSettled([
      locations.refresh(),
      labels.refresh(),
      getGroupDetailVisibility(),
    ])
    if (visibilityResult.status === 'fulfilled') visibility.value = visibilityResult.value

    if (props.id === undefined) return

    const item = await items.fetchOne(props.id)
    original.value = item
    name.value = item.name
    description.value = item.description ?? ''
    locationID.value = item.location_id ?? ''
    quantity.value = item.quantity
    attached.value = new Set((await listItemLabels(props.id)).labels.map((label) => label.id))
  } catch (err) {
    error.value = describe(err)
  } finally {
    loading.value = false
  }
}

async function toggleLabel(labelID: string): Promise<void> {
  if (props.id === undefined) return
  const on = attached.value.has(labelID)
  const next = new Set(attached.value)
  if (on) next.delete(labelID)
  else next.add(labelID)
  attached.value = next

  try {
    if (on) await detachLabel(props.id, labelID)
    else await attachLabel(props.id, labelID)
  } catch (err) {
    const reverted = new Set(attached.value)
    if (on) reverted.add(labelID)
    else reverted.delete(labelID)
    attached.value = reverted
    error.value = describe(err)
  }
}

async function save(): Promise<void> {
  busy.value = true
  error.value = ''
  conflict.value = false
  try {
    const body = {
      name: name.value.trim(),
      description: description.value,
      location_id: locationID.value,
      quantity: quantity.value,
    }

    if (original.value === null) {
      const created = await items.create(body)
      await router.replace(`/items/${created.id}`)
      return
    }

    const saved = await items.save(original.value, body)
    original.value = saved
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
  if (props.id === undefined) return
  if (!window.confirm(`Delete “${original.value?.name ?? 'this item'}”?`)) return
  busy.value = true
  try {
    await items.remove(props.id)
    await router.replace('/items')
  } catch (err) {
    error.value = describe(err)
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="edit v-stack">
    <h1 class="edit__title">{{ isNew ? 'New item' : 'Edit item' }}</h1>

    <p v-if="loading" class="muted">Loading…</p>

    <template v-else>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>

      <div v-if="conflict" class="alert" role="alert">
        <p>
          Someone else changed this item while you were editing it. Your changes have not been
          saved.
        </p>
        <button class="btn btn--small" type="button" @click="discardAndReload">
          Discard my changes and load theirs
        </button>
      </div>

      <form class="card v-stack" @submit.prevent="save">
        <div class="field">
          <label for="item-name">Name</label>
          <input id="item-name" v-model="name" class="input" required :disabled="busy" />
        </div>

        <div class="field">
          <label for="item-description">Description</label>
          <textarea
            id="item-description"
            v-model="description"
            class="textarea"
            :disabled="busy"
          ></textarea>
        </div>

        <div class="field">
          <label for="item-location">Location</label>
          <select id="item-location" v-model="locationID" class="select" :disabled="busy">
            <option value="">Unplaced</option>
            <option v-for="location in locations.flat" :key="location.id" :value="location.id">
              {{ location.name }}
            </option>
          </select>
        </div>

        <div class="field edit__quantity">
          <label for="item-quantity">Quantity</label>
          <input
            id="item-quantity"
            v-model.number="quantity"
            class="input"
            type="number"
            min="0"
            step="1"
            :disabled="busy"
          />
        </div>

        <div class="h-stack edit__actions">
          <button class="btn btn--primary" type="submit" :disabled="busy || !name.trim()">
            {{ busy ? 'Saving…' : 'Save' }}
          </button>
          <button class="btn" type="button" :disabled="busy" @click="router.back()">Cancel</button>
          <button
            v-if="!isNew"
            class="btn btn--danger"
            type="button"
            :disabled="busy"
            @click="remove"
          >
            Delete
          </button>
        </div>
      </form>

      <section v-if="!isNew" class="card v-stack">
        <h2 class="edit__subtitle">Labels</h2>
        <p v-if="labels.labels.length === 0" class="muted">
          No labels yet. <RouterLink to="/labels">Create one</RouterLink>.
        </p>
        <ul v-else class="edit__labels">
          <li v-for="label in labels.labels" :key="label.id">
            <button
              type="button"
              class="chip edit__label"
              :class="{ 'edit__label--on': attached.has(label.id) }"
              :aria-pressed="attached.has(label.id)"
              @click="toggleLabel(label.id)"
            >
              <span class="chip__swatch" :style="{ background: label.color }"></span>
              {{ label.name }}
            </button>
          </li>
        </ul>
      </section>

      <p v-else class="muted edit__hint">Labels can be added once the item is saved.</p>

      <template v-if="!isNew && original !== null">
        <WarrantyBlock v-if="visibility?.warranty_visible" :item-id="original.id" />
        <SaleBlock v-if="visibility?.sale_visible" :item-id="original.id" />
        <PurchaseBlock v-if="visibility?.purchase_visible" :item-id="original.id" />
        <IdentificationsBlock :item-id="original.id" />
        <CustomFieldsBlock :item-id="original.id" />
        <AttachmentsGallery :item-id="original.id" />
      </template>
    </template>
  </section>
</template>

<style scoped>
.edit {
  max-width: 34rem;
}

.edit__title {
  margin: 0;
  font-size: 1.25rem;
}

.edit__subtitle {
  margin: 0;
  font-size: 0.8125rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--hho-muted);
}

.edit__quantity {
  max-width: 8rem;
}

.edit__actions {
  gap: 0.5rem;
}

.edit__labels {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.edit__label {
  cursor: pointer;
  font: inherit;
  font-size: 0.8125rem;
  color: inherit;
}

.edit__label--on {
  border-color: var(--hho-accent);
  background: var(--hho-accent);
  color: var(--hho-accent-fg);
}

.edit__hint {
  font-size: 0.875rem;
}

.alert p {
  margin: 0 0 0.5rem;
}
</style>
