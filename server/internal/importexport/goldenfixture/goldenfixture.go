package goldenfixture

import (
	"database/sql"
	_ "embed"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
)

//go:embed testdata/golden_export.csv
var csvBytes []byte

func CSV() []byte {
	out := make([]byte, len(csvBytes))
	copy(out, csvBytes)
	return out
}

func Defs() []storage.CustomFieldDef {
	return []storage.CustomFieldDef{
		{ID: "def-weight-kg", Name: "Weight (kg)", FieldType: "number"},
		{ID: "def-color", Name: "Color", FieldType: "text"},
		{ID: "def-model-number", Name: "Model:Number", FieldType: "text"},
	}
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

func Rows() []importexport.Row {
	return []importexport.Row{
		goldenChair(),
		goldenUnicodeEmptyItem(),
		goldenAllDetailBlocks(),
		goldenPurchaseOnlyItem(),
		goldenSaleOnlyItem(),
	}
}

func goldenChair() importexport.Row {
	return importexport.Row{
		Item: storage.Item{
			ID:          "golden-001",
			Name:        "Dining Chair, Oak",
			Description: "Seats four.\nCondition: \"like new\", minor scuff on left leg.",
			Quantity:    4,
			LocationID:  nullStr("loc-shelf-1"),
			ShortCode:   "AB12CD34",
		},
		LocationPath: "Garage/Shelf 1",
		Labels: []storage.Label{
			{ID: "lbl-furniture", Name: "Furniture"},
			{ID: "lbl-style", Name: "Style: Modern|Chic"},
		},
		Identifications: []storage.Identification{
			{Kind: "serial", Value: "SN-100:Special|Edition"},
		},
		Attachments: []storage.Attachment{
			{Category: "image", OriginalFilename: "chair (front) café.jpg", Sha256: "a1b2c3"},
		},
		Warranty: &storage.Warranty{
			Holder:     "Acme Möbel GmbH",
			Provider:   "Acme, Inc.",
			StartsOn:   nullStr("2023-01-01"),
			ExpiresOn:  nullStr("2025-01-01"),
			IsLifetime: 0,
			Notes:      "Registered \"online\"\nSee receipt for details.",
		},
		CustomFields: []storage.ItemCustomField{
			{FieldDefID: nullStr("def-model-number"), Name: "Model:Number", FieldType: "text", TextValue: nullStr("MN-123")},
			{Name: "Assembly Notes", FieldType: "text", TextValue: nullStr("Some assembly required")},
			{Name: "Registered", FieldType: "boolean", BoolValue: sql.NullInt64{Int64: 1, Valid: true}},
		},
	}
}

func goldenUnicodeEmptyItem() importexport.Row {
	return importexport.Row{
		Item: storage.Item{
			ID:       "golden-002",
			Name:     "テーブル 🪑 café",
			Quantity: 0,
		},
		CustomFields: []storage.ItemCustomField{
			{Name: "備考", FieldType: "text", TextValue: nullStr("日本語のテスト 🎌")},
			{Name: "InstalledOn", FieldType: "date", DateValue: nullStr("2024-03-04")},
		},
	}
}

func goldenAllDetailBlocks() importexport.Row {
	return importexport.Row{
		Item: storage.Item{
			ID:         "golden-003",
			Name:       `"Special" Reading Lamp`,
			Quantity:   1,
			LocationID: nullStr("loc-shelf-2"),
			ShortCode:  "LMP001",
		},
		LocationPath: "House/Garage/Shelf 2",
		Labels: []storage.Label{
			{ID: "lbl-pipe", Name: "|"},
			{ID: "lbl-comma", Name: "A,B"},
		},
		Identifications: []storage.Identification{
			{Kind: "serial", Value: "XYZ"},
		},
		Attachments: []storage.Attachment{
			{Category: "manual", OriginalFilename: "lamp-manual.pdf", Sha256: "deadbeef01"},
			{Category: "image", OriginalFilename: "lampe été 2024.png", Sha256: "cafef00d02"},
		},
		Warranty: &storage.Warranty{
			Holder:     "Bob's Warranty House",
			Provider:   "N/A",
			IsLifetime: 1,
			Notes:      "Lifetime coverage; call 1-800-000-0000.",
		},
		Purchase: &storage.Purchase{
			Vendor:             "Costco",
			PurchasedOn:        nullStr("2022-11-11"),
			PurchasePriceMinor: 999900,
		},
		Sale: &storage.Sale{
			BuyerName:      `Jane "JJ" Doe`,
			SoldOn:         nullStr("2025-06-01"),
			SalePriceMinor: 50000,
			Notes:          "Picked up in person; paid cash.",
		},
		CustomFields: []storage.ItemCustomField{
			{FieldDefID: nullStr("def-weight-kg"), Name: "Weight (kg)", FieldType: "number", NumberValue: sql.NullFloat64{Float64: 12.5, Valid: true}},
			{Name: "Registered", FieldType: "boolean", BoolValue: sql.NullInt64{Int64: 0, Valid: true}},
		},
	}
}

func goldenPurchaseOnlyItem() importexport.Row {
	return importexport.Row{
		Item: storage.Item{
			ID:         "golden-004",
			Name:       "Toolbox",
			Quantity:   2,
			LocationID: nullStr("loc-shelf-2"),
			ShortCode:  "TB042",
		},
		LocationPath: "House/Garage/Shelf 2",
		Purchase: &storage.Purchase{
			Vendor:             "Hardware Depot",
			PurchasedOn:        nullStr("2021-05-20"),
			PurchasePriceMinor: 4599,
			OrderReference:     "ORD-200,B",
			Notes:              "Discontinued model; keep for spares.",
		},
		CustomFields: []storage.ItemCustomField{
			{Name: `A|B:C`, FieldType: "text", TextValue: sql.NullString{String: `value|with:delims\and\backslash`, Valid: true}},
		},
	}
}

func goldenSaleOnlyItem() importexport.Row {
	return importexport.Row{
		Item: storage.Item{
			ID:         "golden-005",
			Name:       "Box\nSet",
			Quantity:   10,
			LocationID: nullStr("loc-attic"),
			ShortCode:  "ZZ99YY88",
		},
		LocationPath: "Attic",
		Attachments: []storage.Attachment{
			{Category: "manual", OriginalFilename: "guide.pdf", Sha256: "deadbeef"},
		},
		Sale: &storage.Sale{
			BuyerName:      "Sam",
			SoldOn:         nullStr("2025-01-01"),
			SalePriceMinor: 2500,
		},
	}
}
