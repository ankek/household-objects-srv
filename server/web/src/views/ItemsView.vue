<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import LocationTree from '@/components/LocationTree.vue'
import { useItemsStore, useLabelsStore, useLocationsStore } from '@/stores'

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

function clearAll(): void {
  search.value = ''
  void items.clearFilters()
}

onMounted(async () => {
  await Promise.allSettled([items.refresh(), locations.refresh(), labels.refresh()])
})
</script>

<template>
  <div class="inventory">
    <aside class="inventory__rail">
      <section class="v-stack">
        <div class="h-stack inventory__rail-head">
          <h2 class="inventory__rail-title">Locations</h2>
          <RouterLink class="btn btn--ghost btn--small" to="/locations">Manage</RouterLink>
        </div>

        <p v-if="locations.tree.length === 0" class="muted inventory__hint">No locations yet.</p>
        <LocationTree
          v-else
          :nodes="locations.tree"
          :selected="items.locationID"
          @select="items.applyFilters({ locationID: $event })"
        />

        <label v-if="items.locationID" class="h-stack inventory__descendants">
          <input
            type="checkbox"
            :checked="items.descendants"
            @change="
              items.applyFilters({ descendants: ($event.target as HTMLInputElement).checked })
            "
          />
          <span class="muted">Include sub-locations</span>
        </label>
      </section>

      <section class="v-stack">
        <div class="h-stack inventory__rail-head">
          <h2 class="inventory__rail-title">Labels</h2>
          <RouterLink class="btn btn--ghost btn--small" to="/labels">Manage</RouterLink>
        </div>

        <p v-if="labels.labels.length === 0" class="muted inventory__hint">No labels yet.</p>
        <ul v-else class="inventory__labels">
          <li v-for="label in labels.labels" :key="label.id">
            <button
              type="button"
              class="chip inventory__label"
              :class="{ 'inventory__label--on': items.labelIDs.includes(label.id) }"
              :aria-pressed="items.labelIDs.includes(label.id)"
              @click="items.toggleLabel(label.id)"
            >
              <span class="chip__swatch" :style="{ background: label.color }"></span>
              {{ label.name }}
            </button>
          </li>
        </ul>
        <p v-if="items.labelIDs.length > 1" class="muted inventory__hint">
          Showing items with all {{ items.labelIDs.length }} labels.
        </p>
      </section>
    </aside>

    <section class="inventory__main v-stack">
      <div class="inventory__toolbar">
        <form class="inventory__search" role="search" @submit.prevent="onSearchSubmit">
          <label class="sr-only" for="item-search">Search items</label>
          <input
            id="item-search"
            v-model="search"
            class="input"
            type="search"
            placeholder="Search items…"
            @input="onSearchInput"
          />
        </form>
        <button v-if="items.hasFilters" class="btn" type="button" @click="clearAll">Clear</button>
        <RouterLink class="btn btn--primary" to="/items/new">Add item</RouterLink>
      </div>

      <p v-if="items.loading" class="muted">Loading…</p>

      <template v-else>
        <div v-if="items.items.length === 0" class="empty">
          <template v-if="items.hasFilters">
            <p>No items match these filters.</p>
            <button class="btn" type="button" @click="clearAll">Clear filters</button>
          </template>
          <template v-else>
            <p>Nothing in the inventory yet.</p>
            <RouterLink class="btn btn--primary" to="/items/new">Add the first item</RouterLink>
          </template>
        </div>

        <ul v-else class="inventory__list">
          <li v-for="item in items.items" :key="item.id">
            <RouterLink class="item" :to="`/items/${item.id}`">
              <span class="item__name">{{ item.name }}</span>
              <span v-if="item.description" class="item__description muted">
                {{ item.description }}
              </span>
              <span class="item__meta muted">
                <span v-if="item.location_id">
                  {{ locations.nameByID.get(item.location_id) ?? 'Unknown location' }}
                </span>
                <span v-else>Unplaced</span>
                <span v-if="item.quantity !== 1">· {{ item.quantity }}×</span>
              </span>
            </RouterLink>
          </li>
        </ul>

        <div v-if="items.hasPreviousPage || items.hasNextPage" class="h-stack inventory__pager">
          <button
            class="btn"
            type="button"
            :disabled="!items.hasPreviousPage"
            @click="items.previousPage()"
          >
            Previous
          </button>
          <button
            class="btn"
            type="button"
            :disabled="!items.hasNextPage"
            @click="items.nextPage()"
          >
            Next
          </button>
        </div>
      </template>
    </section>
  </div>
</template>

<style scoped>
.inventory {
  display: grid;
  grid-template-columns: 15rem 1fr;
  gap: 1.5rem;
  align-items: start;
}

@media (max-width: 44rem) {
  .inventory {
    grid-template-columns: 1fr;
  }
}

.inventory__rail {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
  background: var(--hho-surface);
  border: 1px solid var(--hho-border);
  border-radius: var(--hho-radius);
  padding: var(--hho-gap);
  position: sticky;
  top: 1rem;
}

@media (max-width: 44rem) {
  .inventory__rail {
    position: static;
  }
}

.inventory__rail-head {
  justify-content: space-between;
}

.inventory__rail-title {
  margin: 0;
  font-size: 0.8125rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--hho-muted);
}

.inventory__hint {
  font-size: 0.8125rem;
  margin: 0;
}

.inventory__descendants {
  font-size: 0.8125rem;
}

.inventory__labels {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.inventory__label {
  cursor: pointer;
  font: inherit;
  font-size: 0.8125rem;
  color: inherit;
}

.inventory__label--on {
  border-color: var(--hho-accent);
  background: var(--hho-accent);
  color: var(--hho-accent-fg);
}

.inventory__toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: center;
}

.inventory__search {
  flex: 1;
  min-width: 8rem;
}

.inventory__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.item {
  display: grid;
  gap: 0.15rem;
  padding: 0.75rem var(--hho-gap);
  border: 1px solid var(--hho-muted);
  border-radius: var(--hho-radius);
  text-decoration: none;
  color: inherit;
}

.item:hover {
  border-color: var(--hho-accent);
}

.item__name {
  font-weight: 600;
}

.item__description {
  font-size: 0.875rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item__meta {
  font-size: 0.8125rem;
  display: flex;
  gap: 0.35rem;
}

.inventory__pager {
  justify-content: flex-end;
}
</style>
