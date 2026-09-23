import { API_BASE_URL, apiFetch } from './client'
import type {
  Attachment,
  AttachmentCategory,
  AttachmentListResponse,
  CustomFieldDef,
  CustomFieldDefCreateRequest,
  CustomFieldDefListResponse,
  CustomFieldDefUpdateRequest,
  CustomFieldType,
  DeviceTokenListResponse,
  GroupMembersResponse,
  GroupVisibility,
  GroupVisibilityUpdateRequest,
  Identification,
  IdentificationKind,
  IdentificationListResponse,
  ImportCommitResponse,
  ImportPreviewResponse,
  ImportUploadResponse,
  InviteCreateResponse,
  InviteListResponse,
  InviteRedeemRequest,
  InviteRedeemResponse,
  Item,
  ItemCustomField,
  ItemCustomFieldListResponse,
  ItemListResponse,
  Label,
  LabelListResponse,
  LabelsQRBatchResponse,
  Location,
  LocationListResponse,
  LocationTreeResponse,
  LoginResponse,
  PurchaseBlock,
  ReportItemCountByLocationResponse,
  ReportPurchasesResponse,
  ReportValuationResponse,
  ReportWarrantyExpiringResponse,
  SaleBlock,
  SessionListResponse,
  WarrantyBlock,
} from './types'

export function register(username: string, password: string): Promise<LoginResponse> {
  return apiFetch<LoginResponse>('/auth/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
}

export function login(username: string, password: string): Promise<LoginResponse> {
  return apiFetch<LoginResponse>('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
}

export function logout(): Promise<void> {
  return apiFetch<void>('/auth/logout', { method: 'POST' })
}

export function getCurrentUser(): Promise<LoginResponse> {
  return apiFetch<LoginResponse>('/auth/me')
}

export interface ItemQuery {
  q?: string | undefined
  locationId?: string | undefined
  descendants?: boolean | undefined
  labelIds?: readonly string[]
  limit?: number
  offset?: number
}

function itemQueryString(query: ItemQuery): string {
  const params = new URLSearchParams()
  if (query.q?.trim()) params.set('q', query.q.trim())
  if (query.locationId) {
    params.set('location_id', query.locationId)
    if (query.descendants) params.set('descendants', 'true')
  }
  for (const id of query.labelIds ?? []) params.append('label_id', id)
  if (query.limit !== undefined) params.set('limit', String(query.limit))
  if (query.offset !== undefined) params.set('offset', String(query.offset))
  const rendered = params.toString()
  return rendered ? `?${rendered}` : ''
}

export function listItems(query: ItemQuery = {}): Promise<ItemListResponse> {
  return apiFetch<ItemListResponse>(`/items${itemQueryString(query)}`)
}

export function getItem(itemID: string): Promise<Item> {
  return apiFetch<Item>(`/items/${encodeURIComponent(itemID)}`)
}

export interface ItemWrite {
  name: string
  description?: string
  location_id?: string
  quantity?: number
}

export function createItem(body: ItemWrite): Promise<Item> {
  return apiFetch<Item>('/items', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function updateItem(itemID: string, body: ItemWrite & { version: number }): Promise<Item> {
  return apiFetch<Item>(`/items/${encodeURIComponent(itemID)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function deleteItem(itemID: string): Promise<void> {
  return apiFetch<void>(`/items/${encodeURIComponent(itemID)}`, { method: 'DELETE' })
}

export function listItemLabels(itemID: string): Promise<LabelListResponse> {
  return apiFetch<LabelListResponse>(`/items/${encodeURIComponent(itemID)}/labels`)
}

export function attachLabel(itemID: string, labelID: string): Promise<void> {
  return apiFetch<void>(
    `/items/${encodeURIComponent(itemID)}/labels/${encodeURIComponent(labelID)}`,
    { method: 'PUT' },
  )
}

export function detachLabel(itemID: string, labelID: string): Promise<void> {
  return apiFetch<void>(
    `/items/${encodeURIComponent(itemID)}/labels/${encodeURIComponent(labelID)}`,
    { method: 'DELETE' },
  )
}

export function listLocations(): Promise<LocationListResponse> {
  return apiFetch<LocationListResponse>('/locations')
}

export function locationTree(): Promise<LocationTreeResponse> {
  return apiFetch<LocationTreeResponse>('/locations/tree')
}

export function createLocation(name: string, parentID?: string): Promise<Location> {
  return apiFetch<Location>('/locations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, parent_id: parentID ?? '' }),
  })
}

export function updateLocation(
  locationID: string,
  body: { name: string; parent_id?: string; version: number },
): Promise<Location> {
  return apiFetch<Location>(`/locations/${encodeURIComponent(locationID)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function deleteLocation(locationID: string, reassignTo?: string): Promise<void> {
  const params = new URLSearchParams()
  if (reassignTo !== undefined) params.set('reassign_to', reassignTo)
  const query = params.toString() ? `?${params.toString()}` : ''
  return apiFetch<void>(`/locations/${encodeURIComponent(locationID)}${query}`, {
    method: 'DELETE',
  })
}

export interface LocationNotEmpty {
  readonly child_count: number
  readonly item_count: number
}

export function listLabels(): Promise<LabelListResponse> {
  return apiFetch<LabelListResponse>('/labels')
}

export function createLabel(name: string, color: string): Promise<Label> {
  return apiFetch<Label>('/labels', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, color }),
  })
}

export function updateLabel(
  labelID: string,
  body: { name: string; color: string; version: number },
): Promise<Label> {
  return apiFetch<Label>(`/labels/${encodeURIComponent(labelID)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function deleteLabel(labelID: string): Promise<void> {
  return apiFetch<void>(`/labels/${encodeURIComponent(labelID)}`, { method: 'DELETE' })
}

export function createLabelsQRBatch(itemIDs: readonly string[]): Promise<LabelsQRBatchResponse> {
  return apiFetch<LabelsQRBatchResponse>('/labels/qr/batch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ item_ids: itemIDs }),
  })
}

export interface WarrantyWrite {
  holder?: string
  provider?: string
  starts_on?: string
  expires_on?: string
  is_lifetime?: boolean
  notes?: string
}

export function getItemWarranty(itemID: string): Promise<WarrantyBlock> {
  return apiFetch<WarrantyBlock>(`/items/${encodeURIComponent(itemID)}/warranty`)
}

export function createItemWarranty(itemID: string, body: WarrantyWrite): Promise<WarrantyBlock> {
  return apiFetch<WarrantyBlock>(`/items/${encodeURIComponent(itemID)}/warranty`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function updateItemWarranty(
  itemID: string,
  body: WarrantyWrite & { version: number },
): Promise<WarrantyBlock> {
  return apiFetch<WarrantyBlock>(`/items/${encodeURIComponent(itemID)}/warranty`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function deleteItemWarranty(itemID: string): Promise<void> {
  return apiFetch<void>(`/items/${encodeURIComponent(itemID)}/warranty`, { method: 'DELETE' })
}

export interface SaleWrite {
  buyer_name?: string
  sold_on?: string
  sale_price_minor?: number
  notes?: string
}

export function getItemSale(itemID: string): Promise<SaleBlock> {
  return apiFetch<SaleBlock>(`/items/${encodeURIComponent(itemID)}/sale`)
}

export function createItemSale(itemID: string, body: SaleWrite): Promise<SaleBlock> {
  return apiFetch<SaleBlock>(`/items/${encodeURIComponent(itemID)}/sale`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function updateItemSale(
  itemID: string,
  body: SaleWrite & { version: number },
): Promise<SaleBlock> {
  return apiFetch<SaleBlock>(`/items/${encodeURIComponent(itemID)}/sale`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function deleteItemSale(itemID: string): Promise<void> {
  return apiFetch<void>(`/items/${encodeURIComponent(itemID)}/sale`, { method: 'DELETE' })
}

export interface PurchaseWrite {
  vendor?: string
  purchased_on?: string
  purchase_price_minor?: number
  order_reference?: string
  notes?: string
}

export function getItemPurchase(itemID: string): Promise<PurchaseBlock> {
  return apiFetch<PurchaseBlock>(`/items/${encodeURIComponent(itemID)}/purchase`)
}

export function createItemPurchase(itemID: string, body: PurchaseWrite): Promise<PurchaseBlock> {
  return apiFetch<PurchaseBlock>(`/items/${encodeURIComponent(itemID)}/purchase`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function updateItemPurchase(
  itemID: string,
  body: PurchaseWrite & { version: number },
): Promise<PurchaseBlock> {
  return apiFetch<PurchaseBlock>(`/items/${encodeURIComponent(itemID)}/purchase`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function deleteItemPurchase(itemID: string): Promise<void> {
  return apiFetch<void>(`/items/${encodeURIComponent(itemID)}/purchase`, { method: 'DELETE' })
}

export interface IdentificationWrite {
  kind: IdentificationKind
  value: string
}

export function listItemIdentifications(itemID: string): Promise<IdentificationListResponse> {
  return apiFetch<IdentificationListResponse>(
    `/items/${encodeURIComponent(itemID)}/identifications`,
  )
}

export function createItemIdentification(
  itemID: string,
  body: IdentificationWrite,
): Promise<Identification> {
  return apiFetch<Identification>(`/items/${encodeURIComponent(itemID)}/identifications`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function updateItemIdentification(
  itemID: string,
  identificationID: string,
  body: IdentificationWrite & { version: number },
): Promise<Identification> {
  return apiFetch<Identification>(
    `/items/${encodeURIComponent(itemID)}/identifications/${encodeURIComponent(identificationID)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
}

export function deleteItemIdentification(itemID: string, identificationID: string): Promise<void> {
  return apiFetch<void>(
    `/items/${encodeURIComponent(itemID)}/identifications/${encodeURIComponent(identificationID)}`,
    { method: 'DELETE' },
  )
}

export interface ItemCustomFieldWrite {
  field_def_id: string
  name: string
  field_type: CustomFieldType
  text_value: string | null
  number_value: number | null
  bool_value: boolean | null
  date_value: string | null
}

export function listItemCustomFields(itemID: string): Promise<ItemCustomFieldListResponse> {
  return apiFetch<ItemCustomFieldListResponse>(`/items/${encodeURIComponent(itemID)}/custom-fields`)
}

export function createItemCustomField(
  itemID: string,
  body: ItemCustomFieldWrite,
): Promise<ItemCustomField> {
  return apiFetch<ItemCustomField>(`/items/${encodeURIComponent(itemID)}/custom-fields`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function updateItemCustomField(
  itemID: string,
  customFieldID: string,
  body: ItemCustomFieldWrite & { version: number },
): Promise<ItemCustomField> {
  return apiFetch<ItemCustomField>(
    `/items/${encodeURIComponent(itemID)}/custom-fields/${encodeURIComponent(customFieldID)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
}

export function deleteItemCustomField(itemID: string, customFieldID: string): Promise<void> {
  return apiFetch<void>(
    `/items/${encodeURIComponent(itemID)}/custom-fields/${encodeURIComponent(customFieldID)}`,
    { method: 'DELETE' },
  )
}

export function listCustomFieldDefs(): Promise<CustomFieldDefListResponse> {
  return apiFetch<CustomFieldDefListResponse>('/custom-field-defs')
}

export function createCustomFieldDef(body: CustomFieldDefCreateRequest): Promise<CustomFieldDef> {
  return apiFetch<CustomFieldDef>('/custom-field-defs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function updateCustomFieldDef(
  fieldDefID: string,
  body: CustomFieldDefUpdateRequest,
): Promise<CustomFieldDef> {
  return apiFetch<CustomFieldDef>(`/custom-field-defs/${encodeURIComponent(fieldDefID)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function deleteCustomFieldDef(fieldDefID: string): Promise<void> {
  return apiFetch<void>(`/custom-field-defs/${encodeURIComponent(fieldDefID)}`, {
    method: 'DELETE',
  })
}

export function listItemAttachments(itemID: string): Promise<AttachmentListResponse> {
  return apiFetch<AttachmentListResponse>(`/items/${encodeURIComponent(itemID)}/attachments`)
}

export function uploadItemAttachment(
  itemID: string,
  category: AttachmentCategory,
  file: File,
): Promise<Attachment> {
  const body = new FormData()
  body.append('category', category)
  body.append('file', file, file.name)
  return apiFetch<Attachment>(`/items/${encodeURIComponent(itemID)}/attachments`, {
    method: 'POST',
    body,
  })
}

export function deleteItemAttachment(itemID: string, attachmentID: string): Promise<void> {
  return apiFetch<void>(
    `/items/${encodeURIComponent(itemID)}/attachments/${encodeURIComponent(attachmentID)}`,
    { method: 'DELETE' },
  )
}

export function itemAttachmentURL(itemID: string, attachmentID: string): string {
  return `${API_BASE_URL}/items/${encodeURIComponent(itemID)}/attachments/${encodeURIComponent(attachmentID)}`
}

export function itemAttachmentThumbnailURL(itemID: string, attachmentID: string): string {
  return `${API_BASE_URL}/items/${encodeURIComponent(itemID)}/attachments/${encodeURIComponent(attachmentID)}/thumbnail`
}

export function getGroupDetailVisibility(): Promise<GroupVisibility> {
  return apiFetch<GroupVisibility>('/groups/detail-visibility')
}

export function updateGroupDetailVisibility(
  body: GroupVisibilityUpdateRequest,
): Promise<GroupVisibility> {
  return apiFetch<GroupVisibility>('/groups/detail-visibility', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function listGroupMembers(): Promise<GroupMembersResponse> {
  return apiFetch<GroupMembersResponse>('/groups/members')
}

export function listSessions(): Promise<SessionListResponse> {
  return apiFetch<SessionListResponse>('/auth/sessions')
}

export function revokeSession(sessionID: string): Promise<void> {
  return apiFetch<void>(`/auth/sessions/${encodeURIComponent(sessionID)}`, { method: 'DELETE' })
}

export function listDeviceTokens(): Promise<DeviceTokenListResponse> {
  return apiFetch<DeviceTokenListResponse>('/auth/device-tokens')
}

export function revokeDeviceToken(deviceTokenID: string): Promise<void> {
  return apiFetch<void>(`/auth/device-tokens/${encodeURIComponent(deviceTokenID)}`, {
    method: 'DELETE',
  })
}

export function listInvites(): Promise<InviteListResponse> {
  return apiFetch<InviteListResponse>('/invites')
}

export function createInvite(): Promise<InviteCreateResponse> {
  return apiFetch<InviteCreateResponse>('/invites', { method: 'POST' })
}

export function revokeInvite(inviteID: string): Promise<void> {
  return apiFetch<void>(`/invites/${encodeURIComponent(inviteID)}`, { method: 'DELETE' })
}

export function redeemInvite(request: InviteRedeemRequest): Promise<InviteRedeemResponse> {
  return apiFetch<InviteRedeemResponse>('/invites/redeem', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  })
}

export type ReportGroupBy = 'location' | 'label'

export function getReportValuation(groupBy: ReportGroupBy): Promise<ReportValuationResponse> {
  return apiFetch<ReportValuationResponse>(`/reports/valuation?group_by=${groupBy}`)
}

export function reportValuationCSVURL(groupBy: ReportGroupBy): string {
  return `${API_BASE_URL}/reports/valuation?group_by=${groupBy}&format=csv`
}

export function getReportWarrantyExpiring(
  withinDays: number,
): Promise<ReportWarrantyExpiringResponse> {
  return apiFetch<ReportWarrantyExpiringResponse>(
    `/reports/warranty-expiring?within_days=${encodeURIComponent(String(withinDays))}`,
  )
}

export function reportWarrantyExpiringCSVURL(withinDays: number): string {
  return `${API_BASE_URL}/reports/warranty-expiring?within_days=${encodeURIComponent(String(withinDays))}&format=csv`
}

export function getReportPurchases(from: string, to: string): Promise<ReportPurchasesResponse> {
  const params = new URLSearchParams({ from, to })
  return apiFetch<ReportPurchasesResponse>(`/reports/purchases?${params.toString()}`)
}

export function reportPurchasesCSVURL(from: string, to: string): string {
  const params = new URLSearchParams({ from, to, format: 'csv' })
  return `${API_BASE_URL}/reports/purchases?${params.toString()}`
}

export function getReportItemCountByLocation(): Promise<ReportItemCountByLocationResponse> {
  return apiFetch<ReportItemCountByLocationResponse>('/reports/item-count-by-location')
}

export function reportItemCountByLocationCSVURL(): string {
  return `${API_BASE_URL}/reports/item-count-by-location?format=csv`
}

export interface BomSelection {
  locationIds?: readonly string[]
  labelIds?: readonly string[]
  itemIds?: readonly string[]
}

export function exportBomCSVURL(selection: BomSelection): string {
  const params = new URLSearchParams()
  for (const id of selection.locationIds ?? []) params.append('location_id', id)
  for (const id of selection.labelIds ?? []) params.append('label_id', id)
  for (const id of selection.itemIds ?? []) params.append('item_id', id)
  const query = params.toString()
  return `${API_BASE_URL}/export/bom${query ? `?${query}` : ''}`
}

export function importNativeUpload(csv: string): Promise<ImportUploadResponse> {
  return apiFetch<ImportUploadResponse>('/import/native/upload', {
    method: 'POST',
    headers: { 'Content-Type': 'text/csv' },
    body: csv,
  })
}

export function getImportPreview(importID: string): Promise<ImportPreviewResponse> {
  return apiFetch<ImportPreviewResponse>(`/import/${encodeURIComponent(importID)}/preview`)
}

export function postImportCommit(importID: string): Promise<ImportCommitResponse> {
  return apiFetch<ImportCommitResponse>(`/import/${encodeURIComponent(importID)}/commit`, {
    method: 'POST',
  })
}
