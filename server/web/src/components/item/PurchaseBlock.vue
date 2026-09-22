<script setup lang="ts">
import { computed, onMounted, reactive } from 'vue'
import { createItemPurchase, deleteItemPurchase, getItemPurchase, updateItemPurchase } from '@/api'
import type { PurchaseWrite } from '@/api'
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
  get: getItemPurchase,
  create: createItemPurchase,
  update: updateItemPurchase,
  remove: deleteItemPurchase,
})

const draft = reactive<{
  vendor: string
  purchased_on: string
  purchase_price_minor: number
  order_reference: string
  notes: string
}>({
  vendor: '',
  purchased_on: '',
  purchase_price_minor: 0,
  order_reference: '',
  notes: '',
})

function resetDraft(): void {
  draft.vendor = data.value?.vendor ?? ''
  draft.purchased_on = data.value?.purchased_on ?? ''
  draft.purchase_price_minor = data.value?.purchase_price_minor ?? 0
  draft.order_reference = data.value?.order_reference ?? ''
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
  const body: PurchaseWrite = { ...draft }
  await save(body)
}

async function removeBlock(): Promise<void> {
  if (!window.confirm('Remove the purchase details for this item?')) return
  await remove()
}

function formatMinor(minor: number): string {
  return (minor / 100).toFixed(2)
}

const summary = computed(() => {
  const purchase = data.value
  if (purchase === null) return ''
  const parts: string[] = [formatMinor(purchase.purchase_price_minor)]
  if (purchase.vendor) parts.push(`from ${purchase.vendor}`)
  if (purchase.purchased_on) parts.push(`on ${purchase.purchased_on}`)
  return parts.join(' ')
})

onMounted(load)
</script>

<template>
  <section class="card v-stack detail-block" data-testid="purchase-block">
    <h2 class="detail-block__title">Purchase</h2>

    <p v-if="loading" class="muted">Loading…</p>

    <template v-else>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>

      <div v-if="conflict" class="alert" role="alert">
        <p>Someone else changed the purchase details while you were editing them.</p>
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
        + Add purchase details
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
          <label for="purchase-vendor">Vendor</label>
          <input id="purchase-vendor" v-model="draft.vendor" class="input" :disabled="busy" />
        </div>
        <div class="h-stack detail-block__field-row">
          <div class="field">
            <label for="purchase-date">Purchased on</label>
            <input
              id="purchase-date"
              v-model="draft.purchased_on"
              class="input"
              type="date"
              :disabled="busy"
            />
          </div>
          <div class="field">
            <label for="purchase-price">Price</label>
            <input
              id="purchase-price"
              v-model.number="draft.purchase_price_minor"
              class="input"
              type="number"
              min="0"
              step="1"
              :disabled="busy"
            />
          </div>
        </div>
        <div class="field">
          <label for="purchase-order">Order reference</label>
          <input
            id="purchase-order"
            v-model="draft.order_reference"
            class="input"
            :disabled="busy"
          />
        </div>
        <div class="field">
          <label for="purchase-notes">Notes</label>
          <textarea
            id="purchase-notes"
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
