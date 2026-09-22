<script setup lang="ts">
import { computed, onMounted, reactive } from 'vue'
import { createItemWarranty, deleteItemWarranty, getItemWarranty, updateItemWarranty } from '@/api'
import type { WarrantyWrite } from '@/api'
import { useDetailBlock } from '@/composables/useDetailBlock'

const props = defineProps<{ itemId: string }>()

const {
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
} = useDetailBlock(props.itemId, {
  get: getItemWarranty,
  create: createItemWarranty,
  update: updateItemWarranty,
  remove: deleteItemWarranty,
})

const draft = reactive<Required<WarrantyWrite>>({
  holder: '',
  provider: '',
  starts_on: '',
  expires_on: '',
  is_lifetime: false,
  notes: '',
})

function resetDraft(): void {
  draft.holder = data.value?.holder ?? ''
  draft.provider = data.value?.provider ?? ''
  draft.starts_on = data.value?.starts_on ?? ''
  draft.expires_on = data.value?.expires_on ?? ''
  draft.is_lifetime = data.value?.is_lifetime ?? false
  draft.notes = data.value?.notes ?? ''
}

function add(): void {
  resetDraft()
  startAdd()
}

function edit(): void {
  resetDraft()
  startEdit()
}

async function submit(): Promise<void> {
  await save({ ...draft })
}

async function removeBlock(): Promise<void> {
  if (!window.confirm('Remove the warranty details for this item?')) return
  await remove()
}

const summary = computed(() => {
  const warranty = data.value
  if (warranty === null) return ''
  const parts: string[] = []
  if (warranty.provider) parts.push(warranty.provider)
  if (warranty.is_lifetime) parts.push('lifetime')
  else if (warranty.expires_on) parts.push(`expires ${warranty.expires_on}`)
  return parts.length > 0 ? parts.join(' · ') : 'On file, no dates recorded'
})

onMounted(load)
</script>

<template>
  <section class="card v-stack detail-block" data-testid="warranty-block">
    <h2 class="detail-block__title">Warranty</h2>

    <p v-if="loading" class="muted">Loading…</p>

    <template v-else>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>

      <div v-if="conflict" class="alert" role="alert">
        <p>Someone else changed the warranty details while you were editing them.</p>
        <button class="btn btn--small" type="button" @click="discardAndReload">
          Discard my changes and load theirs
        </button>
      </div>

      <button
        v-if="data === null && !editing"
        class="btn btn--ghost detail-block__add"
        type="button"
        @click="add"
      >
        + Add warranty details
      </button>

      <div v-else-if="data !== null && !editing" class="h-stack detail-block__summary">
        <p class="detail-block__summary-text">{{ summary }}</p>
        <button class="btn btn--ghost btn--small" type="button" @click="edit">Edit</button>
        <button
          class="btn btn--ghost btn--small btn--danger"
          type="button"
          :disabled="busy"
          @click="removeBlock"
        >
          Remove
        </button>
      </div>

      <form v-else class="v-stack" @submit.prevent="submit">
        <div class="field">
          <label for="warranty-provider">Provider</label>
          <input id="warranty-provider" v-model="draft.provider" class="input" :disabled="busy" />
        </div>
        <div class="field">
          <label for="warranty-holder">Held by</label>
          <input id="warranty-holder" v-model="draft.holder" class="input" :disabled="busy" />
        </div>
        <div class="h-stack detail-block__field-row">
          <div class="field">
            <label for="warranty-starts">Starts</label>
            <input
              id="warranty-starts"
              v-model="draft.starts_on"
              class="input"
              type="date"
              :disabled="busy"
            />
          </div>
          <div class="field">
            <label for="warranty-expires">Expires</label>
            <input
              id="warranty-expires"
              v-model="draft.expires_on"
              class="input"
              type="date"
              :disabled="busy || draft.is_lifetime"
            />
          </div>
        </div>
        <label class="h-stack detail-block__checkbox">
          <input v-model="draft.is_lifetime" type="checkbox" :disabled="busy" />
          Lifetime warranty
        </label>
        <div class="field">
          <label for="warranty-notes">Notes</label>
          <textarea
            id="warranty-notes"
            v-model="draft.notes"
            class="textarea"
            :disabled="busy"
          ></textarea>
        </div>
        <div class="h-stack">
          <button class="btn btn--primary" type="submit" :disabled="busy">
            {{ busy ? 'Saving…' : 'Save' }}
          </button>
          <button class="btn" type="button" :disabled="busy" @click="cancel">Cancel</button>
        </div>
      </form>
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

.detail-block__summary {
  justify-content: space-between;
}

.detail-block__summary-text {
  margin: 0;
  flex: 1;
}

.detail-block__field-row {
  flex-wrap: wrap;
}

.detail-block__field-row .field {
  flex: 1;
  min-width: 8rem;
}

.detail-block__checkbox {
  font-size: 0.875rem;
  cursor: pointer;
}

.alert p {
  margin: 0 0 0.5rem;
}
</style>
