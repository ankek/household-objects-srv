<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  API_BASE_URL,
  ApiError,
  createCustomFieldDef,
  createInvite,
  deleteCustomFieldDef,
  getGroupDetailVisibility,
  listCustomFieldDefs,
  listDeviceTokens,
  listGroupMembers,
  listInvites,
  listSessions,
  revokeDeviceToken,
  revokeInvite,
  revokeSession,
  updateCustomFieldDef,
  updateGroupDetailVisibility,
} from '@/api'
import type {
  CustomFieldDef,
  CustomFieldType,
  DeviceTokenListItem,
  GroupMemberListItem,
  GroupVisibility,
  InviteListItem,
  SessionListItem,
} from '@/api'
import { useSessionStore } from '@/stores'

const session = useSessionStore()

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

function formatDate(ms: number): string {
  return new Date(ms).toLocaleString()
}

const members = ref<GroupMemberListItem[]>([])
const membersLoading = ref(true)
const membersError = ref('')

async function loadMembers(): Promise<void> {
  membersLoading.value = true
  membersError.value = ''
  try {
    members.value = [...(await listGroupMembers()).members]
  } catch (err) {
    membersError.value = describe(err)
  } finally {
    membersLoading.value = false
  }
}

const sessions = ref<SessionListItem[]>([])
const sessionsLoading = ref(true)
const sessionsError = ref('')
const sessionsBusyId = ref<string | null>(null)

async function loadSessions(): Promise<void> {
  sessionsLoading.value = true
  sessionsError.value = ''
  try {
    sessions.value = [...(await listSessions()).sessions]
  } catch (err) {
    sessionsError.value = describe(err)
  } finally {
    sessionsLoading.value = false
  }
}

async function doRevokeSession(row: SessionListItem): Promise<void> {
  if (!window.confirm('Sign this session out?')) return
  sessionsBusyId.value = row.id
  sessionsError.value = ''
  try {
    await revokeSession(row.id)
    sessions.value = sessions.value.filter((s) => s.id !== row.id)
  } catch (err) {
    sessionsError.value = describe(err)
  } finally {
    sessionsBusyId.value = null
  }
}

const deviceTokens = ref<DeviceTokenListItem[]>([])
const devicesLoading = ref(true)
const devicesError = ref('')
const devicesBusyId = ref<string | null>(null)

async function loadDeviceTokens(): Promise<void> {
  devicesLoading.value = true
  devicesError.value = ''
  try {
    deviceTokens.value = [...(await listDeviceTokens()).device_tokens]
  } catch (err) {
    devicesError.value = describe(err)
  } finally {
    devicesLoading.value = false
  }
}

async function doRevokeDeviceToken(row: DeviceTokenListItem): Promise<void> {
  if (!window.confirm(`Revoke the device "${row.device_label}"?`)) return
  devicesBusyId.value = row.id
  devicesError.value = ''
  try {
    await revokeDeviceToken(row.id)
    deviceTokens.value = deviceTokens.value.filter((d) => d.id !== row.id)
  } catch (err) {
    devicesError.value = describe(err)
  } finally {
    devicesBusyId.value = null
  }
}

const invites = ref<InviteListItem[]>([])
const invitesLoading = ref(true)
const invitesError = ref('')
const creatingInvite = ref(false)
const revokingInviteId = ref<string | null>(null)
const justCreated = ref<{ token: string; expiresAt: number } | null>(null)

async function loadInvites(): Promise<void> {
  if (!session.isOwner) return
  invitesLoading.value = true
  invitesError.value = ''
  try {
    invites.value = [...(await listInvites()).invites]
  } catch (err) {
    invitesError.value = describe(err)
  } finally {
    invitesLoading.value = false
  }
}

function inviteStatus(invite: InviteListItem): 'redeemed' | 'expired' | 'active' {
  if (invite.redeemed) return 'redeemed'
  if (invite.expires_at <= Date.now()) return 'expired'
  return 'active'
}

async function doCreateInvite(): Promise<void> {
  creatingInvite.value = true
  invitesError.value = ''
  try {
    const created = await createInvite()
    justCreated.value = { token: created.token, expiresAt: created.expires_at }
    await loadInvites()
  } catch (err) {
    invitesError.value = describe(err)
  } finally {
    creatingInvite.value = false
  }
}

async function copyInviteToken(): Promise<void> {
  const token = justCreated.value?.token
  if (!token) return
  try {
    await navigator.clipboard.writeText(token)
  } catch {
    // Clipboard access can be denied or unavailable (permissions, insecure
    // context, an older browser). The token is still selectable text in the
    // code block either way, so this is a convenience, not the only path.
  }
}

const justCreatedJoinLink = computed(() =>
  justCreated.value
    ? `${window.location.origin}/join?token=${encodeURIComponent(justCreated.value.token)}`
    : '',
)

async function copyInviteJoinLink(): Promise<void> {
  const link = justCreatedJoinLink.value
  if (!link) return
  try {
    await navigator.clipboard.writeText(link)
  } catch {
    // See copyInviteToken's identical rationale.
  }
}

async function doRevokeInvite(row: InviteListItem): Promise<void> {
  if (!window.confirm('Revoke this invite? It can no longer be redeemed.')) return
  revokingInviteId.value = row.id
  invitesError.value = ''
  try {
    await revokeInvite(row.id)
    invites.value = invites.value.filter((i) => i.id !== row.id)
  } catch (err) {
    invitesError.value = describe(err)
  } finally {
    revokingInviteId.value = null
  }
}

const visibility = ref<GroupVisibility | null>(null)
const visibilityLoading = ref(true)
const visibilityError = ref('')
const visibilitySaving = ref(false)
const warrantyVisible = ref(false)
const saleVisible = ref(false)
const purchaseVisible = ref(false)

async function loadVisibility(): Promise<void> {
  if (!session.isOwner) return
  visibilityLoading.value = true
  visibilityError.value = ''
  try {
    const state = await getGroupDetailVisibility()
    visibility.value = state
    warrantyVisible.value = state.warranty_visible
    saleVisible.value = state.sale_visible
    purchaseVisible.value = state.purchase_visible
  } catch (err) {
    visibilityError.value = describe(err)
  } finally {
    visibilityLoading.value = false
  }
}

async function saveVisibility(): Promise<void> {
  if (visibility.value?.version === undefined) return
  visibilitySaving.value = true
  visibilityError.value = ''
  try {
    const updated = await updateGroupDetailVisibility({
      warranty_visible: warrantyVisible.value,
      sale_visible: saleVisible.value,
      purchase_visible: purchaseVisible.value,
      version: visibility.value.version,
    })
    visibility.value = updated
    warrantyVisible.value = updated.warranty_visible
    saleVisible.value = updated.sale_visible
    purchaseVisible.value = updated.purchase_visible
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      await loadVisibility()
      visibilityError.value =
        'Someone else changed these settings first. Reloading the current values.'
    } else {
      visibilityError.value = describe(err)
    }
  } finally {
    visibilitySaving.value = false
  }
}

const FIELD_TYPES: CustomFieldType[] = ['text', 'number', 'boolean', 'date']

const fieldDefs = ref<CustomFieldDef[]>([])
const fieldDefsLoading = ref(true)
const fieldDefsError = ref('')
const fieldDefsBusy = ref(false)

const newFieldDefName = ref('')
const newFieldDefType = ref<CustomFieldType>('text')

const editingFieldDefId = ref<string | null>(null)
const editFieldDefName = ref('')
const editFieldDefType = ref<CustomFieldType>('text')

async function loadFieldDefs(): Promise<void> {
  if (!session.isOwner) return
  fieldDefsLoading.value = true
  fieldDefsError.value = ''
  try {
    fieldDefs.value = [...(await listCustomFieldDefs()).custom_field_defs]
  } catch (err) {
    fieldDefsError.value = describe(err)
  } finally {
    fieldDefsLoading.value = false
  }
}

async function doCreateFieldDef(): Promise<void> {
  const name = newFieldDefName.value.trim()
  if (!name) return
  fieldDefsBusy.value = true
  fieldDefsError.value = ''
  try {
    const created = await createCustomFieldDef({
      name,
      field_type: newFieldDefType.value,
      display_order: fieldDefs.value.length,
    })
    fieldDefs.value = [...fieldDefs.value, created]
    newFieldDefName.value = ''
    newFieldDefType.value = 'text'
  } catch (err) {
    fieldDefsError.value = describe(err)
  } finally {
    fieldDefsBusy.value = false
  }
}

function startRenameFieldDef(row: CustomFieldDef): void {
  editingFieldDefId.value = row.id
  editFieldDefName.value = row.name
  editFieldDefType.value = row.field_type
  fieldDefsError.value = ''
}

function cancelRenameFieldDef(): void {
  editingFieldDefId.value = null
}

async function doRenameFieldDef(): Promise<void> {
  const row = fieldDefs.value.find((d) => d.id === editingFieldDefId.value)
  const name = editFieldDefName.value.trim()
  if (row === undefined || !name) return
  fieldDefsBusy.value = true
  fieldDefsError.value = ''
  try {
    const updated = await updateCustomFieldDef(row.id, {
      name,
      field_type: editFieldDefType.value,
      display_order: row.display_order,
      version: row.version,
    })
    fieldDefs.value = fieldDefs.value.map((d) => (d.id === updated.id ? updated : d))
    editingFieldDefId.value = null
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      fieldDefsError.value = 'Someone else changed this definition first. Reload and try again.'
    } else {
      fieldDefsError.value = describe(err)
    }
  } finally {
    fieldDefsBusy.value = false
  }
}

async function doDeleteFieldDef(row: CustomFieldDef): Promise<void> {
  if (
    !window.confirm(
      `Delete the "${row.name}" field definition? Items that already have a value for it keep ` +
        'that value, but it will no longer appear as a choice when adding a new custom field.',
    )
  ) {
    return
  }
  fieldDefsBusy.value = true
  fieldDefsError.value = ''
  try {
    await deleteCustomFieldDef(row.id)
    fieldDefs.value = fieldDefs.value.filter((d) => d.id !== row.id)
  } catch (err) {
    fieldDefsError.value = describe(err)
  } finally {
    fieldDefsBusy.value = false
  }
}

onMounted(() => {
  void loadMembers()
  void loadSessions()
  void loadDeviceTokens()
  void loadInvites()
  void loadVisibility()
  void loadFieldDefs()
})
</script>

<template>
  <section class="settings v-stack">
    <h1 class="settings__title">Settings</h1>

    <section class="card v-stack settings__section">
      <h2 class="settings__section-title">Group members</h2>
      <p class="muted">Everyone in this household's group.</p>

      <p v-if="membersError" class="alert" role="alert">{{ membersError }}</p>
      <p v-if="membersLoading" class="muted">Loading…</p>
      <p v-else-if="members.length === 0" class="empty">No members.</p>
      <ul v-else class="settings__list">
        <li v-for="member in members" :key="member.id" class="settings__row">
          <div class="v-stack settings__row-info">
            <span>{{ member.username }}</span>
            <span class="muted settings__row-meta">
              {{ member.role }} · joined {{ formatDate(member.joined_at) }}
            </span>
          </div>
        </li>
      </ul>
    </section>

    <section v-if="session.isOwner" class="card v-stack settings__section">
      <h2 class="settings__section-title">Invites</h2>
      <p class="muted">
        Invite links let someone join this household's group. Each one is valid for 24 hours and can
        be used once.
      </p>

      <p v-if="invitesError" class="alert" role="alert">{{ invitesError }}</p>

      <div v-if="justCreated" class="card v-stack settings__invite-token" role="alert">
        <p>
          <strong>New invite created.</strong> Copy this token now — it will never be shown again.
        </p>
        <code class="settings__invite-code">{{ justCreated.token }}</code>
        <div class="h-stack">
          <button class="btn btn--small" type="button" @click="copyInviteToken">Copy</button>
          <button class="btn btn--ghost btn--small" type="button" @click="justCreated = null">
            Dismiss
          </button>
        </div>
        <p class="muted">Or send this link — it fills the token in for them:</p>
        <code class="settings__invite-code">{{ justCreatedJoinLink }}</code>
        <div class="h-stack">
          <button class="btn btn--small" type="button" @click="copyInviteJoinLink">
            Copy link
          </button>
        </div>
        <p class="muted settings__invite-expiry">
          Expires {{ formatDate(justCreated.expiresAt) }}.
        </p>
      </div>

      <button
        class="btn btn--primary btn--small settings__invite-create"
        type="button"
        :disabled="creatingInvite"
        @click="doCreateInvite"
      >
        {{ creatingInvite ? 'Creating…' : 'Create invite' }}
      </button>

      <p v-if="invitesLoading" class="muted">Loading…</p>
      <p v-else-if="invites.length === 0" class="empty">No invites issued yet.</p>
      <ul v-else class="settings__list">
        <li v-for="invite in invites" :key="invite.id" class="settings__row">
          <div class="v-stack settings__row-info">
            <span class="settings__status" :class="`settings__status--${inviteStatus(invite)}`">
              {{ inviteStatus(invite) }}
            </span>
            <span class="muted settings__row-meta">
              Created {{ formatDate(invite.created_at) }}
              <template v-if="invite.redeemed && invite.redeemed_at">
                · redeemed {{ formatDate(invite.redeemed_at) }}
              </template>
              <template v-else> · expires {{ formatDate(invite.expires_at) }} </template>
            </span>
          </div>
          <button
            v-if="inviteStatus(invite) === 'active'"
            class="btn btn--ghost btn--small btn--danger"
            type="button"
            :disabled="revokingInviteId === invite.id"
            @click="doRevokeInvite(invite)"
          >
            Revoke
          </button>
        </li>
      </ul>
    </section>

    <section v-if="session.isOwner" class="card v-stack settings__section">
      <h2 class="settings__section-title">Item detail blocks</h2>
      <p class="muted">
        Choose which optional detail blocks appear on every item's page for the whole household. A
        fresh household starts with all three hidden.
      </p>

      <p v-if="visibilityError" class="alert" role="alert">{{ visibilityError }}</p>
      <p v-if="visibilityLoading" class="muted">Loading…</p>
      <form v-else class="v-stack" @submit.prevent="saveVisibility">
        <label class="h-stack settings__toggle">
          <input v-model="warrantyVisible" type="checkbox" :disabled="visibilitySaving" />
          Warranty
        </label>
        <label class="h-stack settings__toggle">
          <input v-model="purchaseVisible" type="checkbox" :disabled="visibilitySaving" />
          Purchase
        </label>
        <label class="h-stack settings__toggle">
          <input v-model="saleVisible" type="checkbox" :disabled="visibilitySaving" />
          Sale
        </label>
        <button
          class="btn btn--primary btn--small settings__visibility-save"
          type="submit"
          :disabled="visibilitySaving"
        >
          {{ visibilitySaving ? 'Saving…' : 'Save' }}
        </button>
      </form>
    </section>

    <section v-if="session.isOwner" class="card v-stack settings__section">
      <h2 class="settings__section-title">Custom fields</h2>
      <p class="muted">
        Field definitions standardize the custom fields available when entering an item's data.
        Deleting one does not remove its value from items that already have it — only from the list
        offered when adding a new custom field.
      </p>

      <p v-if="fieldDefsError" class="alert" role="alert">{{ fieldDefsError }}</p>
      <p v-if="fieldDefsLoading" class="muted">Loading…</p>
      <p v-else-if="fieldDefs.length === 0" class="empty">No custom field definitions yet.</p>
      <ul v-else class="settings__list">
        <li v-for="def in fieldDefs" :key="def.id" class="settings__row">
          <template v-if="editingFieldDefId === def.id">
            <form class="h-stack" @submit.prevent="doRenameFieldDef">
              <input
                v-model="editFieldDefName"
                class="input"
                :disabled="fieldDefsBusy"
                aria-label="Name"
              />
              <select v-model="editFieldDefType" class="select" :disabled="fieldDefsBusy">
                <option v-for="type in FIELD_TYPES" :key="type" :value="type">{{ type }}</option>
              </select>
              <button class="btn btn--primary btn--small" type="submit" :disabled="fieldDefsBusy">
                Save
              </button>
              <button
                class="btn btn--small"
                type="button"
                :disabled="fieldDefsBusy"
                @click="cancelRenameFieldDef"
              >
                Cancel
              </button>
            </form>
          </template>
          <template v-else>
            <div class="v-stack settings__row-info">
              <span>{{ def.name }}</span>
              <span class="muted settings__row-meta">{{ def.field_type }}</span>
            </div>
            <div class="h-stack">
              <button
                class="btn btn--ghost btn--small"
                type="button"
                :disabled="fieldDefsBusy"
                @click="startRenameFieldDef(def)"
              >
                Rename
              </button>
              <button
                class="btn btn--ghost btn--small btn--danger"
                type="button"
                :disabled="fieldDefsBusy"
                @click="doDeleteFieldDef(def)"
              >
                Delete
              </button>
            </div>
          </template>
        </li>
      </ul>

      <form class="h-stack settings__field-def-create" @submit.prevent="doCreateFieldDef">
        <input
          v-model="newFieldDefName"
          class="input"
          placeholder="Field name"
          :disabled="fieldDefsBusy"
          aria-label="New field name"
        />
        <select v-model="newFieldDefType" class="select" :disabled="fieldDefsBusy">
          <option v-for="type in FIELD_TYPES" :key="type" :value="type">{{ type }}</option>
        </select>
        <button class="btn btn--primary btn--small" type="submit" :disabled="fieldDefsBusy">
          Add
        </button>
      </form>
    </section>

    <section class="card v-stack settings__section">
      <h2 class="settings__section-title">Sessions</h2>
      <p class="muted">Every browser currently signed in as you.</p>

      <p v-if="sessionsError" class="alert" role="alert">{{ sessionsError }}</p>
      <p v-if="sessionsLoading" class="muted">Loading…</p>
      <p v-else-if="sessions.length === 0" class="empty">No sessions.</p>
      <ul v-else class="settings__list">
        <li v-for="row in sessions" :key="row.id" class="settings__row">
          <div class="v-stack settings__row-info">
            <span>{{ row.user_agent || 'Unknown browser' }}</span>
            <span class="muted settings__row-meta">
              {{ row.created_from_ip }} · signed in {{ formatDate(row.created_at) }}
            </span>
          </div>
          <span v-if="row.revoked" class="muted settings__status settings__status--revoked">
            revoked
          </span>
          <button
            v-else
            class="btn btn--ghost btn--small btn--danger"
            type="button"
            :disabled="sessionsBusyId === row.id"
            @click="doRevokeSession(row)"
          >
            Sign out
          </button>
        </li>
      </ul>
    </section>

    <section class="card v-stack settings__section">
      <h2 class="settings__section-title">Devices</h2>
      <p class="muted">Devices registered to sync with your account.</p>

      <p v-if="devicesError" class="alert" role="alert">{{ devicesError }}</p>
      <p v-if="devicesLoading" class="muted">Loading…</p>
      <p v-else-if="deviceTokens.length === 0" class="empty">No registered devices.</p>
      <ul v-else class="settings__list">
        <li v-for="row in deviceTokens" :key="row.id" class="settings__row">
          <div class="v-stack settings__row-info">
            <span>{{ row.device_label }}</span>
            <span class="muted settings__row-meta"
              >registered {{ formatDate(row.created_at) }}</span
            >
          </div>
          <span v-if="row.revoked" class="muted settings__status settings__status--revoked">
            revoked
          </span>
          <button
            v-else
            class="btn btn--ghost btn--small btn--danger"
            type="button"
            :disabled="devicesBusyId === row.id"
            @click="doRevokeDeviceToken(row)"
          >
            Revoke
          </button>
        </li>
      </ul>
    </section>

    <section v-if="session.isOwner" class="card v-stack settings__section">
      <h2 class="settings__section-title">Backup and restore</h2>
      <p class="muted">
        This backup is a full snapshot of the entire server: every household's data, attachments,
        and account records, not just this one. Store it somewhere private. To restore from it, use
        <code>hho restore</code> on the command line — see the backup and restore runbook in the
        project docs.
      </p>
      <a class="btn btn--primary btn--small" :href="`${API_BASE_URL}/backup`" download>
        Download backup
      </a>
    </section>
  </section>
</template>

<style scoped>
.settings {
  max-width: 42rem;
}

.settings__title {
  margin: 0;
  font-size: 1.25rem;
}

.settings__section-title {
  margin: 0;
  font-size: 1rem;
}

.settings__list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.settings__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.5rem 0;
  border-bottom: 1px solid var(--hho-border);
}

.settings__row:last-child {
  border-bottom: none;
}

.settings__row-info {
  gap: 0.15rem;
  min-width: 0;
  overflow-wrap: anywhere;
}

.settings__row-meta {
  font-size: 0.8125rem;
}

.settings__status {
  font-size: 0.8125rem;
  text-transform: capitalize;
}

.settings__status--active {
  color: var(--hho-accent);
}

.settings__status--expired,
.settings__status--revoked {
  color: var(--hho-muted);
}

.settings__status--redeemed {
  color: var(--hho-muted);
}

.settings__invite-token {
  border-color: var(--hho-accent);
}

.settings__invite-code {
  display: block;
  padding: 0.5rem 0.65rem;
  border-radius: var(--hho-radius);
  background: var(--hho-bg);
  border: 1px solid var(--hho-border);
  overflow-wrap: anywhere;
  font-size: 0.9rem;
}

.settings__invite-expiry {
  font-size: 0.8125rem;
}

.settings__invite-create {
  align-self: flex-start;
}

.settings__toggle {
  gap: 0.5rem;
  align-items: center;
}

.settings__visibility-save {
  align-self: flex-start;
}

.settings__field-def-create {
  align-items: center;
  gap: 0.5rem;
}
</style>
