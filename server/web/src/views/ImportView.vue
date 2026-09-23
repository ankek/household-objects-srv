<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  ApiError,
  getImportPreview,
  importNativeUpload,
  postImportCommit,
  type ImportCommitRejectedResponse,
  type ImportCommitResponse,
  type ImportPreviewResponse,
  type ImportPreviewRow,
  type ImportPreviewSummary,
} from '@/api'
import { API_BASE_URL } from '@/api/client'

type WizardStep = 'upload' | 'preview' | 'result'

const step = ref<WizardStep>('upload')

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

const file = ref<File | null>(null)
const uploadBusy = ref(false)
const uploadError = ref('')
const importID = ref('')

function onFileChange(event: Event): void {
  const input = event.target as HTMLInputElement
  file.value = input.files?.[0] ?? null
}

function describeUploadError(err: unknown): string {
  if (err instanceof ApiError && err.status === 413) {
    return 'This file is larger than the 10 MiB upload limit.'
  }
  if (err instanceof ApiError && err.status === 400) {
    return err.problem?.detail
      ? err.problem.detail
      : 'This file was not recognised as a well-formed native HHO CSV export.'
  }
  return describe(err)
}

async function upload(): Promise<void> {
  if (!file.value) return
  uploadBusy.value = true
  uploadError.value = ''
  try {
    const csv = await file.value.text()
    const response = await importNativeUpload(csv)
    importID.value = response.import_id
    step.value = 'preview'
    await loadPreview()
  } catch (err) {
    uploadError.value = describeUploadError(err)
  } finally {
    uploadBusy.value = false
  }
}

const previewBusy = ref(false)
const previewError = ref('')
const previewRows = ref<readonly ImportPreviewRow[]>([])
const previewSummary = ref<ImportPreviewSummary | null>(null)

function applyRows(response: ImportPreviewResponse | ImportCommitRejectedResponse): void {
  previewRows.value = response.rows
  previewSummary.value = response.summary
}

async function loadPreview(): Promise<void> {
  previewBusy.value = true
  previewError.value = ''
  try {
    const response = await getImportPreview(importID.value)
    applyRows(response)
  } catch (err) {
    previewError.value = describe(err)
  } finally {
    previewBusy.value = false
  }
}

const hasErrorRows = computed(() => (previewSummary.value?.error ?? 0) > 0)
const canCommit = computed(() => previewSummary.value !== null && !hasErrorRows.value)

const commitBusy = ref(false)
const commitError = ref('')
const commitResult = ref<ImportCommitResponse | null>(null)

async function commit(): Promise<void> {
  if (!canCommit.value) return
  commitBusy.value = true
  commitError.value = ''
  try {
    commitResult.value = await postImportCommit(importID.value)
    step.value = 'result'
  } catch (err) {
    if (err instanceof ApiError && err.status === 422 && err.problem) {
      applyRows(err.problem as unknown as ImportCommitRejectedResponse)
      commitError.value =
        'The staged file now has row errors and cannot be committed. Review the rows below.'
    } else {
      commitError.value = describe(err)
    }
  } finally {
    commitBusy.value = false
  }
}

function reset(): void {
  step.value = 'upload'
  file.value = null
  uploadError.value = ''
  importID.value = ''
  previewRows.value = []
  previewSummary.value = null
  previewError.value = ''
  commitError.value = ''
  commitResult.value = null
}

function actionLabel(action: ImportPreviewRow['action']): string {
  switch (action) {
    case 'create':
      return 'Create'
    case 'update':
      return 'Update'
    case 'unchanged':
      return 'Unchanged'
    case 'error':
      return 'Error'
  }
}
</script>

<template>
  <section class="import v-stack">
    <h1 class="import__title">Import items</h1>

    <p class="muted import__export-hint">
      Start from your current inventory:
      <a :href="`${API_BASE_URL}/export/items.csv`" download>Export items (CSV)</a>, then edit and
      import it back.
    </p>

    <section v-if="step === 'upload'" class="card v-stack" aria-label="Upload a CSV file">
      <div class="field">
        <label for="import-file">CSV file</label>
        <input
          id="import-file"
          class="input"
          type="file"
          accept=".csv,text/csv"
          @change="onFileChange"
        />
      </div>

      <p v-if="uploadError" class="alert" role="alert">{{ uploadError }}</p>

      <button
        class="btn btn--primary"
        type="button"
        :disabled="uploadBusy || !file"
        @click="upload"
      >
        {{ uploadBusy ? 'Uploading…' : 'Upload and preview' }}
      </button>
    </section>

    <section
      v-else-if="step === 'preview'"
      class="card v-stack"
      aria-label="Review the staged import"
    >
      <p v-if="previewError" class="alert" role="alert">{{ previewError }}</p>
      <p v-else-if="commitError" class="alert" role="alert">{{ commitError }}</p>

      <p v-if="previewBusy" class="muted">Loading preview…</p>
      <template v-else>
        <p v-if="previewSummary" class="import__summary" role="status">
          Create: {{ previewSummary.create }} · Update: {{ previewSummary.update }} · Unchanged:
          {{ previewSummary.unchanged }} · Error: {{ previewSummary.error }}
        </p>

        <p v-if="hasErrorRows" class="muted">
          Fix or remove the row{{ previewSummary!.error === 1 ? '' : 's' }} with errors before
          committing — a staged file with any error row cannot be committed (all-or-nothing).
        </p>

        <p v-if="previewRows.length === 0" class="empty">No data rows in this file.</p>
        <table v-else class="import__table">
          <thead>
            <tr>
              <th scope="col">Line</th>
              <th scope="col">Action</th>
              <th scope="col">Name</th>
              <th scope="col">Detail</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in previewRows" :key="row.line">
              <td>{{ row.line }}</td>
              <td>
                <span class="import__badge" :class="`import__badge--${row.action}`">
                  {{ actionLabel(row.action) }}
                </span>
              </td>
              <td>{{ row.name || '—' }}</td>
              <td>
                <span v-if="row.action === 'update' && row.changes?.length">
                  Changed: {{ row.changes.join(', ') }}
                </span>
                <ul v-else-if="row.action === 'error' && row.errors?.length" class="import__errors">
                  <li v-for="(rowError, index) in row.errors" :key="index">
                    <template v-if="rowError.column">{{ rowError.column }}: </template
                    >{{ rowError.message }}
                  </li>
                </ul>
              </td>
            </tr>
          </tbody>
        </table>

        <div class="h-stack">
          <button
            class="btn btn--primary"
            type="button"
            :disabled="commitBusy || previewBusy || !canCommit"
            @click="commit"
          >
            {{ commitBusy ? 'Committing…' : 'Commit import' }}
          </button>
          <button class="btn btn--ghost" type="button" :disabled="commitBusy" @click="reset">
            Cancel
          </button>
        </div>
      </template>
    </section>

    <section
      v-else-if="step === 'result' && commitResult"
      class="card v-stack"
      aria-label="Import complete"
    >
      <p class="import__summary" role="status">
        Created: {{ commitResult.summary.created }} · Updated: {{ commitResult.summary.updated }} ·
        Unchanged: {{ commitResult.summary.unchanged }}
      </p>

      <ul v-if="commitResult.created_items.length > 0" class="import__created">
        <li v-for="created in commitResult.created_items" :key="created.line">
          Line {{ created.line }} → new item {{ created.item_id }}
        </li>
      </ul>

      <button class="btn btn--primary" type="button" @click="reset">Import another file</button>
    </section>
  </section>
</template>

<style scoped>
.import {
  max-width: 48rem;
}

.import__title {
  margin: 0;
  font-size: 1.25rem;
}

.import__export-hint {
  margin: 0;
}

.import__summary {
  font-weight: 600;
}

.import__table {
  width: 100%;
  border-collapse: collapse;
}

.import__table th,
.import__table td {
  text-align: left;
  padding: 0.4rem 0.5rem;
  border-bottom: 1px solid var(--hho-border);
  vertical-align: top;
}

.import__badge {
  display: inline-block;
  padding: 0.1rem 0.5rem;
  border-radius: 999px;
  font-size: 0.75rem;
  font-weight: 600;
  border: 1px solid var(--hho-muted);
}

.import__badge--create {
  border-color: var(--hho-accent);
  color: var(--hho-accent);
}

.import__badge--update {
  border-color: var(--hho-muted);
  color: var(--hho-fg);
}

.import__badge--unchanged {
  color: var(--hho-muted);
}

.import__badge--error {
  border-color: var(--hho-danger);
  color: var(--hho-danger);
}

.import__errors {
  margin: 0;
  padding-left: 1.1rem;
  color: var(--hho-danger);
}

.import__created {
  margin: 0;
  padding-left: 1.1rem;
}
</style>
