package importexport

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"sort"
)

func DiffStagedRow(current Row, proposed StagedRow, defs []storage.CustomFieldDef) []string {
	var diffs []string

	item := current.Item
	if item.Name != proposed.Name {
		diffs = append(diffs, "name")
	}
	if item.Description != proposed.Description {
		diffs = append(diffs, "description")
	}
	if item.Quantity != proposed.Quantity {
		diffs = append(diffs, "quantity")
	}
	if nullString(item.LocationID.Valid, item.LocationID.String) != nullString(proposed.LocationID.Valid, proposed.LocationID.String) {
		diffs = append(diffs, "location_id")
	}
	if !labelsEqual(current.Labels, proposed.Labels) {
		diffs = append(diffs, "labels")
	}
	if !identificationsEqual(current.Identifications, proposed.Identifications) {
		diffs = append(diffs, "identifications")
	}
	if warrantyFieldsOf(current.Warranty) != stagedWarrantyFieldsOf(proposed.Warranty) {
		diffs = append(diffs, "warranty")
	}
	if purchaseFieldsOf(current.Purchase) != stagedPurchaseFieldsOf(proposed.Purchase) {
		diffs = append(diffs, "purchase")
	}
	if saleFieldsOf(current.Sale) != stagedSaleFieldsOf(proposed.Sale) {
		diffs = append(diffs, "sale")
	}
	if !customFieldsEqual(current.CustomFields, proposed.CustomFields, defs) {
		diffs = append(diffs, "custom_fields")
	}

	return diffs
}

func labelsEqual(current []storage.Label, proposed []string) bool {
	a := make(map[string]struct{}, len(current))
	for _, l := range current {
		a[l.Name] = struct{}{}
	}
	b := make(map[string]struct{}, len(proposed))
	for _, name := range proposed {
		b[name] = struct{}{}
	}
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			return false
		}
	}
	return true
}

func identificationsEqual(current []storage.Identification, proposed []StagedIdentification) bool {
	if len(current) != len(proposed) {
		return false
	}
	a := make([]string, len(current))
	for i, id := range current {
		a[i] = id.Kind + "\x00" + id.Value
	}
	b := make([]string, len(proposed))
	for i, id := range proposed {
		b[i] = id.Kind + "\x00" + id.Value
	}
	return sortedStringsEqual(a, b)
}

func sortedStringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

type warrantyFields struct {
	Holder, Provider, StartsOn, ExpiresOn, Notes string
	IsLifetime                                   bool
}

func warrantyFieldsOf(w *storage.Warranty) warrantyFields {
	if w == nil {
		return warrantyFields{}
	}
	return warrantyFields{
		Holder:     w.Holder,
		Provider:   w.Provider,
		StartsOn:   nullString(w.StartsOn.Valid, w.StartsOn.String),
		ExpiresOn:  nullString(w.ExpiresOn.Valid, w.ExpiresOn.String),
		IsLifetime: w.IsLifetime != 0,
		Notes:      w.Notes,
	}
}

func stagedWarrantyFieldsOf(w *StagedWarranty) warrantyFields {
	if w == nil {
		return warrantyFields{}
	}
	return warrantyFields{
		Holder:     w.Holder,
		Provider:   w.Provider,
		StartsOn:   w.StartsOn,
		ExpiresOn:  w.ExpiresOn,
		IsLifetime: w.IsLifetime,
		Notes:      w.Notes,
	}
}

type purchaseFields struct {
	Vendor, PurchasedOn, OrderReference, Notes string
	PurchasePriceMinor                         int64
}

func purchaseFieldsOf(p *storage.Purchase) purchaseFields {
	if p == nil {
		return purchaseFields{}
	}
	return purchaseFields{
		Vendor:             p.Vendor,
		PurchasedOn:        nullString(p.PurchasedOn.Valid, p.PurchasedOn.String),
		PurchasePriceMinor: p.PurchasePriceMinor,
		OrderReference:     p.OrderReference,
		Notes:              p.Notes,
	}
}

func stagedPurchaseFieldsOf(p *StagedPurchase) purchaseFields {
	if p == nil {
		return purchaseFields{}
	}
	return purchaseFields{
		Vendor:             p.Vendor,
		PurchasedOn:        p.PurchasedOn,
		PurchasePriceMinor: p.PurchasePriceMinor,
		OrderReference:     p.OrderReference,
		Notes:              p.Notes,
	}
}

type saleFields struct {
	BuyerName, SoldOn, Notes string
	SalePriceMinor           int64
}

func saleFieldsOf(s *storage.Sale) saleFields {
	if s == nil {
		return saleFields{}
	}
	return saleFields{
		BuyerName:      s.BuyerName,
		SoldOn:         nullString(s.SoldOn.Valid, s.SoldOn.String),
		SalePriceMinor: s.SalePriceMinor,
		Notes:          s.Notes,
	}
}

func stagedSaleFieldsOf(s *StagedSale) saleFields {
	if s == nil {
		return saleFields{}
	}
	return saleFields{
		BuyerName:      s.BuyerName,
		SoldOn:         s.SoldOn,
		SalePriceMinor: s.SalePriceMinor,
		Notes:          s.Notes,
	}
}

func customFieldsEqual(current, proposed []storage.ItemCustomField, defs []storage.CustomFieldDef) bool {
	liveDefIDs := liveCustomFieldDefIDs(defs)

	curByDef := make(map[string]storage.ItemCustomField, len(current))
	curAdHoc := make(map[adHocColumnKey]storage.ItemCustomField, len(current))
	for _, v := range current {
		if isAdHocCustomFieldValue(v, liveDefIDs) {
			k := adHocColumnKey{fieldType: v.FieldType, name: v.Name}
			if _, exists := curAdHoc[k]; !exists {
				curAdHoc[k] = v
			}
			continue
		}
		curByDef[v.FieldDefID.String] = v
	}

	propByDef := make(map[string]storage.ItemCustomField, len(proposed))
	propAdHoc := make(map[adHocColumnKey]storage.ItemCustomField, len(proposed))
	for _, v := range proposed {
		if v.FieldDefID.Valid {
			propByDef[v.FieldDefID.String] = v
			continue
		}
		k := adHocColumnKey{fieldType: v.FieldType, name: v.Name}
		if _, exists := propAdHoc[k]; !exists {
			propAdHoc[k] = v
		}
	}

	if len(curByDef) != len(propByDef) || len(curAdHoc) != len(propAdHoc) {
		return false
	}
	for defID, cv := range curByDef {
		pv, ok := propByDef[defID]
		if !ok || !customFieldValueEqual(cv, pv) {
			return false
		}
	}
	for k, cv := range curAdHoc {
		pv, ok := propAdHoc[k]
		if !ok || !customFieldValueEqual(cv, pv) {
			return false
		}
	}
	return true
}

func customFieldValueEqual(a, b storage.ItemCustomField) bool {
	if a.FieldType != b.FieldType {
		return false
	}
	switch a.FieldType {
	case "text":
		return nullString(a.TextValue.Valid, a.TextValue.String) == nullString(b.TextValue.Valid, b.TextValue.String)
	case "number":
		av, bv := 0.0, 0.0
		if a.NumberValue.Valid {
			av = a.NumberValue.Float64
		}
		if b.NumberValue.Valid {
			bv = b.NumberValue.Float64
		}
		return a.NumberValue.Valid == b.NumberValue.Valid && av == bv
	case "boolean":
		av := a.BoolValue.Valid && a.BoolValue.Int64 != 0
		bv := b.BoolValue.Valid && b.BoolValue.Int64 != 0
		return av == bv
	case "date":
		return nullString(a.DateValue.Valid, a.DateValue.String) == nullString(b.DateValue.Valid, b.DateValue.String)
	default:
		return true
	}
}
