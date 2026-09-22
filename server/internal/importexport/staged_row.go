package importexport

import (
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
)

type StagedRow struct {
	Line int

	Action RowAction

	Errors []RowError

	SourceID string
	ID       string

	Name        string
	Description string
	Quantity    int64
	LocationID  sql.NullString
	ShortCode   string

	Labels          []string
	Identifications []StagedIdentification
	Attachments     []StagedAttachmentRef

	Warranty *StagedWarranty
	Purchase *StagedPurchase
	Sale     *StagedSale

	CustomFields []storage.ItemCustomField
}

type RowAction string

const (
	RowActionUnknown RowAction = ""
	RowActionCreate  RowAction = "create"
	RowActionUpdate  RowAction = "update"
	RowActionError   RowAction = "error"
)

type RowError struct {
	Line    int
	Column  string
	Message string
}

type StagedIdentification struct {
	Kind  string
	Value string
}

type StagedAttachmentRef struct {
	Category         string
	OriginalFilename string
	Sha256           string
}

type StagedWarranty struct {
	Holder     string
	Provider   string
	StartsOn   string
	ExpiresOn  string
	IsLifetime bool
	Notes      string
}

type StagedPurchase struct {
	Vendor             string
	PurchasedOn        string
	PurchasePriceMinor int64
	OrderReference     string
	Notes              string
}

type StagedSale struct {
	BuyerName      string
	SoldOn         string
	SalePriceMinor int64
	Notes          string
}
