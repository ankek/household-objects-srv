import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { API_BASE_URL } from '@/api/client'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import ImportView from '@/views/ImportView.vue'

const UPLOAD = 'POST /api/v1/import/native/upload'
const IMPORT_ID = '0191b1b2-3c4d-7c9a-8f2e-1a2b3c4d5e6f'
const PREVIEW = `GET /api/v1/import/${IMPORT_ID}/preview`
const COMMIT = `POST /api/v1/import/${IMPORT_ID}/commit`

afterEach(() => {
  vi.unstubAllGlobals()
})

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

function mountImport() {
  return mount(ImportView)
}

async function setFile(
  wrapper: ReturnType<typeof mountImport>,
  name = 'export.csv',
  contents = 'x',
): Promise<void> {
  const input = wrapper.get<HTMLInputElement>('#import-file')
  const file = new File([contents], name, { type: 'text/csv' })
  Object.defineProperty(input.element, 'files', { value: [file] })
  await input.trigger('change')
  await wrapper.vm.$nextTick()
}

async function uploadTo(
  wrapper: ReturnType<typeof mountImport>,
  routes: Routes = {},
): Promise<ReturnType<typeof stubRoutedFetch>> {
  const fetchMock = stubRoutedFetch({
    [UPLOAD]: () => jsonResponse(201, { import_id: IMPORT_ID }),
    [PREVIEW]: () =>
      jsonResponse(200, {
        import_id: IMPORT_ID,
        rows: [],
        summary: { create: 0, update: 0, unchanged: 0, error: 0 },
      }),
    ...routes,
  })
  await setFile(wrapper)
  await wrapper.get('button.btn--primary').trigger('click')
  await flush()
  await wrapper.vm.$nextTick()
  return fetchMock
}

describe('ImportView export link (FR-050)', () => {
  it('links to the native CSV export as a plain download, member-readable and not role-gated', () => {
    const wrapper = mountImport()

    const link = wrapper.get('a[download]')
    expect(link.attributes('href')).toBe(`${API_BASE_URL}/export/items.csv`)
    expect(link.attributes('download')).toBeDefined()
  })
})

describe('ImportView upload step', () => {
  it('disables the submit button until a file is chosen', async () => {
    const wrapper = mountImport()

    const submit = wrapper.get('button.btn--primary')
    expect(submit.attributes('disabled')).toBeDefined()

    await setFile(wrapper)
    await wrapper.vm.$nextTick()
    expect(submit.attributes('disabled')).toBeUndefined()
  })

  it('uploads the file as a raw text/csv body (not multipart) and moves to preview on success', async () => {
    const wrapper = mountImport()
    const fetchMock = await uploadTo(wrapper)

    const uploadCall = fetchMock.mock.calls.find(
      ([input]) => input === '/api/v1/import/native/upload',
    )!
    const init = uploadCall[1]!
    expect(init.method).toBe('POST')
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('text/csv')
    expect(init.body).toBe('x')

    expect(
      fetchMock.mock.calls.some(([input]) => input === `/api/v1/import/${IMPORT_ID}/preview`),
    ).toBe(true)
    expect(wrapper.find('[aria-label="Review the staged import"]').exists()).toBe(true)
  })

  it('shows the server detail on a 400 (malformed CSV) and stays on the upload step', async () => {
    const wrapper = mountImport()
    stubRoutedFetch({
      [UPLOAD]: () =>
        jsonResponse(400, { title: 'Bad Request', detail: 'unrecognised header row' }),
    })
    await setFile(wrapper)
    await wrapper.get('button.btn--primary').trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('.alert').text()).toContain('unrecognised header row')
    expect(wrapper.find('[aria-label="Upload a CSV file"]').exists()).toBe(true)
  })

  it('shows a plain-language message on a 413 (over the 10 MiB cap), which carries no detail', async () => {
    const wrapper = mountImport()
    stubRoutedFetch({
      [UPLOAD]: () => jsonResponse(413, { title: 'Payload Too Large' }),
    })
    await setFile(wrapper)
    await wrapper.get('button.btn--primary').trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('.alert').text()).toContain('10 MiB upload limit')
  })
})

describe('ImportView preview step', () => {
  function previewRoutes(overrides: Routes = {}): Routes {
    return {
      [UPLOAD]: () => jsonResponse(201, { import_id: IMPORT_ID }),
      [PREVIEW]: () =>
        jsonResponse(200, {
          import_id: IMPORT_ID,
          rows: [
            { line: 2, action: 'create', name: 'Cordless drill' },
            {
              line: 3,
              action: 'update',
              item_id: 'item-1',
              name: 'Bench vice',
              changes: ['quantity', 'location_id'],
            },
            { line: 4, action: 'unchanged', item_id: 'item-2', name: 'Shop vac' },
            {
              line: 5,
              action: 'error',
              name: 'Missing quantity',
              errors: [
                { line: 5, column: 'quantity', message: 'quantity must be a non-negative integer' },
              ],
            },
          ],
          summary: { create: 1, update: 1, unchanged: 1, error: 1 },
        }),
      ...overrides,
    }
  }

  it('renders every row with its own classification, changes and error detail', async () => {
    const wrapper = mountImport()
    await uploadTo(wrapper, previewRoutes())

    expect(wrapper.text()).toContain('Create: 1 · Update: 1 · Unchanged: 1 · Error: 1')

    const rows = wrapper.findAll('.import__table tbody tr')
    expect(rows).toHaveLength(4)
    expect(rows[0]!.text()).toContain('Cordless drill')
    expect(rows[0]!.text()).toContain('Create')
    expect(rows[1]!.text()).toContain('Changed: quantity, location_id')
    expect(rows[3]!.text()).toContain('quantity: quantity must be a non-negative integer')
  })

  it('disables Commit and explains why while any row classifies error', async () => {
    const wrapper = mountImport()
    await uploadTo(wrapper, previewRoutes())

    const commit = wrapper.findAll('button').find((b) => b.text().startsWith('Commit'))!
    expect(commit.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('cannot be committed (all-or-nothing)')
  })

  it('Cancel abandons the wizard state and returns to the upload step, with no DELETE call', async () => {
    const wrapper = mountImport()
    const fetchMock = await uploadTo(wrapper, previewRoutes())

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Cancel')!
      .trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[aria-label="Upload a CSV file"]').exists()).toBe(true)
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE')).toBe(false)
  })

  it('commits successfully and shows the per-action counts and created item lines', async () => {
    const wrapper = mountImport()
    await uploadTo(
      wrapper,
      previewRoutes({
        [PREVIEW]: () =>
          jsonResponse(200, {
            import_id: IMPORT_ID,
            rows: [{ line: 2, action: 'create', name: 'Cordless drill' }],
            summary: { create: 1, update: 0, unchanged: 0, error: 0 },
          }),
        [COMMIT]: () =>
          jsonResponse(200, {
            import_id: IMPORT_ID,
            summary: { created: 1, updated: 0, unchanged: 0 },
            created_items: [{ line: 2, item_id: 'item-9' }],
          }),
      }),
    )

    await wrapper
      .findAll('button')
      .find((b) => b.text().startsWith('Commit'))!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[aria-label="Import complete"]').text()).toContain(
      'Created: 1 · Updated: 0 · Unchanged: 0',
    )
    expect(wrapper.text()).toContain('Line 2 → new item item-9')

    await wrapper.get('button.btn--primary').trigger('click')
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[aria-label="Upload a CSV file"]').exists()).toBe(true)
  })

  it('a 422 on commit refreshes the rows from the rejection body (not a flat detail string)', async () => {
    const wrapper = mountImport()
    await uploadTo(
      wrapper,
      previewRoutes({
        [PREVIEW]: () =>
          jsonResponse(200, {
            import_id: IMPORT_ID,
            rows: [{ line: 2, action: 'create', name: 'Cordless drill' }],
            summary: { create: 1, update: 0, unchanged: 0, error: 0 },
          }),
        [COMMIT]: () =>
          jsonResponse(422, {
            import_id: IMPORT_ID,
            rows: [
              {
                line: 2,
                action: 'error',
                name: 'Cordless drill',
                errors: [{ line: 2, message: 'location_id no longer exists' }],
              },
            ],
            summary: { create: 0, update: 0, unchanged: 0, error: 1 },
          }),
      }),
    )

    await wrapper
      .findAll('button')
      .find((b) => b.text().startsWith('Commit'))!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[aria-label="Review the staged import"]').exists()).toBe(true)
    expect(wrapper.get('.alert').text()).toContain('row errors and cannot be committed')
    expect(wrapper.text()).toContain('location_id no longer exists')
    expect(wrapper.text()).toContain('Create: 0 · Update: 0 · Unchanged: 0 · Error: 1')
  })
})

describe('ImportView network failure and empty-state handling', () => {
  it('shows a generic message when the upload request itself fails (not an ApiError)', async () => {
    const wrapper = mountImport()
    stubRoutedFetch({
      [UPLOAD]: () => Promise.reject(new Error('network down')),
    })
    await setFile(wrapper)
    await wrapper.get('button.btn--primary').trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('.alert').text()).toContain('Could not reach the server')
    expect(wrapper.find('[aria-label="Upload a CSV file"]').exists()).toBe(true)
  })

  it('shows the previewError alert (rather than crashing) when the preview GET fails after upload', async () => {
    const wrapper = mountImport()
    const fetchMock = stubRoutedFetch({
      [UPLOAD]: () => jsonResponse(201, { import_id: IMPORT_ID }),
      [PREVIEW]: () => jsonResponse(500, { title: 'Internal Server Error' }),
    })
    await setFile(wrapper)
    await wrapper.get('button.btn--primary').trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[aria-label="Review the staged import"]').exists()).toBe(true)
    expect(wrapper.get('.alert').text()).toContain('Internal Server Error')
    expect(
      fetchMock.mock.calls.some(([input]) => input === `/api/v1/import/${IMPORT_ID}/preview`),
    ).toBe(true)
  })

  it('shows "No data rows in this file." and a zeroed summary when the staged file has no rows', async () => {
    const wrapper = mountImport()
    await uploadTo(wrapper, {
      [PREVIEW]: () =>
        jsonResponse(200, {
          import_id: IMPORT_ID,
          rows: [],
          summary: { create: 0, update: 0, unchanged: 0, error: 0 },
        }),
    })

    expect(wrapper.text()).toContain('Create: 0 · Update: 0 · Unchanged: 0 · Error: 0')
    expect(wrapper.text()).toContain('No data rows in this file.')
    expect(wrapper.find('.import__table').exists()).toBe(false)
  })

  it('keeps Commit disabled when the preview failed to load', async () => {
    const wrapper = mountImport()
    stubRoutedFetch({
      [UPLOAD]: () => jsonResponse(201, { import_id: IMPORT_ID }),
      [PREVIEW]: () => jsonResponse(500, { title: 'Internal Server Error' }),
    })
    await setFile(wrapper)
    await wrapper.get('button.btn--primary').trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    const commit = wrapper.findAll('button').find((b) => b.text().startsWith('Commit'))!
    expect(commit.attributes('disabled')).toBeDefined()
  })
})
