package importexport

import (
	"database/sql"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"strconv"
)

type rowFields struct {
	record []string
	layout headerLayout
}

func (f rowFields) get(col string) string {
	idx, ok := f.layout.fixedIdx[col]
	if !ok || idx >= len(f.record) {
		return ""
	}
	return f.record[idx]
}

func newRowErr(line int, col, format string, args ...any) RowError {
	return RowError{Line: line, Column: col, Message: fmt.Sprintf(format, args...)}
}

func parseRow(
	line int,
	record []string,
	layout headerLayout,
	existingItemIDs map[string]struct{},
	defsByID map[string]storage.CustomFieldDef,
	defsByUniqueName map[string]storage.CustomFieldDef,
	defNameCounts map[string]int,
) StagedRow {
	f := rowFields{record: record, layout: layout}
	var errs []RowError

	sourceID := f.get(ColumnID)

	name := f.get(ColumnName)
	if name == "" {
		errs = append(errs, newRowErr(line, ColumnName, "name is required"))
	}

	quantity, err := parseQuantity(f.get(ColumnQuantity))
	if err != nil {
		errs = append(errs, newRowErr(line, ColumnQuantity, "%v", err))
	}

	var locationID sql.NullString
	if raw := f.get(ColumnLocationID); raw != "" {
		locationID = sql.NullString{String: raw, Valid: true}
	}

	labels, labelErrs := parseLabels(line, f.get(ColumnLabels))
	errs = append(errs, labelErrs...)

	identifications, idErrs := parseIdentifications(line, f.get(ColumnIdentifications))
	errs = append(errs, idErrs...)

	attachments, attErrs := parseAttachments(line, f.get(ColumnAttachments))
	errs = append(errs, attErrs...)

	warranty, warrantyErrs := parseWarrantyBlock(line, f)
	errs = append(errs, warrantyErrs...)

	purchase, purchaseErrs := parsePurchaseBlock(line, f)
	errs = append(errs, purchaseErrs...)

	sale, saleErrs := parseSaleBlock(line, f)
	errs = append(errs, saleErrs...)

	customFields, cfErrs := parseCustomFields(line, record, layout.dynCols, defsByID, defsByUniqueName, defNameCounts)
	errs = append(errs, cfErrs...)

	row := StagedRow{
		Line:            line,
		SourceID:        sourceID,
		Name:            name,
		Description:     f.get(ColumnDescription),
		Quantity:        quantity,
		LocationID:      locationID,
		ShortCode:       f.get(ColumnShortCode),
		Labels:          labels,
		Identifications: identifications,
		Attachments:     attachments,
		Warranty:        warranty,
		Purchase:        purchase,
		Sale:            sale,
		CustomFields:    customFields,
	}

	if len(errs) > 0 {
		row.Errors = errs
		row.Action = RowActionError
		return row
	}

	if sourceID != "" {
		if _, ok := existingItemIDs[sourceID]; ok {
			row.Action = RowActionUpdate
			row.ID = sourceID
			return row
		}
	}
	row.Action = RowActionCreate
	return row
}

func parseLabels(line int, cell string) ([]string, []RowError) {
	raw := SplitEscaped(cell, '|')
	if len(raw) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(raw))
	var errs []RowError
	for i, r := range raw {
		name := UnescapeSubValue(r)
		if name == "" {
			errs = append(errs, newRowErr(line, ColumnLabels, "entry %d is empty", i+1))
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, errs
	}
	return names, errs
}

func parseIdentifications(line int, cell string) ([]StagedIdentification, []RowError) {
	rawPairs := SplitEscaped(cell, '|')
	if len(rawPairs) == 0 {
		return nil, nil
	}
	out := make([]StagedIdentification, 0, len(rawPairs))
	var errs []RowError
	for i, rawPair := range rawPairs {
		parts := SplitEscaped(rawPair, ':')
		if len(parts) != 2 {
			errs = append(errs, newRowErr(line, ColumnIdentifications, "entry %d: want kind:value, got %d part(s)", i+1, len(parts)))
			continue
		}
		kind := UnescapeSubValue(parts[0])
		value := UnescapeSubValue(parts[1])
		valid := true
		if !identificationKinds[kind] {
			errs = append(errs, newRowErr(line, ColumnIdentifications, "entry %d: kind must be one of serial, model, asset_tag, barcode, other (got %q)", i+1, kind))
			valid = false
		}
		if value == "" {
			errs = append(errs, newRowErr(line, ColumnIdentifications, "entry %d: value is required", i+1))
			valid = false
		}
		if valid {
			out = append(out, StagedIdentification{Kind: kind, Value: value})
		}
	}
	if len(out) == 0 {
		return nil, errs
	}
	return out, errs
}

func parseAttachments(line int, cell string) ([]StagedAttachmentRef, []RowError) {
	rawTriples := SplitEscaped(cell, '|')
	if len(rawTriples) == 0 {
		return nil, nil
	}
	out := make([]StagedAttachmentRef, 0, len(rawTriples))
	var errs []RowError
	for i, rawTriple := range rawTriples {
		parts := SplitEscaped(rawTriple, ':')
		if len(parts) != 3 {
			errs = append(errs, newRowErr(line, ColumnAttachments, "entry %d: want category:filename:sha256, got %d part(s)", i+1, len(parts)))
			continue
		}
		category := UnescapeSubValue(parts[0])
		filename := UnescapeSubValue(parts[1])
		sha := UnescapeSubValue(parts[2])
		valid := true
		if category == "" {
			errs = append(errs, newRowErr(line, ColumnAttachments, "entry %d: category is required", i+1))
			valid = false
		} else if !attachmentCategories[category] {
			errs = append(errs, newRowErr(line, ColumnAttachments, "entry %d: category must be one of image, manual, warranty, receipt, general (got %q)", i+1, category))
			valid = false
		}
		if filename == "" {
			errs = append(errs, newRowErr(line, ColumnAttachments, "entry %d: filename is required", i+1))
			valid = false
		}
		if sha == "" {
			errs = append(errs, newRowErr(line, ColumnAttachments, "entry %d: sha256 is required", i+1))
			valid = false
		}
		if valid {
			out = append(out, StagedAttachmentRef{Category: category, OriginalFilename: filename, Sha256: sha})
		}
	}
	if len(out) == 0 {
		return nil, errs
	}
	return out, errs
}

func parseWarrantyBlock(line int, f rowFields) (*StagedWarranty, []RowError) {
	holder := f.get(ColumnWarrantyHolder)
	provider := f.get(ColumnWarrantyProvider)
	startsOn := f.get(ColumnWarrantyStartsOn)
	expiresOn := f.get(ColumnWarrantyExpiresOn)
	isLifetimeRaw := f.get(ColumnWarrantyIsLifetime)
	notes := f.get(ColumnWarrantyNotes)
	if holder == "" && provider == "" && startsOn == "" && expiresOn == "" && isLifetimeRaw == "" && notes == "" {
		return nil, nil
	}

	var errs []RowError
	if err := parseISODate(startsOn); err != nil {
		errs = append(errs, newRowErr(line, ColumnWarrantyStartsOn, "%v", err))
	}
	if err := parseISODate(expiresOn); err != nil {
		errs = append(errs, newRowErr(line, ColumnWarrantyExpiresOn, "%v", err))
	}
	isLifetime := false
	if isLifetimeRaw != "" {
		b, err := parseBool(isLifetimeRaw)
		if err != nil {
			errs = append(errs, newRowErr(line, ColumnWarrantyIsLifetime, "%v", err))
		} else {
			isLifetime = b
		}
	}

	return &StagedWarranty{
		Holder:     holder,
		Provider:   provider,
		StartsOn:   startsOn,
		ExpiresOn:  expiresOn,
		IsLifetime: isLifetime,
		Notes:      notes,
	}, errs
}

func parsePurchaseBlock(line int, f rowFields) (*StagedPurchase, []RowError) {
	vendor := f.get(ColumnPurchaseVendor)
	purchasedOn := f.get(ColumnPurchasePurchasedOn)
	priceRaw := f.get(ColumnPurchasePriceMinor)
	orderReference := f.get(ColumnPurchaseOrderReference)
	notes := f.get(ColumnPurchaseNotes)
	if vendor == "" && purchasedOn == "" && priceRaw == "" && orderReference == "" && notes == "" {
		return nil, nil
	}

	var errs []RowError
	if err := parseISODate(purchasedOn); err != nil {
		errs = append(errs, newRowErr(line, ColumnPurchasePurchasedOn, "%v", err))
	}
	price, err := parsePriceMinor(priceRaw)
	if err != nil {
		errs = append(errs, newRowErr(line, ColumnPurchasePriceMinor, "%v", err))
	}

	return &StagedPurchase{
		Vendor:             vendor,
		PurchasedOn:        purchasedOn,
		PurchasePriceMinor: price,
		OrderReference:     orderReference,
		Notes:              notes,
	}, errs
}

func parseSaleBlock(line int, f rowFields) (*StagedSale, []RowError) {
	buyerName := f.get(ColumnSaleBuyerName)
	soldOn := f.get(ColumnSaleSoldOn)
	priceRaw := f.get(ColumnSalePriceMinor)
	notes := f.get(ColumnSaleNotes)
	if buyerName == "" && soldOn == "" && priceRaw == "" && notes == "" {
		return nil, nil
	}

	var errs []RowError
	if err := parseISODate(soldOn); err != nil {
		errs = append(errs, newRowErr(line, ColumnSaleSoldOn, "%v", err))
	}
	price, err := parsePriceMinor(priceRaw)
	if err != nil {
		errs = append(errs, newRowErr(line, ColumnSalePriceMinor, "%v", err))
	}

	return &StagedSale{
		BuyerName:      buyerName,
		SoldOn:         soldOn,
		SalePriceMinor: price,
		Notes:          notes,
	}, errs
}

func parseCustomFields(
	line int,
	record []string,
	dynCols []dynamicColumn,
	defsByID map[string]storage.CustomFieldDef,
	defsByUniqueName map[string]storage.CustomFieldDef,
	defNameCounts map[string]int,
) ([]storage.ItemCustomField, []RowError) {
	if len(dynCols) == 0 {
		return nil, nil
	}
	var out []storage.ItemCustomField
	var errs []RowError

	for _, dc := range dynCols {
		var cell string
		if dc.index < len(record) {
			cell = record[dc.index]
		}
		if cell == "" {
			continue
		}

		var fieldType, valueName, fieldDefID string
		switch dc.parsed.Kind {
		case CustomFieldColumnAdHoc:
			fieldType = dc.parsed.FieldType
			valueName = dc.parsed.Name
			if !customFieldTypes[fieldType] {
				errs = append(errs, newRowErr(line, dc.header, "field type %q is not one of text, number, boolean, date", fieldType))
				continue
			}
		case CustomFieldColumnDef:
			var def storage.CustomFieldDef
			var ok bool
			if dc.parsed.DefID != "" {
				def, ok = defsByID[dc.parsed.DefID]
				if !ok {
					errs = append(errs, newRowErr(line, dc.header, "no live custom field definition with id %q (renamed or deleted since export)", dc.parsed.DefID))
					continue
				}
			} else {
				if defNameCounts[dc.parsed.Name] > 1 {
					errs = append(errs, newRowErr(line, dc.header, "definition name %q is now ambiguous among the group's live definitions, and this column carries no #<def_id> to disambiguate", dc.parsed.Name))
					continue
				}
				def, ok = defsByUniqueName[dc.parsed.Name]
				if !ok {
					errs = append(errs, newRowErr(line, dc.header, "no live custom field definition named %q (renamed or deleted since export)", dc.parsed.Name))
					continue
				}
			}
			fieldType = def.FieldType
			fieldDefID = def.ID
			valueName = def.Name
		default:
			continue
		}

		v := storage.ItemCustomField{Name: valueName, FieldType: fieldType}
		if fieldDefID != "" {
			v.FieldDefID = sql.NullString{String: fieldDefID, Valid: true}
		}

		switch fieldType {
		case "text":
			v.TextValue = sql.NullString{String: cell, Valid: true}
		case "number":
			n, err := strconv.ParseFloat(cell, 64)
			if err != nil {
				errs = append(errs, newRowErr(line, dc.header, "must be a number, got %q", cell))
				continue
			}
			v.NumberValue = sql.NullFloat64{Float64: n, Valid: true}
		case "boolean":
			b, err := parseBool(cell)
			if err != nil {
				errs = append(errs, newRowErr(line, dc.header, "%v", err))
				continue
			}
			v.BoolValue = sql.NullInt64{Valid: true}
			if b {
				v.BoolValue.Int64 = 1
			}
		case "date":
			if err := parseISODate(cell); err != nil {
				errs = append(errs, newRowErr(line, dc.header, "%v", err))
				continue
			}
			v.DateValue = sql.NullString{String: cell, Valid: true}
		}

		out = append(out, v)
	}

	return out, errs
}
