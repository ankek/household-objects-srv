<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  ApiError,
  deleteItemAttachment,
  itemAttachmentThumbnailURL,
  itemAttachmentURL,
  listItemAttachments,
  uploadItemAttachment,
} from '@/api'
import type { Attachment, AttachmentCategory } from '@/api'

const props = defineProps<{ itemId: string }>()

const CATEGORY_ORDER: readonly AttachmentCategory[] = [
  'image',
  'manual',
  'warranty',
  'receipt',
  'general',
]

const CATEGORY_LABELS: Record<AttachmentCategory, string> = {
  image: 'Images',
  manual: 'Manuals',
  warranty: 'Warranty documents',
  receipt: 'Receipts',
  general: 'General',
}

const attachments = ref<Attachment[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')

const uploadCategory = ref<AttachmentCategory>('general')
const selectedFile = ref<File | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    attachments.value = [...(await listItemAttachments(props.itemId)).attachments]
  } catch (err) {
    error.value = describe(err)
  } finally {
    loading.value = false
  }
}

const groups = computed(() =>
  CATEGORY_ORDER.map((category) => ({
    category,
    label: CATEGORY_LABELS[category],
    items: attachments.value.filter((row) => row.category === category),
  })).filter((group) => group.items.length > 0),
)

function humanSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unitIndex]}`
}

function onFileChange(event: Event): void {
  const input = event.target as HTMLInputElement
  selectedFile.value = input.files?.[0] ?? null
}

async function upload(): Promise<void> {
  const file = selectedFile.value
  if (file === null) return
  busy.value = true
  error.value = ''
  try {
    const created = await uploadItemAttachment(props.itemId, uploadCategory.value, file)
    attachments.value = [...attachments.value, created]
    selectedFile.value = null
    if (fileInput.value) fileInput.value.value = ''
  } catch (err) {
    error.value = describe(err)
  } finally {
    busy.value = false
  }
}

async function remove(row: Attachment): Promise<void> {
  if (!window.confirm(`Delete "${row.original_filename}"?`)) return
  busy.value = true
  error.value = ''
  try {
    await deleteItemAttachment(props.itemId, row.id)
    attachments.value = attachments.value.filter((a) => a.id !== row.id)
  } catch (err) {
    error.value = describe(err)
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="card v-stack detail-block" data-testid="attachments-gallery">
    <h2 class="detail-block__title">Attachments</h2>

    <p v-if="loading" class="muted">Loading…</p>

    <template v-else>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>

      <div v-for="group in groups" :key="group.category" class="attachments__category v-stack">
        <h3 class="attachments__category-title">{{ group.label }}</h3>

        <div v-if="group.category === 'image'" class="attachments__grid">
          <div v-for="row in group.items" :key="row.id" class="attachments__thumb">
            <img
              v-if="row.has_thumbnail"
              class="attachments__thumb-img"
              :src="itemAttachmentThumbnailURL(itemId, row.id)"
              :alt="row.original_filename"
            />
            <div v-else class="attachments__thumb-fallback">
              <span class="attachments__thumb-icon" aria-hidden="true">🖼</span>
              <span class="attachments__thumb-name">{{ row.original_filename }}</span>
            </div>
            <div class="attachments__thumb-meta h-stack">
              <span class="muted attachments__thumb-size">{{ humanSize(row.size_bytes) }}</span>
              <a
                class="btn btn--ghost btn--small"
                :href="itemAttachmentURL(itemId, row.id)"
                target="_blank"
                rel="noopener"
              >
                Open
              </a>
              <button
                class="btn btn--ghost btn--small btn--danger"
                type="button"
                :disabled="busy"
                @click="remove(row)"
              >
                Delete
              </button>
            </div>
          </div>
        </div>

        <ul v-else class="attachments__rows">
          <li v-for="row in group.items" :key="row.id" class="attachments__row">
            <span class="attachments__row-icon" aria-hidden="true">📄</span>
            <span class="attachments__row-name">{{ row.original_filename }}</span>
            <span class="muted attachments__row-size">{{ humanSize(row.size_bytes) }}</span>
            <span class="h-stack">
              <a
                class="btn btn--ghost btn--small"
                :href="itemAttachmentURL(itemId, row.id)"
                download
              >
                Download
              </a>
              <button
                class="btn btn--ghost btn--small btn--danger"
                type="button"
                :disabled="busy"
                @click="remove(row)"
              >
                Delete
              </button>
            </span>
          </li>
        </ul>
      </div>

      <form class="attachments__upload h-stack" @submit.prevent="upload">
        <div class="field">
          <label for="attachment-category">Category</label>
          <select id="attachment-category" v-model="uploadCategory" class="select" :disabled="busy">
            <option v-for="category in CATEGORY_ORDER" :key="category" :value="category">
              {{ CATEGORY_LABELS[category] }}
            </option>
          </select>
        </div>
        <div class="field">
          <label for="attachment-file">File</label>
          <input
            id="attachment-file"
            ref="fileInput"
            type="file"
            :disabled="busy"
            @change="onFileChange"
          />
        </div>
        <button
          class="btn btn--primary btn--small attachments__upload-btn"
          type="submit"
          :disabled="busy || selectedFile === null"
        >
          {{ busy ? 'Uploading…' : 'Upload' }}
        </button>
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

.attachments__category-title {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 600;
}

.attachments__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr));
  gap: 0.75rem;
}

.attachments__thumb {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.attachments__thumb-img {
  width: 100%;
  aspect-ratio: 1 / 1;
  object-fit: cover;
  border-radius: var(--hho-radius);
  border: 1px solid var(--hho-border);
  background: var(--hho-bg);
}

.attachments__thumb-fallback {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.25rem;
  aspect-ratio: 1 / 1;
  border-radius: var(--hho-radius);
  border: 1px dashed var(--hho-border);
  background: var(--hho-bg);
  padding: 0.5rem;
  text-align: center;
}

.attachments__thumb-icon {
  font-size: 1.5rem;
}

.attachments__thumb-name {
  font-size: 0.75rem;
  color: var(--hho-muted);
  overflow-wrap: anywhere;
}

.attachments__thumb-meta {
  flex-wrap: wrap;
  font-size: 0.8125rem;
}

.attachments__thumb-size {
  flex: 1;
}

.attachments__rows {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

.attachments__row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.35rem 0;
  border-bottom: 1px solid var(--hho-border);
}

.attachments__row-name {
  flex: 1;
  overflow-wrap: anywhere;
}

.attachments__row-size {
  font-size: 0.8125rem;
}

.attachments__upload {
  flex-wrap: wrap;
  align-items: flex-end;
}

.attachments__upload-btn {
  align-self: flex-end;
}
</style>
