<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ApiError, createLabelsQRBatch } from '@/api'
import type { LabelsQRBatchItem } from '@/api'
import { useItemsStore, useLabelsStore, useLocationsStore } from '@/stores'
import LocationTree from '@/components/LocationTree.vue'

interface LabelPreset {
  readonly id: string
  readonly name: string
  readonly perSheet: number
  readonly columns: number
  readonly rows: number
  readonly labelWidthIn: number
  readonly labelHeightIn: number
  readonly pageMarginTopIn: number
  readonly pageMarginLeftIn: number
  readonly columnGapIn: number
  readonly rowGapIn: number
  readonly qrSizeIn: number
}

const LABEL_PRESETS: readonly LabelPreset[] = [
  {
    id: 'avery-5160',
    name: 'Avery 5160 — 30/sheet, 2.625" × 1"',
    perSheet: 30,
    columns: 3,
    rows: 10,
    labelWidthIn: 2.625,
    labelHeightIn: 1,
    pageMarginTopIn: 0.5,
    pageMarginLeftIn: 0.1875,
    columnGapIn: 0.125,
    rowGapIn: 0,
    qrSizeIn: 0.85,
  },
  {
    id: 'avery-5163',
    name: 'Avery 5163 — 10/sheet, 4" × 2"',
    perSheet: 10,
    columns: 2,
    rows: 5,
    labelWidthIn: 4,
    labelHeightIn: 2,
    pageMarginTopIn: 0.5,
    pageMarginLeftIn: 0.25,
    columnGapIn: 0,
    rowGapIn: 0,
    qrSizeIn: 1.7,
  },
]

const MAX_BATCH_SIZE = 200

const items = useItemsStore()
const locations = useLocationsStore()
const labels = useLabelsStore()

const search = ref('')
let searchTimer: ReturnType<typeof setTimeout> | undefined
const SEARCH_DEBOUNCE_MS = 250

function onSearchInput(): void {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void items.applyFilters({ q: search.value }), SEARCH_DEBOUNCE_MS)
}

function onSearchSubmit(): void {
  clearTimeout(searchTimer)
  void items.applyFilters({ q: search.value })
}

const selected = ref<Set<string>>(new Set())
const selectedCount = computed(() => selected.value.size)
const overLimit = computed(() => selectedCount.value > MAX_BATCH_SIZE)

function toggleSelect(id: string): void {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

function selectPage(): void {
  const next = new Set(selected.value)
  for (const item of items.items) next.add(item.id)
  selected.value = next
}

function clearSelection(): void {
  selected.value = new Set()
  sheetItems.value = []
  generated.value = false
}

const presetID = ref(LABEL_PRESETS[0]!.id)
const preset = computed(
  (): LabelPreset => LABEL_PRESETS.find((p) => p.id === presetID.value) ?? LABEL_PRESETS[0]!,
)

const busy = ref(false)
const error = ref('')
const generated = ref(false)
const requestedCount = ref(0)
const sheetItems = ref<readonly LabelsQRBatchItem[]>([])

const omittedCount = computed(() => Math.max(0, requestedCount.value - sheetItems.value.length))

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

async function generate(): Promise<void> {
  if (selectedCount.value === 0 || overLimit.value) return
  busy.value = true
  error.value = ''
  try {
    const ids = Array.from(selected.value)
    requestedCount.value = ids.length
    const response = await createLabelsQRBatch(ids)
    sheetItems.value = response.items
    generated.value = true
  } catch (err) {
    error.value = describe(err)
  } finally {
    busy.value = false
  }
}

const pages = computed<readonly (readonly LabelsQRBatchItem[])[]>(() => {
  const perSheet = preset.value.perSheet
  const chunks: LabelsQRBatchItem[][] = []
  for (let i = 0; i < sheetItems.value.length; i += perSheet) {
    chunks.push(sheetItems.value.slice(i, i + perSheet))
  }
  return chunks
})

const sheetStyle = computed(() => {
  const p = preset.value
  return {
    '--sheet-columns': String(p.columns),
    '--sheet-rows': String(p.rows),
    '--cell-w': `${p.labelWidthIn}in`,
    '--cell-h': `${p.labelHeightIn}in`,
    '--col-gap': `${p.columnGapIn}in`,
    '--row-gap': `${p.rowGapIn}in`,
    '--margin-top': `${p.pageMarginTopIn}in`,
    '--margin-left': `${p.pageMarginLeftIn}in`,
    '--qr-size': `${p.qrSizeIn}in`,
  }
})

function print(): void {
  window.print()
}

onMounted(async () => {
  await Promise.allSettled([items.refresh(), locations.refresh(), labels.refresh()])
})
</script>

<template>
  <section class="print-labels v-stack">
    <h1 class="print-labels__title no-print">Print labels</h1>

    <p v-if="error" class="alert no-print" role="alert">{{ error }}</p>

    <div class="print-labels__picker no-print">
      <aside class="print-labels__rail v-stack">
        <section class="v-stack">
          <h2 class="print-labels__rail-title">Locations</h2>
          <p v-if="locations.tree.length === 0" class="muted">No locations yet.</p>
          <LocationTree
            v-else
            :nodes="locations.tree"
            :selected="items.locationID"
            @select="items.applyFilters({ locationID: $event })"
          />
        </section>

        <section class="v-stack">
          <h2 class="print-labels__rail-title">Labels</h2>
          <p v-if="labels.labels.length === 0" class="muted">No labels yet.</p>
          <ul v-else class="print-labels__labelchips">
            <li v-for="label in labels.labels" :key="label.id">
              <button
                type="button"
                class="chip print-labels__labelchip"
                :class="{ 'print-labels__labelchip--on': items.labelIDs.includes(label.id) }"
                :aria-pressed="items.labelIDs.includes(label.id)"
                @click="items.toggleLabel(label.id)"
              >
                <span class="chip__swatch" :style="{ background: label.color }"></span>
                {{ label.name }}
              </button>
            </li>
          </ul>
        </section>
      </aside>

      <section class="print-labels__main v-stack">
        <form class="print-labels__search" role="search" @submit.prevent="onSearchSubmit">
          <label class="sr-only" for="label-item-search">Search items</label>
          <input
            id="label-item-search"
            v-model="search"
            class="input"
            type="search"
            placeholder="Search items…"
            @input="onSearchInput"
          />
        </form>

        <p v-if="items.loading" class="muted">Loading…</p>
        <template v-else>
          <p v-if="items.items.length === 0" class="empty">No items match these filters.</p>
          <ul v-else class="print-labels__list">
            <li v-for="item in items.items" :key="item.id">
              <label class="print-labels__row h-stack">
                <input
                  type="checkbox"
                  :checked="selected.has(item.id)"
                  @change="toggleSelect(item.id)"
                />
                <span class="print-labels__row-name">{{ item.name }}</span>
              </label>
            </li>
          </ul>

          <div v-if="items.hasPreviousPage || items.hasNextPage" class="h-stack">
            <button
              class="btn btn--small"
              type="button"
              :disabled="!items.hasPreviousPage"
              @click="items.previousPage()"
            >
              Previous
            </button>
            <button
              class="btn btn--small"
              type="button"
              :disabled="!items.hasNextPage"
              @click="items.nextPage()"
            >
              Next
            </button>
          </div>
        </template>

        <div class="h-stack print-labels__selection-summary">
          <button
            class="btn btn--small"
            type="button"
            :disabled="items.items.length === 0"
            @click="selectPage"
          >
            Select page
          </button>
          <button
            class="btn btn--ghost btn--small"
            type="button"
            :disabled="selectedCount === 0"
            @click="clearSelection"
          >
            Clear selection
          </button>
          <span class="muted" role="status">
            {{ selectedCount }} selected
            <template v-if="overLimit"
              >— over the {{ MAX_BATCH_SIZE }}-label limit per sheet run</template
            >
          </span>
        </div>
      </section>
    </div>

    <div class="print-labels__sheet-controls h-stack no-print">
      <div class="field print-labels__preset-field">
        <label for="label-preset">Label stock</label>
        <select id="label-preset" v-model="presetID" class="input">
          <option v-for="p in LABEL_PRESETS" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
      </div>

      <button
        class="btn btn--primary"
        type="button"
        :disabled="busy || selectedCount === 0 || overLimit"
        @click="generate"
      >
        {{ busy ? 'Generating…' : 'Generate sheet' }}
      </button>

      <button class="btn" type="button" :disabled="pages.length === 0" @click="print">Print</button>

      <p v-if="generated && omittedCount > 0" class="muted print-labels__omitted" role="status">
        {{ omittedCount }} selected item{{ omittedCount === 1 ? '' : 's' }} could not be printed —
        {{ omittedCount === 1 ? 'it' : 'they' }} may have been deleted since being selected.
      </p>
      <p v-else-if="generated && sheetItems.length === 0" class="muted">Nothing to print.</p>
    </div>

    <section v-if="pages.length > 0" class="print-labels__preview">
      <div v-for="(page, pageIndex) in pages" :key="pageIndex" class="sheet" :style="sheetStyle">
        <div v-for="entry in page" :key="entry.id" class="sheet__cell">
          <!-- eslint-disable-next-line vue/no-v-html -- `qr_svg` is server-rendered, self-contained
               (no embedded scripts, per openapi.yaml's own operation doc) SVG from this SPA's own
               same-origin, session-authenticated API — the same trust boundary every other value
               this view renders already crosses, not user-supplied HTML. -->
          <div class="sheet__qr" v-html="entry.qr_svg"></div>
          <div class="sheet__text">
            <div class="sheet__name">{{ entry.name }}</div>
            <div class="sheet__code">{{ entry.short_code }}</div>
          </div>
        </div>
      </div>
    </section>
  </section>
</template>

<style scoped>
.print-labels__title {
  margin: 0;
  font-size: 1.25rem;
}

.print-labels__picker {
  display: grid;
  grid-template-columns: 15rem 1fr;
  gap: 1.5rem;
  align-items: start;
}

@media (max-width: 44rem) {
  .print-labels__picker {
    grid-template-columns: 1fr;
  }
}

.print-labels__rail {
  gap: 1.5rem;
  background: var(--hho-surface);
  border: 1px solid var(--hho-border);
  border-radius: var(--hho-radius);
  padding: var(--hho-gap);
}

.print-labels__rail-title {
  margin: 0 0 0.5rem;
  font-size: 0.8125rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--hho-muted);
}

.print-labels__labelchips {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.print-labels__labelchip {
  cursor: pointer;
  font: inherit;
  font-size: 0.8125rem;
  color: inherit;
}

.print-labels__labelchip--on {
  border-color: var(--hho-accent);
  background: var(--hho-accent);
  color: var(--hho-accent-fg);
}

.print-labels__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.print-labels__row {
  padding: 0.4rem 0.5rem;
  border: 1px solid var(--hho-border);
  border-radius: var(--hho-radius);
  cursor: pointer;
}

.print-labels__row-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.print-labels__selection-summary {
  flex-wrap: wrap;
}

.print-labels__sheet-controls {
  flex-wrap: wrap;
  background: var(--hho-surface);
  border: 1px solid var(--hho-border);
  border-radius: var(--hho-radius);
  padding: var(--hho-gap);
}

.print-labels__preset-field {
  min-width: 16rem;
}

.print-labels__omitted {
  width: 100%;
}

.print-labels__preview {
  overflow-x: auto;
}

.sheet {
  width: 8.5in;
  height: 11in;
  box-sizing: border-box;
  padding: var(--margin-top) 0 0 var(--margin-left);
  display: grid;
  grid-template-columns: repeat(var(--sheet-columns), var(--cell-w));
  grid-template-rows: repeat(var(--sheet-rows), var(--cell-h));
  column-gap: var(--col-gap);
  row-gap: var(--row-gap);
  background: var(--hho-bg);
  margin: 0 auto 1rem;
  border: 1px solid var(--hho-border);
  break-after: page;
}

.sheet:last-child {
  break-after: auto;
}

.sheet__cell {
  display: flex;
  align-items: center;
  gap: 0.1in;
  padding: 0.05in;
  overflow: hidden;
  box-sizing: border-box;
}

.sheet__qr {
  flex: none;
  width: var(--qr-size);
  height: var(--qr-size);
}

.sheet__qr :deep(svg) {
  width: 100%;
  height: 100%;
  display: block;
}

.sheet__text {
  min-width: 0;
  font-size: 0.6875rem;
  line-height: 1.25;
}

.sheet__name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sheet__code {
  color: var(--hho-muted);
  font-variant-numeric: tabular-nums;
}
</style>

<style>
@page {
  size: letter;
  margin: 0;
}

@media print {
  .no-print {
    display: none !important;
  }

  .print-labels__preview {
    overflow: visible;
  }

  .sheet {
    border: none;
    margin: 0;
  }
}
</style>
