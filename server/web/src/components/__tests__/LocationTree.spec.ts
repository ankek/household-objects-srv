import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LocationTree from '@/components/LocationTree.vue'
import type { LocationTreeNode } from '@/api'

function node(overrides: Partial<LocationTreeNode> = {}): LocationTreeNode {
  return {
    id: 'loc-1',
    created_at: 0,
    updated_at: 0,
    version: 1,
    name: 'Garage',
    item_count: 0,
    total_item_count: 0,
    children: [],
    ...overrides,
  }
}

function threeLevels(): LocationTreeNode[] {
  return [
    node({
      id: 'garage',
      name: 'Garage',
      total_item_count: 12,
      children: [
        node({
          id: 'shelf',
          name: 'Shelf',
          total_item_count: 5,
          children: [node({ id: 'bin', name: 'Bin', total_item_count: 2 })],
        }),
      ],
    }),
  ]
}

describe('LocationTree recursive rendering', () => {
  it('renders one row per node across every depth, not just the top level', () => {
    const wrapper = mount(LocationTree, { props: { nodes: threeLevels() } })

    const names = wrapper.findAll('.tree__name').map((n) => n.text())
    expect(names).toEqual(['Garage', 'Shelf', 'Bin'])
  })

  it('nests a child inside its own <ul>, not as a sibling of the parent row', () => {
    const wrapper = mount(LocationTree, { props: { nodes: threeLevels() } })

    const outerList = wrapper.get('ul.tree')
    const outerItems = outerList.findAll(':scope > li')
    expect(outerItems).toHaveLength(1)

    const garageItem = outerItems[0]!
    expect(garageItem.get('.tree__name').text()).toBe('Garage')
    const nestedList = garageItem.get('ul.tree')
    expect(nestedList.findAll(':scope > li')).toHaveLength(1)
  })

  it('renders no nested <ul> at all for a leaf node', () => {
    const wrapper = mount(LocationTree, { props: { nodes: [node({ children: [] })] } })

    expect(wrapper.findAll('ul.tree')).toHaveLength(1)
  })

  it('shows the roll-up total, not a direct-only count, on the row', () => {
    const wrapper = mount(LocationTree, {
      props: { nodes: [node({ total_item_count: 12, item_count: 3 })] },
    })

    expect(wrapper.get('.tree__count').text()).toBe('12')
  })

  it('marks only the selected row, at whichever depth it sits', () => {
    const wrapper = mount(LocationTree, { props: { nodes: threeLevels(), selected: 'shelf' } })

    const rows = wrapper.findAll('.tree__row')
    const pressed = rows.filter((r) => r.attributes('aria-pressed') === 'true')
    expect(pressed).toHaveLength(1)
    expect(pressed[0]!.get('.tree__name').text()).toBe('Shelf')
    expect(pressed[0]!.classes()).toContain('tree__row--selected')
  })

  it('emits the clicked id when nothing was selected', async () => {
    const wrapper = mount(LocationTree, { props: { nodes: threeLevels() } })

    await wrapper.findAll('.tree__row')[1]!.trigger('click')
    expect(wrapper.emitted('select')).toEqual([['shelf']])
  })

  it('clicking the already-selected row emits undefined instead of reselecting it', async () => {
    const wrapper = mount(LocationTree, { props: { nodes: threeLevels(), selected: 'shelf' } })

    await wrapper.findAll('.tree__row')[1]!.trigger('click')
    expect(wrapper.emitted('select')).toEqual([[undefined]])
  })

  it('a click on a nested (child-level) row still reaches the outermost listener', async () => {
    const wrapper = mount(LocationTree, { props: { nodes: threeLevels(), selected: undefined } })

    await wrapper.findAll('.tree__row')[2]!.trigger('click')
    expect(wrapper.emitted('select')).toEqual([['bin']])
  })
})
