<script setup lang="ts">
import type { LocationTreeNode } from '@/api'

defineProps<{
  nodes: readonly LocationTreeNode[]
  selected?: string | undefined
}>()

const emit = defineEmits<{ select: [id: string | undefined] }>()

function toggle(id: string, selected: string | undefined): void {
  emit('select', id === selected ? undefined : id)
}
</script>

<template>
  <ul class="tree">
    <li v-for="node in nodes" :key="node.id">
      <button
        type="button"
        class="tree__row"
        :class="{ 'tree__row--selected': node.id === selected }"
        :aria-pressed="node.id === selected"
        @click="toggle(node.id, selected)"
      >
        <span class="tree__name">{{ node.name }}</span>
        <span
          class="tree__count muted"
          :title="`${node.total_item_count} item(s) including sub-locations`"
        >
          {{ node.total_item_count }}
        </span>
      </button>

      <LocationTree
        v-if="node.children.length > 0"
        :nodes="node.children"
        :selected="selected"
        @select="emit('select', $event)"
      />
    </li>
  </ul>
</template>

<style scoped>
.tree {
  list-style: none;
  margin: 0;
  padding: 0;
}

.tree .tree {
  padding-left: 0.75rem;
  border-left: 1px solid var(--hho-border);
  margin-left: 0.4rem;
}

.tree__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  width: 100%;
  font: inherit;
  color: inherit;
  text-align: left;
  background: none;
  border: none;
  border-radius: var(--hho-radius);
  padding: 0.25rem 0.4rem;
  cursor: pointer;
}

.tree__row:hover {
  background: var(--hho-bg);
}

.tree__row--selected {
  background: var(--hho-accent);
  color: var(--hho-accent-fg);
}

.tree__row--selected .tree__count {
  color: inherit;
}

.tree__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tree__count {
  font-variant-numeric: tabular-nums;
  font-size: 0.8125rem;
}
</style>
