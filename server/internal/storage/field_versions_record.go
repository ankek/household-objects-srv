package storage

import (
	"context"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

func recordFieldVersionsTx(ctx context.Context, q *gen.Queries, group, entityType, entityID string, version, now int64, changedFields []string) error {
	for _, field := range changedFields {
		if err := upsertFieldVersionTx(ctx, q, group, UpsertFieldVersionParams{
			EntityType: entityType,
			EntityID:   entityID,
			FieldName:  field,
			Version:    version,
			Now:        now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func diffItemFieldVersions(before, after Item) []string {
	var changed []string
	if before.Name != after.Name {
		changed = append(changed, "name")
	}
	if before.Description != after.Description {
		changed = append(changed, "description")
	}
	if before.LocationID != after.LocationID {
		changed = append(changed, "location_id")
	}
	if before.Quantity != after.Quantity {
		changed = append(changed, "quantity")
	}
	return changed
}

func diffWarrantyFieldVersions(before, after Warranty) []string {
	var changed []string
	if before.Holder != after.Holder {
		changed = append(changed, "holder")
	}
	if before.Provider != after.Provider {
		changed = append(changed, "provider")
	}
	if before.StartsOn != after.StartsOn {
		changed = append(changed, "starts_on")
	}
	if before.ExpiresOn != after.ExpiresOn {
		changed = append(changed, "expires_on")
	}
	if before.IsLifetime != after.IsLifetime {
		changed = append(changed, "is_lifetime")
	}
	if before.Notes != after.Notes {
		changed = append(changed, "notes")
	}
	return changed
}

func diffSaleFieldVersions(before, after Sale) []string {
	var changed []string
	if before.BuyerName != after.BuyerName {
		changed = append(changed, "buyer_name")
	}
	if before.SoldOn != after.SoldOn {
		changed = append(changed, "sold_on")
	}
	if before.SalePriceMinor != after.SalePriceMinor {
		changed = append(changed, "sale_price_minor")
	}
	if before.Notes != after.Notes {
		changed = append(changed, "notes")
	}
	return changed
}

func diffPurchaseFieldVersions(before, after Purchase) []string {
	var changed []string
	if before.Vendor != after.Vendor {
		changed = append(changed, "vendor")
	}
	if before.PurchasedOn != after.PurchasedOn {
		changed = append(changed, "purchased_on")
	}
	if before.PurchasePriceMinor != after.PurchasePriceMinor {
		changed = append(changed, "purchase_price_minor")
	}
	if before.OrderReference != after.OrderReference {
		changed = append(changed, "order_reference")
	}
	if before.Notes != after.Notes {
		changed = append(changed, "notes")
	}
	return changed
}

func diffIdentificationFieldVersions(before, after Identification) []string {
	var changed []string
	if before.Kind != after.Kind {
		changed = append(changed, "kind")
	}
	if before.Value != after.Value {
		changed = append(changed, "value")
	}
	return changed
}

func diffItemCustomFieldFieldVersions(before, after ItemCustomField) []string {
	var changed []string
	if before.FieldDefID != after.FieldDefID {
		changed = append(changed, "field_def_id")
	}
	if before.Name != after.Name {
		changed = append(changed, "name")
	}
	if before.FieldType != after.FieldType {
		changed = append(changed, "field_type")
	}
	if before.TextValue != after.TextValue {
		changed = append(changed, "text_value")
	}
	if before.NumberValue != after.NumberValue {
		changed = append(changed, "number_value")
	}
	if before.BoolValue != after.BoolValue {
		changed = append(changed, "bool_value")
	}
	if before.DateValue != after.DateValue {
		changed = append(changed, "date_value")
	}
	return changed
}

func diffLocationFieldVersions(before, after Location) []string {
	var changed []string
	if before.Name != after.Name {
		changed = append(changed, "name")
	}
	if before.ParentID != after.ParentID {
		changed = append(changed, "parent_id")
	}
	return changed
}

func diffLabelFieldVersions(before, after Label) []string {
	var changed []string
	if before.Name != after.Name {
		changed = append(changed, "name")
	}
	if before.Color != after.Color {
		changed = append(changed, "color")
	}
	return changed
}
