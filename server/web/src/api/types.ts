interface Versioned {
  readonly id: string
  readonly created_at: number
  readonly updated_at: number
  readonly version: number
}

export interface Item extends Versioned {
  readonly name: string
  readonly description: string
  readonly location_id?: string
  readonly quantity: number
  readonly short_code?: string
}

export interface Location extends Versioned {
  readonly name: string
  readonly parent_id?: string
}

export interface Label extends Versioned {
  readonly name: string
  readonly color: string
}

export interface LocationTreeNode extends Location {
  readonly item_count: number
  readonly total_item_count: number
  readonly children: readonly LocationTreeNode[]
}

export interface ItemListResponse {
  readonly items: readonly Item[]
}
export interface LocationListResponse {
  readonly locations: readonly Location[]
}
export interface LocationTreeResponse {
  readonly tree: readonly LocationTreeNode[]
}
export interface LabelListResponse {
  readonly labels: readonly Label[]
}

export interface LabelsQRBatchItem {
  readonly id: string
  readonly short_code: string
  readonly name: string
  readonly qr_svg: string
}

export interface LabelsQRBatchResponse {
  readonly items: readonly LabelsQRBatchItem[]
}

export interface WarrantyBlock {
  readonly item_id?: string
  readonly holder?: string
  readonly provider?: string
  readonly starts_on?: string
  readonly expires_on?: string
  readonly is_lifetime: boolean
  readonly notes?: string
  readonly created_at: number
  readonly updated_at: number
  readonly version: number
}

export interface SaleBlock {
  readonly item_id?: string
  readonly buyer_name?: string
  readonly sold_on?: string
  readonly sale_price_minor: number
  readonly notes?: string
  readonly created_at: number
  readonly updated_at: number
  readonly version: number
}

export interface PurchaseBlock {
  readonly item_id?: string
  readonly vendor?: string
  readonly purchased_on?: string
  readonly purchase_price_minor: number
  readonly order_reference?: string
  readonly notes?: string
  readonly created_at: number
  readonly updated_at: number
  readonly version: number
}

export type IdentificationKind = 'serial' | 'model' | 'asset_tag' | 'barcode' | 'other'

export interface Identification extends Versioned {
  readonly item_id?: string
  readonly kind: IdentificationKind
  readonly value: string
}

export interface IdentificationListResponse {
  readonly identifications: readonly Identification[]
}

export type CustomFieldType = 'text' | 'number' | 'boolean' | 'date'

export interface CustomFieldDef extends Versioned {
  readonly name: string
  readonly field_type: CustomFieldType
  readonly display_order: number
}

export interface CustomFieldDefListResponse {
  readonly custom_field_defs: readonly CustomFieldDef[]
}

export interface CustomFieldDefCreateRequest {
  readonly name: string
  readonly field_type: CustomFieldType
  readonly display_order: number
}

export interface CustomFieldDefUpdateRequest extends CustomFieldDefCreateRequest {
  readonly version: number
}

export interface ItemCustomField extends Versioned {
  readonly item_id?: string
  readonly field_def_id?: string
  readonly name: string
  readonly field_type: CustomFieldType
  readonly text_value?: string
  readonly number_value?: number
  readonly bool_value?: boolean
  readonly date_value?: string
}

export interface ItemCustomFieldListResponse {
  readonly custom_fields: readonly ItemCustomField[]
}

export interface GroupVisibility {
  readonly warranty_visible: boolean
  readonly sale_visible: boolean
  readonly purchase_visible: boolean
  readonly version?: number
}

export interface GroupVisibilityUpdateRequest {
  readonly warranty_visible: boolean
  readonly sale_visible: boolean
  readonly purchase_visible: boolean
  readonly version: number
}

export type AttachmentCategory = 'image' | 'manual' | 'warranty' | 'receipt' | 'general'

export interface Attachment extends Versioned {
  readonly item_id: string
  readonly category: AttachmentCategory
  readonly original_filename: string
  readonly content_type: string
  readonly size_bytes: number
  readonly sha256: string
  readonly has_thumbnail: boolean
}

export interface AttachmentListResponse {
  readonly attachments: readonly Attachment[]
}

export interface LoginResponse {
  readonly group_id: string
  readonly user_id: string
  readonly username: string
  readonly role: 'owner' | 'member'
}

export interface GroupMemberListItem {
  readonly id: string
  readonly username: string
  readonly role: string
  readonly joined_at: number
}

export interface GroupMembersResponse {
  readonly members: readonly GroupMemberListItem[]
}

export interface SessionListItem {
  readonly id: string
  readonly user_agent: string
  readonly created_from_ip: string
  readonly created_at: number
  readonly expires_at: number
  readonly revoked: boolean
  readonly revoked_at?: number
}

export interface SessionListResponse {
  readonly sessions: readonly SessionListItem[]
}

export interface DeviceTokenListItem {
  readonly id: string
  readonly device_label: string
  readonly created_at: number
  readonly revoked: boolean
  readonly revoked_at?: number
}

export interface DeviceTokenListResponse {
  readonly device_tokens: readonly DeviceTokenListItem[]
}

export interface InviteListItem {
  readonly id: string
  readonly created_by_user_id: string
  readonly created_at: number
  readonly expires_at: number
  readonly redeemed: boolean
  readonly redeemed_at?: number
  readonly redeemed_by_user_id?: string
}

export interface InviteListResponse {
  readonly invites: readonly InviteListItem[]
}

export interface InviteCreateResponse {
  readonly id: string
  readonly token: string
  readonly expires_at: number
}

export interface InviteRedeemRequest {
  readonly token: string
  readonly username: string
  readonly password: string
}

export interface InviteRedeemResponse {
  readonly group_id: string
  readonly user_id: string
  readonly username: string
}

export interface StatusResponse {
  readonly status: 'ok'
  readonly version: string
  readonly schema_version: number
}

export interface ReportValuationRow {
  readonly group_key: string
  readonly group_label: string
  readonly item_count: number
  readonly total_value_minor: number
}

export interface ReportValuationResponse {
  readonly rows: readonly ReportValuationRow[]
}

export interface ReportWarrantyExpiringRow {
  readonly item_id: string
  readonly item_name: string
  readonly expires_on: string
  readonly days_remaining: number
}

export interface ReportWarrantyExpiringResponse {
  readonly rows: readonly ReportWarrantyExpiringRow[]
}

export interface ReportPurchaseRow {
  readonly item_id: string
  readonly item_name: string
  readonly purchased_on: string
  readonly vendor: string
  readonly purchase_price_minor: number
}

export interface ReportPurchasesResponse {
  readonly rows: readonly ReportPurchaseRow[]
}

export interface ReportLocationItemCountRow {
  readonly location_id: string
  readonly location_name: string
  readonly item_count: number
}

export interface ReportItemCountByLocationResponse {
  readonly rows: readonly ReportLocationItemCountRow[]
}

export interface ImportUploadResponse {
  readonly import_id: string
}

export interface ImportPreviewRowError {
  readonly line: number
  readonly column?: string
  readonly message: string
}

export type ImportRowAction = 'create' | 'update' | 'unchanged' | 'error'

export type ImportRowChange =
  | 'name'
  | 'description'
  | 'quantity'
  | 'location_id'
  | 'labels'
  | 'identifications'
  | 'warranty'
  | 'purchase'
  | 'sale'
  | 'custom_fields'

export interface ImportPreviewRow {
  readonly line: number
  readonly action: ImportRowAction
  readonly item_id?: string
  readonly name?: string
  readonly changes?: readonly ImportRowChange[]
  readonly errors?: readonly ImportPreviewRowError[]
}

export interface ImportPreviewSummary {
  readonly create: number
  readonly update: number
  readonly unchanged: number
  readonly error: number
}

export interface ImportPreviewResponse {
  readonly import_id: string
  readonly rows: readonly ImportPreviewRow[]
  readonly summary: ImportPreviewSummary
}

export interface ImportCommitSummary {
  readonly created: number
  readonly updated: number
  readonly unchanged: number
}

export interface ImportCommitCreatedItem {
  readonly line: number
  readonly item_id: string
}

export interface ImportCommitResponse {
  readonly import_id: string
  readonly summary: ImportCommitSummary
  readonly created_items: readonly ImportCommitCreatedItem[]
}

export interface ImportCommitRejectedResponse {
  readonly import_id: string
  readonly rows: readonly ImportPreviewRow[]
  readonly summary: ImportPreviewSummary
}
