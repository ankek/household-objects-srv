<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  ApiError,
  exportBomCSVURL,
  getReportItemCountByLocation,
  getReportPurchases,
  getReportValuation,
  getReportWarrantyExpiring,
  reportItemCountByLocationCSVURL,
  reportPurchasesCSVURL,
  reportValuationCSVURL,
  reportWarrantyExpiringCSVURL,
} from '@/api'
import type {
  ReportGroupBy,
  ReportLocationItemCountRow,
  ReportPurchaseRow,
  ReportValuationRow,
  ReportWarrantyExpiringRow,
} from '@/api'
import { useItemsStore, useLabelsStore, useLocationsStore } from '@/stores'

type TabId = 'valuation' | 'warranty' | 'purchases' | 'item-count' | 'bom'

const TABS: ReadonlyArray<{ id: TabId; label: string }> = [
  { id: 'valuation', label: 'Valuation' },
  { id: 'warranty', label: 'Warranty expiring' },
  { id: 'purchases', label: 'Purchases' },
  { id: 'item-count', label: 'Item count by location' },
  { id: 'bom', label: 'Bill of materials' },
]

const activeTab = ref<TabId>('valuation')

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

const valuationGroupBy = ref<ReportGroupBy>('location')
const valuationRows = ref<readonly ReportValuationRow[]>([])
const valuationLoading = ref(false)
const valuationError = ref('')

const valuationCSVHref = computed(() => reportValuationCSVURL(valuationGroupBy.value))

function valuationGroupName(row: ReportValuationRow): string {
  if (row.group_key === '') {
    return valuationGroupBy.value === 'location' ? 'Unassigned' : row.group_label || 'Unlabelled'
  }
  return row.group_label
}

async function loadValuation(): Promise<void> {
  valuationLoading.value = true
  valuationError.value = ''
  try {
    valuationRows.value = (await getReportValuation(valuationGroupBy.value)).rows
  } catch (err) {
    valuationError.value = describe(err)
  } finally {
    valuationLoading.value = false
  }
}

const DEFAULT_WITHIN_DAYS = 30
const withinDays = ref(DEFAULT_WITHIN_DAYS)
const warrantyRows = ref<readonly ReportWarrantyExpiringRow[]>([])
const warrantyLoading = ref(false)
const warrantyError = ref('')

const withinDaysValid = computed(() => Number.isInteger(withinDays.value) && withinDays.value >= 0)
const warrantyCSVHref = computed(() =>
  withinDaysValid.value ? reportWarrantyExpiringCSVURL(withinDays.value) : undefined,
)

async function loadWarranty(): Promise<void> {
  if (!withinDaysValid.value) return
  warrantyLoading.value = true
  warrantyError.value = ''
  try {
    warrantyRows.value = (await getReportWarrantyExpiring(withinDays.value)).rows
  } catch (err) {
    warrantyError.value = describe(err)
  } finally {
    warrantyLoading.value = false
  }
}

const fromDate = ref('')
const toDate = ref('')
const purchasesRows = ref<readonly ReportPurchaseRow[]>([])
const purchasesLoading = ref(false)
const purchasesError = ref('')
const purchasesRun = ref(false)

const purchasesRangeValid = computed(
  () => fromDate.value !== '' && toDate.value !== '' && fromDate.value <= toDate.value,
)
const purchasesCSVHref = computed(() =>
  purchasesRangeValid.value ? reportPurchasesCSVURL(fromDate.value, toDate.value) : undefined,
)

async function loadPurchases(): Promise<void> {
  if (!purchasesRangeValid.value) return
  purchasesLoading.value = true
  purchasesError.value = ''
  purchasesRun.value = true
  try {
    purchasesRows.value = (await getReportPurchases(fromDate.value, toDate.value)).rows
  } catch (err) {
    purchasesError.value = describe(err)
  } finally {
    purchasesLoading.value = false
  }
}

const itemCountRows = ref<readonly ReportLocationItemCountRow[]>([])
const itemCountLoading = ref(false)
const itemCountError = ref('')

const itemCountCSVHref = reportItemCountByLocationCSVURL()

async function loadItemCount(): Promise<void> {
  itemCountLoading.value = true
  itemCountError.value = ''
  try {
    itemCountRows.value = (await getReportItemCountByLocation()).rows
  } catch (err) {
    itemCountError.value = describe(err)
  } finally {
    itemCountLoading.value = false
  }
}

type BomMode = 'location' | 'label' | 'item' | null

const locations = useLocationsStore()
const labels = useLabelsStore()
const items = useItemsStore()

const bomMode = ref<BomMode>(null)
const bomLocationIDs = ref<Set<string>>(new Set())
const bomLabelIDs = ref<Set<string>>(new Set())
const bomItemIDs = ref<Set<string>>(new Set())

function chooseBomMode(next: Exclude<BomMode, null>): void {
  if (bomMode.value === next) return
  bomMode.value = next
  bomLocationIDs.value = new Set()
  bomLabelIDs.value = new Set()
  bomItemIDs.value = new Set()
}

function toggleBomLocation(id: string): void {
  const next = new Set(bomLocationIDs.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  bomLocationIDs.value = next
}

function toggleBomLabel(id: string): void {
  const next = new Set(bomLabelIDs.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  bomLabelIDs.value = next
}

function toggleBomItem(id: string): void {
  const next = new Set(bomItemIDs.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  bomItemIDs.value = next
}

const bomItemSearch = ref('')
let bomItemSearchTimer: ReturnType<typeof setTimeout> | undefined
const SEARCH_DEBOUNCE_MS = 250

function onBomItemSearchInput(): void {
  clearTimeout(bomItemSearchTimer)
  bomItemSearchTimer = setTimeout(
    () => void items.applyFilters({ q: bomItemSearch.value }),
    SEARCH_DEBOUNCE_MS,
  )
}

watch(bomMode, (mode) => {
  if (mode === 'item' && items.items.length === 0 && !items.loading) {
    void items.refresh()
  }
})

const bomExportHref = computed(() => {
  if (bomMode.value === 'location') {
    return exportBomCSVURL({ locationIds: Array.from(bomLocationIDs.value) })
  }
  if (bomMode.value === 'label') {
    return exportBomCSVURL({ labelIds: Array.from(bomLabelIDs.value) })
  }
  if (bomMode.value === 'item') {
    return exportBomCSVURL({ itemIds: Array.from(bomItemIDs.value) })
  }
  return exportBomCSVURL({})
})

const bomSelectedCount = computed(() => {
  if (bomMode.value === 'location') return bomLocationIDs.value.size
  if (bomMode.value === 'label') return bomLabelIDs.value.size
  if (bomMode.value === 'item') return bomItemIDs.value.size
  return 0
})

onMounted(async () => {
  await Promise.allSettled([
    loadValuation(),
    loadWarranty(),
    loadItemCount(),
    locations.refresh(),
    labels.refresh(),
  ])
})
</script>

<template>
  <section class="reports v-stack">
    <h1 class="reports__title">Reports</h1>

    <div class="reports__tabs" role="tablist" aria-label="Report type">
      <button
        v-for="tab in TABS"
        :key="tab.id"
        type="button"
        role="tab"
        class="btn reports__tab"
        :class="{ 'reports__tab--active': activeTab === tab.id }"
        :aria-selected="activeTab === tab.id"
        @click="activeTab = tab.id"
      >
        {{ tab.label }}
      </button>
    </div>

    <section
      v-if="activeTab === 'valuation'"
      class="card v-stack reports__panel"
      role="tabpanel"
      aria-label="Valuation"
    >
      <div class="h-stack reports__controls">
        <div class="field">
          <label for="valuation-group-by">Group by</label>
          <select
            id="valuation-group-by"
            v-model="valuationGroupBy"
            class="input"
            @change="loadValuation"
          >
            <option value="location">Location</option>
            <option value="label">Label</option>
          </select>
        </div>
        <a class="btn reports__csv-link" :href="valuationCSVHref" download>Download CSV</a>
      </div>

      <p v-if="valuationError" class="alert" role="alert">{{ valuationError }}</p>
      <p v-if="valuationLoading" class="muted">Loading…</p>
      <p v-else-if="valuationRows.length === 0" class="empty">No data for this report yet.</p>
      <table v-else class="reports__table">
        <thead>
          <tr>
            <th scope="col">{{ valuationGroupBy === 'location' ? 'Location' : 'Label' }}</th>
            <th scope="col">Items</th>
            <th scope="col">Total value</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in valuationRows" :key="row.group_key || '(unassigned)'">
            <td>{{ valuationGroupName(row) }}</td>
            <td>{{ row.item_count }}</td>
            <td>{{ (row.total_value_minor / 100).toFixed(2) }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section
      v-if="activeTab === 'warranty'"
      class="card v-stack reports__panel"
      role="tabpanel"
      aria-label="Warranty expiring"
    >
      <form class="h-stack reports__controls" @submit.prevent="loadWarranty">
        <div class="field">
          <label for="within-days">Within days</label>
          <input
            id="within-days"
            v-model.number="withinDays"
            class="input"
            type="number"
            min="0"
            step="1"
          />
        </div>
        <button class="btn btn--primary" type="submit" :disabled="!withinDaysValid">Run</button>
        <a
          class="btn reports__csv-link"
          :class="{ 'reports__csv-link--disabled': !warrantyCSVHref }"
          :href="warrantyCSVHref"
          :aria-disabled="!warrantyCSVHref"
          download
        >
          Download CSV
        </a>
      </form>
      <p v-if="!withinDaysValid" class="muted reports__hint">
        Enter a whole number of days, zero or more. Zero means "expiring today only", not unlimited.
      </p>

      <p v-if="warrantyError" class="alert" role="alert">{{ warrantyError }}</p>
      <p v-if="warrantyLoading" class="muted">Loading…</p>
      <p v-else-if="warrantyRows.length === 0" class="empty">
        Nothing expiring within this window.
      </p>
      <table v-else class="reports__table">
        <thead>
          <tr>
            <th scope="col">Item</th>
            <th scope="col">Expires on</th>
            <th scope="col">Days remaining</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in warrantyRows" :key="row.item_id">
            <td>{{ row.item_name }}</td>
            <td>{{ row.expires_on }}</td>
            <td>{{ row.days_remaining }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section
      v-if="activeTab === 'purchases'"
      class="card v-stack reports__panel"
      role="tabpanel"
      aria-label="Purchases"
    >
      <form class="h-stack reports__controls" @submit.prevent="loadPurchases">
        <div class="field">
          <label for="purchases-from">From</label>
          <input id="purchases-from" v-model="fromDate" class="input" type="date" />
        </div>
        <div class="field">
          <label for="purchases-to">To</label>
          <input id="purchases-to" v-model="toDate" class="input" type="date" />
        </div>
        <button class="btn btn--primary" type="submit" :disabled="!purchasesRangeValid">Run</button>
        <a
          class="btn reports__csv-link"
          :class="{ 'reports__csv-link--disabled': !purchasesCSVHref }"
          :href="purchasesCSVHref"
          :aria-disabled="!purchasesCSVHref"
          download
        >
          Download CSV
        </a>
      </form>
      <p v-if="!purchasesRangeValid" class="muted reports__hint">
        Both dates are required, and "From" must not be after "To".
      </p>

      <p v-if="purchasesError" class="alert" role="alert">{{ purchasesError }}</p>
      <p v-if="purchasesLoading" class="muted">Loading…</p>
      <p v-else-if="purchasesRun && purchasesRows.length === 0" class="empty">
        Nothing purchased in this range.
      </p>
      <table v-else-if="purchasesRows.length > 0" class="reports__table">
        <thead>
          <tr>
            <th scope="col">Item</th>
            <th scope="col">Purchased on</th>
            <th scope="col">Vendor</th>
            <th scope="col">Price</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in purchasesRows" :key="row.item_id">
            <td>{{ row.item_name }}</td>
            <td>{{ row.purchased_on }}</td>
            <td>{{ row.vendor || '—' }}</td>
            <td>{{ (row.purchase_price_minor / 100).toFixed(2) }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section
      v-if="activeTab === 'item-count'"
      class="card v-stack reports__panel"
      role="tabpanel"
      aria-label="Item count by location"
    >
      <div class="h-stack reports__controls">
        <p class="muted reports__hint">
          Counts include every sub-location's items, not only what is placed directly here.
        </p>
        <a class="btn reports__csv-link" :href="itemCountCSVHref" download>Download CSV</a>
      </div>

      <p v-if="itemCountError" class="alert" role="alert">{{ itemCountError }}</p>
      <p v-if="itemCountLoading" class="muted">Loading…</p>
      <p v-else-if="itemCountRows.length === 0" class="empty">No locations yet.</p>
      <table v-else class="reports__table">
        <thead>
          <tr>
            <th scope="col">Location</th>
            <th scope="col">Items (including sub-locations)</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in itemCountRows" :key="row.location_id">
            <td>{{ row.location_name }}</td>
            <td>{{ row.item_count }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section
      v-if="activeTab === 'bom'"
      class="card v-stack reports__panel"
      role="tabpanel"
      aria-label="Bill of materials"
    >
      <p class="muted">
        Choose exactly one way to select items — by location, by label, or by picking items
        individually. Choosing a different one clears the previous selection. Selecting nothing
        exports an empty sheet.
      </p>

      <div class="reports__bom-modes" role="radiogroup" aria-label="Selection mode">
        <label class="h-stack">
          <input
            type="radio"
            name="bom-mode"
            :checked="bomMode === 'location'"
            @change="chooseBomMode('location')"
          />
          <span>By location</span>
        </label>
        <label class="h-stack">
          <input
            type="radio"
            name="bom-mode"
            :checked="bomMode === 'label'"
            @change="chooseBomMode('label')"
          />
          <span>By label</span>
        </label>
        <label class="h-stack">
          <input
            type="radio"
            name="bom-mode"
            :checked="bomMode === 'item'"
            @change="chooseBomMode('item')"
          />
          <span>By specific items</span>
        </label>
      </div>

      <section v-if="bomMode === 'location'" class="v-stack">
        <p v-if="locations.flat.length === 0" class="muted">No locations yet.</p>
        <ul v-else class="reports__bom-list">
          <li v-for="location in locations.flat" :key="location.id">
            <label class="h-stack">
              <input
                type="checkbox"
                :checked="bomLocationIDs.has(location.id)"
                @change="toggleBomLocation(location.id)"
              />
              <span>{{ location.name }}</span>
            </label>
          </li>
        </ul>
      </section>

      <section v-else-if="bomMode === 'label'" class="v-stack">
        <p v-if="labels.labels.length === 0" class="muted">No labels yet.</p>
        <ul v-else class="reports__bom-labelchips">
          <li v-for="label in labels.labels" :key="label.id">
            <button
              type="button"
              class="chip reports__bom-labelchip"
              :class="{ 'reports__bom-labelchip--on': bomLabelIDs.has(label.id) }"
              :aria-pressed="bomLabelIDs.has(label.id)"
              @click="toggleBomLabel(label.id)"
            >
              <span class="chip__swatch" :style="{ background: label.color }"></span>
              {{ label.name }}
            </button>
          </li>
        </ul>
        <p v-if="bomLabelIDs.size > 1" class="muted reports__hint">
          Selecting items with any of these {{ bomLabelIDs.size }} labels.
        </p>
      </section>

      <section v-else-if="bomMode === 'item'" class="v-stack">
        <form class="reports__bom-search" role="search" @submit.prevent>
          <label class="sr-only" for="bom-item-search">Search items</label>
          <input
            id="bom-item-search"
            v-model="bomItemSearch"
            class="input"
            type="search"
            placeholder="Search items…"
            @input="onBomItemSearchInput"
          />
        </form>

        <p v-if="items.loading" class="muted">Loading…</p>
        <template v-else>
          <p v-if="items.items.length === 0" class="empty">No items match this search.</p>
          <ul v-else class="reports__bom-list">
            <li v-for="item in items.items" :key="item.id">
              <label class="h-stack">
                <input
                  type="checkbox"
                  :checked="bomItemIDs.has(item.id)"
                  @change="toggleBomItem(item.id)"
                />
                <span>{{ item.name }}</span>
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
      </section>

      <div class="h-stack reports__bom-footer">
        <span class="muted" role="status">
          {{ bomMode === null ? 'No selection mode chosen' : `${bomSelectedCount} selected` }}
        </span>
        <a class="btn btn--primary reports__csv-link" :href="bomExportHref" download>
          Download CSV
        </a>
      </div>
    </section>
  </section>
</template>

<style scoped>
.reports {
  max-width: 56rem;
}

.reports__title {
  margin: 0;
  font-size: 1.25rem;
}

.reports__tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.reports__tab--active {
  background: var(--hho-accent);
  color: var(--hho-accent-fg);
  border-color: var(--hho-accent);
}

.reports__panel {
  gap: 0.75rem;
}

.reports__controls {
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
}

.reports__hint {
  font-size: 0.8125rem;
  margin: 0;
}

.reports__csv-link {
  text-decoration: none;
  white-space: nowrap;
}

.reports__csv-link--disabled {
  pointer-events: none;
  opacity: 0.55;
}

.reports__table {
  width: 100%;
  border-collapse: collapse;
}

.reports__table th,
.reports__table td {
  text-align: left;
  padding: 0.4rem 0.5rem;
  border-bottom: 1px solid var(--hho-border);
}

.reports__bom-modes {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
}

.reports__bom-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  max-height: 16rem;
  overflow-y: auto;
}

.reports__bom-labelchips {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.reports__bom-labelchip {
  cursor: pointer;
  font: inherit;
  font-size: 0.8125rem;
  color: inherit;
}

.reports__bom-labelchip--on {
  border-color: var(--hho-accent);
  background: var(--hho-accent);
  color: var(--hho-accent-fg);
}

.reports__bom-search {
  max-width: 20rem;
}

.reports__bom-footer {
  justify-content: space-between;
}
</style>
