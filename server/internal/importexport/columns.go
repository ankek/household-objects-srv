package importexport

const (
	ColumnID                     = "id"
	ColumnName                   = "name"
	ColumnDescription            = "description"
	ColumnQuantity               = "quantity"
	ColumnLocationPath           = "location_path"
	ColumnLocationID             = "location_id"
	ColumnShortCode              = "short_code"
	ColumnLabels                 = "labels"
	ColumnIdentifications        = "identifications"
	ColumnAttachments            = "attachments"
	ColumnWarrantyHolder         = "warranty_holder"
	ColumnWarrantyProvider       = "warranty_provider"
	ColumnWarrantyStartsOn       = "warranty_starts_on"
	ColumnWarrantyExpiresOn      = "warranty_expires_on"
	ColumnWarrantyIsLifetime     = "warranty_is_lifetime"
	ColumnWarrantyNotes          = "warranty_notes"
	ColumnPurchaseVendor         = "purchase_vendor"
	ColumnPurchasePurchasedOn    = "purchase_purchased_on"
	ColumnPurchasePriceMinor     = "purchase_price_minor"
	ColumnPurchaseOrderReference = "purchase_order_reference"
	ColumnPurchaseNotes          = "purchase_notes"
	ColumnSaleBuyerName          = "sale_buyer_name"
	ColumnSaleSoldOn             = "sale_sold_on"
	ColumnSalePriceMinor         = "sale_price_minor"
	ColumnSaleNotes              = "sale_notes"
)

const CustomFieldColumnPrefix = "cf:"

const CustomFieldAdHocColumnPrefix = "cfx:"

var FixedColumns = []string{
	ColumnID,
	ColumnName,
	ColumnDescription,
	ColumnQuantity,
	ColumnLocationPath,
	ColumnLocationID,
	ColumnShortCode,
	ColumnLabels,
	ColumnIdentifications,
	ColumnAttachments,
	ColumnWarrantyHolder,
	ColumnWarrantyProvider,
	ColumnWarrantyStartsOn,
	ColumnWarrantyExpiresOn,
	ColumnWarrantyIsLifetime,
	ColumnWarrantyNotes,
	ColumnPurchaseVendor,
	ColumnPurchasePurchasedOn,
	ColumnPurchasePriceMinor,
	ColumnPurchaseOrderReference,
	ColumnPurchaseNotes,
	ColumnSaleBuyerName,
	ColumnSaleSoldOn,
	ColumnSalePriceMinor,
	ColumnSaleNotes,
}
