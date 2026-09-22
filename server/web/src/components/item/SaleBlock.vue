<script setup lang="ts">
import { computed, onMounted, reactive } from 'vue'
import { createItemSale, deleteItemSale, getItemSale, updateItemSale } from '@/api'
import type { SaleWrite } from '@/api'
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
  get: getItemSale,
  create: createItemSale,
  update: updateItemSale,
  remove: deleteItemSale,
})

const draft = reactive<{
  buyer_name: string
  sold_on: string
  sale_price_minor: number
  notes: string
}>({
  buyer_name: '',
  sold_on: '',
  sale_price_minor: 0,
  notes: '',
})

function resetDraft(): void {
  draft.buyer_name = data.value?.buyer_name ?? ''
  draft.sold_on = data.value?.sold_on ?? ''
  draft.sale_price_minor = data.value?.sale_price_minor ?? 0
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
  const body: SaleWrite = { ...draft }
  await save(body)
}

async function removeBlock(): Promise<void> {
  if (!window.confirm('Remove the sale details for this item?')) return
  await remove()
}

function formatMinor(minor: number): string {
  return (minor / 100).toFixed(2)
}

const summary = computed(() => {
  const sale = data.value
  if (sale === null) return ''
  const parts: string[] = [formatMinor(sale.sale_price_minor)]
  if (sale.buyer_name) parts.push(`to ${sale.buyer_name}`)
  if (sale.sold_on) parts.push(`on ${sale.sold_on}`)
  return parts.join(' ')
})

onMounted(load)
</script>

<template>
  <section class="card v-stack detail-block" data-testid="sale-block">
    <h2 class="detail-block__title">Sale</h2>

    <p v-if="loading" class="muted">Loading…</p>

    <template v-else>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>

      <div v-if="conflict" class="alert" role="alert">
        <p>Someone else changed the sale details while you were editing them.</p>
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
        + Add sale details
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
          <label for="sale-buyer">Buyer</label>
          <input id="sale-buyer" v-model="draft.buyer_name" class="input" :disabled="busy" />
        </div>
        <div class="h-stack detail-block__field-row">
          <div class="field">
            <label for="sale-date">Sold on</label>
            <input
              id="sale-date"
              v-model="draft.sold_on"
              class="input"
              type="date"
              :disabled="busy"
            />
          </div>
          <div class="field">
            <label for="sale-price">Price</label>
            <input
              id="sale-price"
              v-model.number="draft.sale_price_minor"
              class="input"
              type="number"
              min="0"
              step="1"
              :disabled="busy"
            />
          </div>
        </div>
        <div class="field">
          <label for="sale-notes">Notes</label>
          <textarea
            id="sale-notes"
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

.alert p {
  margin: 0 0 0.5rem;
}
</style>
