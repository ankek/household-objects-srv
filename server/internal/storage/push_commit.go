package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"github.com/google/uuid"
	"sort"
)

const entityConflictField = "_entity"

var (
	ErrPushUnknownEntityType  = errors.New("storage: push: entity_type is not a member of the closed sync entity set")
	ErrPushUnknownField       = errors.New("storage: push: field name is not recognised for this entity_type")
	ErrPushMissingField       = errors.New("storage: push: a required field is missing from this mutation")
	ErrPushInvalidFieldValue  = errors.New("storage: push: a field value does not decode to the type this field expects")
	ErrPushAttachmentRejected = errors.New("storage: push: attachment mutations are rejected; attachment bytes travel through the photo-queue, never through push fields")
	ErrPushCreationOnly       = errors.New("storage: push: this entity_type is creation-only over push; base_version must be 0")
)

var pushCommitFailAfterEntityWrite func() error

type PushMutation struct {
	MutationID  string
	EntityType  string
	EntityID    string
	BaseVersion int64
	Fields      map[string]json.RawMessage
	Now         int64
}

func (m PushMutation) validate() error {
	switch {
	case m.MutationID == "":
		return errors.New("storage: PushMutation: MutationID is empty")
	case m.EntityType == "":
		return errors.New("storage: PushMutation: EntityType is empty")
	case m.EntityID == "":
		return errors.New("storage: PushMutation: EntityID is empty")
	case m.BaseVersion < 0:
		return errors.New("storage: PushMutation: BaseVersion must not be negative")
	case m.Now <= 0:
		return errors.New("storage: PushMutation: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type PushOutcome struct {
	Skipped        bool
	Applied        bool
	EntityType     string
	EntityID       string
	Version        int64
	ConflictFields []string
}

type PushCommitRepository interface {
	ApplyMutation(ctx context.Context, m PushMutation) (PushOutcome, error)

	Watermark(ctx context.Context) (int64, error)
}

type pushCommitRepository struct {
	group string
	store *db.Store
}

func (s *Storage) ForGroupPushCommit(g GroupID) (PushCommitRepository, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if g.IsZero() {
		return nil, fmt.Errorf("%w: group id is the zero value", ErrNoGroup)
	}
	return pushCommitRepository{group: g.id, store: s.store}, nil
}

func (r pushCommitRepository) ApplyMutation(ctx context.Context, m PushMutation) (PushOutcome, error) {
	if r.store == nil {
		return PushOutcome{}, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if err := m.validate(); err != nil {
		return PushOutcome{}, err
	}

	var outcome PushOutcome
	err := r.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		if _, err := lookupMutationLedgerEntryTx(ctx, q, r.group, m.MutationID); err == nil {
			outcome = PushOutcome{Skipped: true, EntityType: m.EntityType, EntityID: m.EntityID}
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("storage: push: check mutation ledger for %q: %w", m.MutationID, err)
		}

		handler, ok := pushEntityDispatch[m.EntityType]
		if !ok {
			return fmt.Errorf("%w: %q", ErrPushUnknownEntityType, m.EntityType)
		}

		result, err := handler(ctx, tx, q, r.group, m)
		if err != nil {
			return err
		}
		result.EntityType = m.EntityType
		if result.EntityID == "" {
			result.EntityID = m.EntityID
		}
		outcome = result

		if pushCommitFailAfterEntityWrite != nil {
			if err := pushCommitFailAfterEntityWrite(); err != nil {
				return err
			}
		}

		ledgerOutcome := MutationOutcomeConflict
		if result.Applied {
			ledgerOutcome = MutationOutcomeApplied
		}

		ledgerID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("storage: push: mint ledger row id: %w", err)
		}
		if _, err := insertMutationLedgerEntryTx(ctx, q, r.group, InsertMutationLedgerEntryParams{
			ID:         ledgerID.String(),
			MutationID: m.MutationID,
			EntityType: m.EntityType,
			EntityID:   outcome.EntityID,
			Outcome:    ledgerOutcome,
			Now:        m.Now,
		}); err != nil {
			return fmt.Errorf("storage: push: record mutation ledger row for %q: %w", m.MutationID, err)
		}
		return nil
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return outcome, nil
}

func (r pushCommitRepository) Watermark(ctx context.Context) (int64, error) {
	row, err := gen.New(r.store.Reader()).GetGroup(ctx, r.group)
	if err != nil {
		return 0, fmt.Errorf("storage: push: read group %q watermark: %w", r.group, err)
	}
	return row.ChangeSeqCounter, nil
}

type pushEntityHandler func(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error)

var pushEntityDispatch = map[string]pushEntityHandler{
	"item":                 pushItem,
	"warranty_block":       pushWarrantyBlock,
	"sold_to_block":        pushSoldToBlock,
	"purchased_from_block": pushPurchasedFromBlock,
	"item_identification":  pushItemIdentification,
	"item_custom_field":    pushItemCustomField,
	"stock_adjustment":     pushStockAdjustment,
	"location":             pushLocation,
	"label":                pushLabel,
	"item_label":           pushItemLabel,
	"attachment":           pushAttachment,
}

var pushVersionMismatchPolicy = pushWholeEntityMismatch

func pushWholeEntityMismatch(_ context.Context, _ *gen.Queries, _, _, _ string, fieldNames []string) (conflict, apply []string, err error) {
	return fieldNames, nil, nil
}

func pushFieldNames(fields map[string]json.RawMessage) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func pushRequireKnownFields(fields map[string]json.RawMessage, allowed map[string]bool) error {
	for name := range fields {
		if !allowed[name] {
			return fmt.Errorf("%w: %q", ErrPushUnknownField, name)
		}
	}
	return nil
}

func pushRequireField(fields map[string]json.RawMessage, name string) error {
	if _, ok := fields[name]; !ok {
		return fmt.Errorf("%w: %q", ErrPushMissingField, name)
	}
	return nil
}

func pushDecodeString(fields map[string]json.RawMessage, name string) (string, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return "", false, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", true, fmt.Errorf("%w: field %q: %v", ErrPushInvalidFieldValue, name, err)
	}
	return v, true, nil
}

func pushDecodeInt64(fields map[string]json.RawMessage, name string) (int64, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return 0, false, nil
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, true, fmt.Errorf("%w: field %q: %v", ErrPushInvalidFieldValue, name, err)
	}
	return v, true, nil
}

func pushDecodeBool(fields map[string]json.RawMessage, name string) (bool, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return false, false, nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, true, fmt.Errorf("%w: field %q: %v", ErrPushInvalidFieldValue, name, err)
	}
	return v, true, nil
}

func pushDecodeNullableString(fields map[string]json.RawMessage, name string) (*string, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, true, fmt.Errorf("%w: field %q: %v", ErrPushInvalidFieldValue, name, err)
	}
	return &v, true, nil
}

func pushDecodeNullableFloat64(fields map[string]json.RawMessage, name string) (*float64, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, true, fmt.Errorf("%w: field %q: %v", ErrPushInvalidFieldValue, name, err)
	}
	return &v, true, nil
}

func pushDecodeNullableBool(fields map[string]json.RawMessage, name string) (*bool, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, true, fmt.Errorf("%w: field %q: %v", ErrPushInvalidFieldValue, name, err)
	}
	return &v, true, nil
}

func pushAllowAll(string) bool { return true }

func pushAllowSet(names []string) func(string) bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return func(name string) bool { return set[name] }
}

func pushMergeString(fields map[string]json.RawMessage, name string, allow func(string) bool, current string) (string, error) {
	if !allow(name) {
		return current, nil
	}
	v, present, err := pushDecodeString(fields, name)
	if err != nil {
		return "", err
	}
	if !present {
		return current, nil
	}
	return v, nil
}

func pushMergeInt64(fields map[string]json.RawMessage, name string, allow func(string) bool, current int64) (int64, error) {
	if !allow(name) {
		return current, nil
	}
	v, present, err := pushDecodeInt64(fields, name)
	if err != nil {
		return 0, err
	}
	if !present {
		return current, nil
	}
	return v, nil
}

func pushMergeBool(fields map[string]json.RawMessage, name string, allow func(string) bool, current bool) (bool, error) {
	if !allow(name) {
		return current, nil
	}
	v, present, err := pushDecodeBool(fields, name)
	if err != nil {
		return false, err
	}
	if !present {
		return current, nil
	}
	return v, nil
}

func pushMergeNullableString(fields map[string]json.RawMessage, name string, allow func(string) bool, current *string) (*string, error) {
	if !allow(name) {
		return current, nil
	}
	v, present, err := pushDecodeNullableString(fields, name)
	if err != nil {
		return nil, err
	}
	if !present {
		return current, nil
	}
	return v, nil
}

func pushMergeNullableFloat64(fields map[string]json.RawMessage, name string, allow func(string) bool, current *float64) (*float64, error) {
	if !allow(name) {
		return current, nil
	}
	v, present, err := pushDecodeNullableFloat64(fields, name)
	if err != nil {
		return nil, err
	}
	if !present {
		return current, nil
	}
	return v, nil
}

func pushMergeNullableBool(fields map[string]json.RawMessage, name string, allow func(string) bool, current *bool) (*bool, error) {
	if !allow(name) {
		return current, nil
	}
	v, present, err := pushDecodeNullableBool(fields, name)
	if err != nil {
		return nil, err
	}
	if !present {
		return current, nil
	}
	return v, nil
}

func stringFromNull(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func ptrFromNullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func ptrFromNullFloat64(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func ptrFromNullBool(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Int64 != 0
	return &b
}

func boolFromInt(v int64) bool { return v != 0 }

var pushItemFields = map[string]bool{"name": true, "description": true, "location_id": true, "quantity": true}

func pushItem(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if err := pushRequireKnownFields(m.Fields, pushItemFields); err != nil {
		return PushOutcome{}, err
	}

	if m.BaseVersion == 0 {
		name, _, err := pushDecodeString(m.Fields, "name")
		if err != nil {
			return PushOutcome{}, err
		}
		description, _, err := pushDecodeString(m.Fields, "description")
		if err != nil {
			return PushOutcome{}, err
		}
		locationID, _, err := pushDecodeString(m.Fields, "location_id")
		if err != nil {
			return PushOutcome{}, err
		}
		quantity, _, err := pushDecodeInt64(m.Fields, "quantity")
		if err != nil {
			return PushOutcome{}, err
		}

		var created Item
		var ok bool
		for attempt := 0; attempt < maxImportShortCodeAttempts; attempt++ {
			code, err := newImportShortCode()
			if err != nil {
				return PushOutcome{}, err
			}
			row, err := createItemTx(ctx, tx, q, group, CreateItemParams{
				ID: m.EntityID, Name: name, Description: description, LocationID: locationID,
				Quantity: quantity, ShortCode: code, Now: m.Now,
			})
			if errors.Is(err, ErrShortCodeTaken) {
				continue
			}
			if err != nil {
				return PushOutcome{}, err
			}
			created, ok = row, true
			break
		}
		if !ok {
			return PushOutcome{}, fmt.Errorf("storage: push: create item %q: exhausted %d short-code attempts", m.EntityID, maxImportShortCodeAttempts)
		}
		return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
	}

	current, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: m.EntityID})
	if errors.Is(err, sql.ErrNoRows) {
		return PushOutcome{ConflictFields: []string{entityConflictField}}, nil
	}
	if err != nil {
		return PushOutcome{}, fmt.Errorf("storage: push: get item %q: %w", m.EntityID, err)
	}

	allow := pushAllowAll
	conflict := []string(nil)
	if current.Version != m.BaseVersion {
		names := pushFieldNames(m.Fields)
		var apply []string
		conflict, apply, err = pushVersionMismatchPolicy(ctx, q, group, m.EntityType, m.EntityID, names)
		if err != nil {
			return PushOutcome{}, err
		}
		if len(apply) == 0 {
			return PushOutcome{ConflictFields: conflict}, nil
		}
		allow = pushAllowSet(apply)
	}

	name, err := pushMergeString(m.Fields, "name", allow, current.Name)
	if err != nil {
		return PushOutcome{}, err
	}
	description, err := pushMergeString(m.Fields, "description", allow, current.Description)
	if err != nil {
		return PushOutcome{}, err
	}
	locationID, err := pushMergeString(m.Fields, "location_id", allow, stringFromNull(current.LocationID))
	if err != nil {
		return PushOutcome{}, err
	}
	quantity, err := pushMergeInt64(m.Fields, "quantity", allow, current.Quantity)
	if err != nil {
		return PushOutcome{}, err
	}

	updated, err := updateItemTx(ctx, tx, q, group, UpdateItemParams{
		ItemID: m.EntityID, Name: name, Description: description, LocationID: locationID,
		Quantity: quantity, ExpectedVersion: current.Version, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, Version: updated.Version, ConflictFields: conflict}, nil
}

var pushWarrantyCreateFields = map[string]bool{
	"item_id": true, "holder": true, "provider": true, "starts_on": true, "expires_on": true,
	"is_lifetime": true, "notes": true,
}
var pushWarrantyUpdateFields = map[string]bool{
	"item_id": true, "holder": true, "provider": true, "starts_on": true, "expires_on": true,
	"is_lifetime": true, "notes": true,
}

func pushWarrantyBlock(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if m.BaseVersion == 0 {
		if err := pushRequireKnownFields(m.Fields, pushWarrantyCreateFields); err != nil {
			return PushOutcome{}, err
		}
		if err := pushRequireField(m.Fields, "item_id"); err != nil {
			return PushOutcome{}, err
		}
		itemID, _, err := pushDecodeString(m.Fields, "item_id")
		if err != nil {
			return PushOutcome{}, err
		}
		holder, _, err := pushDecodeString(m.Fields, "holder")
		if err != nil {
			return PushOutcome{}, err
		}
		provider, _, err := pushDecodeString(m.Fields, "provider")
		if err != nil {
			return PushOutcome{}, err
		}
		startsOn, _, err := pushDecodeString(m.Fields, "starts_on")
		if err != nil {
			return PushOutcome{}, err
		}
		expiresOn, _, err := pushDecodeString(m.Fields, "expires_on")
		if err != nil {
			return PushOutcome{}, err
		}
		isLifetime, _, err := pushDecodeBool(m.Fields, "is_lifetime")
		if err != nil {
			return PushOutcome{}, err
		}
		notes, _, err := pushDecodeString(m.Fields, "notes")
		if err != nil {
			return PushOutcome{}, err
		}

		created, err := createWarrantyTx(ctx, tx, q, group, CreateWarrantyParams{
			ID: m.EntityID, ItemID: itemID, Holder: holder, Provider: provider, StartsOn: startsOn,
			ExpiresOn: expiresOn, IsLifetime: isLifetime, Notes: notes, Now: m.Now,
		})
		if err != nil {
			return PushOutcome{}, err
		}
		return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
	}

	if err := pushRequireKnownFields(m.Fields, pushWarrantyUpdateFields); err != nil {
		return PushOutcome{}, err
	}
	if err := pushRequireField(m.Fields, "item_id"); err != nil {
		return PushOutcome{}, err
	}
	itemID, _, err := pushDecodeString(m.Fields, "item_id")
	if err != nil {
		return PushOutcome{}, err
	}

	current, err := q.GetItemWarranty(ctx, gen.GetItemWarrantyParams{GroupID: group, ItemID: itemID})
	if errors.Is(err, sql.ErrNoRows) {
		return PushOutcome{ConflictFields: []string{entityConflictField}}, nil
	}
	if err != nil {
		return PushOutcome{}, fmt.Errorf("storage: push: get warranty for item %q: %w", itemID, err)
	}

	allow := pushAllowAll
	conflict := []string(nil)
	if current.Version != m.BaseVersion {
		names := pushFieldNames(m.Fields)
		var apply []string
		conflict, apply, err = pushVersionMismatchPolicy(ctx, q, group, m.EntityType, m.EntityID, names)
		if err != nil {
			return PushOutcome{}, err
		}
		if len(apply) == 0 {
			return PushOutcome{EntityID: current.ID, ConflictFields: conflict}, nil
		}
		allow = pushAllowSet(apply)
	}

	holder, err := pushMergeString(m.Fields, "holder", allow, current.Holder)
	if err != nil {
		return PushOutcome{}, err
	}
	provider, err := pushMergeString(m.Fields, "provider", allow, current.Provider)
	if err != nil {
		return PushOutcome{}, err
	}
	startsOn, err := pushMergeString(m.Fields, "starts_on", allow, stringFromNull(current.StartsOn))
	if err != nil {
		return PushOutcome{}, err
	}
	expiresOn, err := pushMergeString(m.Fields, "expires_on", allow, stringFromNull(current.ExpiresOn))
	if err != nil {
		return PushOutcome{}, err
	}
	isLifetime, err := pushMergeBool(m.Fields, "is_lifetime", allow, boolFromInt(current.IsLifetime))
	if err != nil {
		return PushOutcome{}, err
	}
	notes, err := pushMergeString(m.Fields, "notes", allow, current.Notes)
	if err != nil {
		return PushOutcome{}, err
	}

	updated, err := updateWarrantyTx(ctx, tx, q, group, UpdateWarrantyParams{
		ItemID: itemID, Holder: holder, Provider: provider, StartsOn: startsOn, ExpiresOn: expiresOn,
		IsLifetime: isLifetime, Notes: notes, ExpectedVersion: current.Version, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, EntityID: updated.ID, Version: updated.Version, ConflictFields: conflict}, nil
}

var pushSaleCreateFields = map[string]bool{
	"item_id": true, "buyer_name": true, "sold_on": true, "sale_price_minor": true, "notes": true,
}
var pushSaleUpdateFields = pushSaleCreateFields

func pushSoldToBlock(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if m.BaseVersion == 0 {
		if err := pushRequireKnownFields(m.Fields, pushSaleCreateFields); err != nil {
			return PushOutcome{}, err
		}
		if err := pushRequireField(m.Fields, "item_id"); err != nil {
			return PushOutcome{}, err
		}
		itemID, _, err := pushDecodeString(m.Fields, "item_id")
		if err != nil {
			return PushOutcome{}, err
		}
		buyerName, _, err := pushDecodeString(m.Fields, "buyer_name")
		if err != nil {
			return PushOutcome{}, err
		}
		soldOn, _, err := pushDecodeString(m.Fields, "sold_on")
		if err != nil {
			return PushOutcome{}, err
		}
		salePriceMinor, _, err := pushDecodeInt64(m.Fields, "sale_price_minor")
		if err != nil {
			return PushOutcome{}, err
		}
		notes, _, err := pushDecodeString(m.Fields, "notes")
		if err != nil {
			return PushOutcome{}, err
		}

		created, err := createSaleTx(ctx, tx, q, group, CreateSaleParams{
			ID: m.EntityID, ItemID: itemID, BuyerName: buyerName, SoldOn: soldOn,
			SalePriceMinor: salePriceMinor, Notes: notes, Now: m.Now,
		})
		if err != nil {
			return PushOutcome{}, err
		}
		return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
	}

	if err := pushRequireKnownFields(m.Fields, pushSaleUpdateFields); err != nil {
		return PushOutcome{}, err
	}
	if err := pushRequireField(m.Fields, "item_id"); err != nil {
		return PushOutcome{}, err
	}
	itemID, _, err := pushDecodeString(m.Fields, "item_id")
	if err != nil {
		return PushOutcome{}, err
	}

	current, err := q.GetItemSale(ctx, gen.GetItemSaleParams{GroupID: group, ItemID: itemID})
	if errors.Is(err, sql.ErrNoRows) {
		return PushOutcome{ConflictFields: []string{entityConflictField}}, nil
	}
	if err != nil {
		return PushOutcome{}, fmt.Errorf("storage: push: get sale for item %q: %w", itemID, err)
	}

	allow := pushAllowAll
	conflict := []string(nil)
	if current.Version != m.BaseVersion {
		names := pushFieldNames(m.Fields)
		var apply []string
		conflict, apply, err = pushVersionMismatchPolicy(ctx, q, group, m.EntityType, m.EntityID, names)
		if err != nil {
			return PushOutcome{}, err
		}
		if len(apply) == 0 {
			return PushOutcome{EntityID: current.ID, ConflictFields: conflict}, nil
		}
		allow = pushAllowSet(apply)
	}

	buyerName, err := pushMergeString(m.Fields, "buyer_name", allow, current.BuyerName)
	if err != nil {
		return PushOutcome{}, err
	}
	soldOn, err := pushMergeString(m.Fields, "sold_on", allow, stringFromNull(current.SoldOn))
	if err != nil {
		return PushOutcome{}, err
	}
	salePriceMinor, err := pushMergeInt64(m.Fields, "sale_price_minor", allow, current.SalePriceMinor)
	if err != nil {
		return PushOutcome{}, err
	}
	notes, err := pushMergeString(m.Fields, "notes", allow, current.Notes)
	if err != nil {
		return PushOutcome{}, err
	}

	updated, err := updateSaleTx(ctx, tx, q, group, UpdateSaleParams{
		ItemID: itemID, BuyerName: buyerName, SoldOn: soldOn, SalePriceMinor: salePriceMinor,
		Notes: notes, ExpectedVersion: current.Version, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, EntityID: updated.ID, Version: updated.Version, ConflictFields: conflict}, nil
}

var pushPurchaseCreateFields = map[string]bool{
	"item_id": true, "vendor": true, "purchased_on": true, "purchase_price_minor": true,
	"order_reference": true, "notes": true,
}
var pushPurchaseUpdateFields = pushPurchaseCreateFields

func pushPurchasedFromBlock(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if m.BaseVersion == 0 {
		if err := pushRequireKnownFields(m.Fields, pushPurchaseCreateFields); err != nil {
			return PushOutcome{}, err
		}
		if err := pushRequireField(m.Fields, "item_id"); err != nil {
			return PushOutcome{}, err
		}
		itemID, _, err := pushDecodeString(m.Fields, "item_id")
		if err != nil {
			return PushOutcome{}, err
		}
		vendor, _, err := pushDecodeString(m.Fields, "vendor")
		if err != nil {
			return PushOutcome{}, err
		}
		purchasedOn, _, err := pushDecodeString(m.Fields, "purchased_on")
		if err != nil {
			return PushOutcome{}, err
		}
		purchasePriceMinor, _, err := pushDecodeInt64(m.Fields, "purchase_price_minor")
		if err != nil {
			return PushOutcome{}, err
		}
		orderReference, _, err := pushDecodeString(m.Fields, "order_reference")
		if err != nil {
			return PushOutcome{}, err
		}
		notes, _, err := pushDecodeString(m.Fields, "notes")
		if err != nil {
			return PushOutcome{}, err
		}

		created, err := createPurchaseTx(ctx, tx, q, group, CreatePurchaseParams{
			ID: m.EntityID, ItemID: itemID, Vendor: vendor, PurchasedOn: purchasedOn,
			PurchasePriceMinor: purchasePriceMinor, OrderReference: orderReference, Notes: notes, Now: m.Now,
		})
		if err != nil {
			return PushOutcome{}, err
		}
		return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
	}

	if err := pushRequireKnownFields(m.Fields, pushPurchaseUpdateFields); err != nil {
		return PushOutcome{}, err
	}
	if err := pushRequireField(m.Fields, "item_id"); err != nil {
		return PushOutcome{}, err
	}
	itemID, _, err := pushDecodeString(m.Fields, "item_id")
	if err != nil {
		return PushOutcome{}, err
	}

	current, err := q.GetItemPurchase(ctx, gen.GetItemPurchaseParams{GroupID: group, ItemID: itemID})
	if errors.Is(err, sql.ErrNoRows) {
		return PushOutcome{ConflictFields: []string{entityConflictField}}, nil
	}
	if err != nil {
		return PushOutcome{}, fmt.Errorf("storage: push: get purchase for item %q: %w", itemID, err)
	}

	allow := pushAllowAll
	conflict := []string(nil)
	if current.Version != m.BaseVersion {
		names := pushFieldNames(m.Fields)
		var apply []string
		conflict, apply, err = pushVersionMismatchPolicy(ctx, q, group, m.EntityType, m.EntityID, names)
		if err != nil {
			return PushOutcome{}, err
		}
		if len(apply) == 0 {
			return PushOutcome{EntityID: current.ID, ConflictFields: conflict}, nil
		}
		allow = pushAllowSet(apply)
	}

	vendor, err := pushMergeString(m.Fields, "vendor", allow, current.Vendor)
	if err != nil {
		return PushOutcome{}, err
	}
	purchasedOn, err := pushMergeString(m.Fields, "purchased_on", allow, stringFromNull(current.PurchasedOn))
	if err != nil {
		return PushOutcome{}, err
	}
	purchasePriceMinor, err := pushMergeInt64(m.Fields, "purchase_price_minor", allow, current.PurchasePriceMinor)
	if err != nil {
		return PushOutcome{}, err
	}
	orderReference, err := pushMergeString(m.Fields, "order_reference", allow, current.OrderReference)
	if err != nil {
		return PushOutcome{}, err
	}
	notes, err := pushMergeString(m.Fields, "notes", allow, current.Notes)
	if err != nil {
		return PushOutcome{}, err
	}

	updated, err := updatePurchaseTx(ctx, tx, q, group, UpdatePurchaseParams{
		ItemID: itemID, Vendor: vendor, PurchasedOn: purchasedOn, PurchasePriceMinor: purchasePriceMinor,
		OrderReference: orderReference, Notes: notes, ExpectedVersion: current.Version, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, EntityID: updated.ID, Version: updated.Version, ConflictFields: conflict}, nil
}

var pushIdentificationCreateFields = map[string]bool{"item_id": true, "kind": true, "value": true}
var pushIdentificationUpdateFields = map[string]bool{"item_id": true, "kind": true, "value": true}

func pushItemIdentification(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if m.BaseVersion == 0 {
		if err := pushRequireKnownFields(m.Fields, pushIdentificationCreateFields); err != nil {
			return PushOutcome{}, err
		}
		if err := pushRequireField(m.Fields, "item_id"); err != nil {
			return PushOutcome{}, err
		}
		itemID, _, err := pushDecodeString(m.Fields, "item_id")
		if err != nil {
			return PushOutcome{}, err
		}
		kind, _, err := pushDecodeString(m.Fields, "kind")
		if err != nil {
			return PushOutcome{}, err
		}
		value, _, err := pushDecodeString(m.Fields, "value")
		if err != nil {
			return PushOutcome{}, err
		}

		created, err := createIdentificationTx(ctx, tx, q, group, CreateIdentificationParams{
			ID: m.EntityID, ItemID: itemID, Kind: kind, Value: value, Now: m.Now,
		})
		if err != nil {
			return PushOutcome{}, err
		}
		return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
	}

	if err := pushRequireKnownFields(m.Fields, pushIdentificationUpdateFields); err != nil {
		return PushOutcome{}, err
	}
	if err := pushRequireField(m.Fields, "item_id"); err != nil {
		return PushOutcome{}, err
	}
	itemID, _, err := pushDecodeString(m.Fields, "item_id")
	if err != nil {
		return PushOutcome{}, err
	}

	current, err := q.GetItemIdentification(ctx, gen.GetItemIdentificationParams{GroupID: group, ItemID: itemID, ID: m.EntityID})
	if errors.Is(err, sql.ErrNoRows) {
		return PushOutcome{ConflictFields: []string{entityConflictField}}, nil
	}
	if err != nil {
		return PushOutcome{}, fmt.Errorf("storage: push: get identification %q on item %q: %w", m.EntityID, itemID, err)
	}

	allow := pushAllowAll
	conflict := []string(nil)
	if current.Version != m.BaseVersion {
		names := pushFieldNames(m.Fields)
		var apply []string
		conflict, apply, err = pushVersionMismatchPolicy(ctx, q, group, m.EntityType, m.EntityID, names)
		if err != nil {
			return PushOutcome{}, err
		}
		if len(apply) == 0 {
			return PushOutcome{ConflictFields: conflict}, nil
		}
		allow = pushAllowSet(apply)
	}

	kind, err := pushMergeString(m.Fields, "kind", allow, current.Kind)
	if err != nil {
		return PushOutcome{}, err
	}
	value, err := pushMergeString(m.Fields, "value", allow, current.Value)
	if err != nil {
		return PushOutcome{}, err
	}

	updated, err := updateIdentificationTx(ctx, tx, q, group, UpdateIdentificationParams{
		ItemID: itemID, ID: m.EntityID, Kind: kind, Value: value, ExpectedVersion: current.Version, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, Version: updated.Version, ConflictFields: conflict}, nil
}

var pushCustomFieldCreateFields = map[string]bool{
	"item_id": true, "field_def_id": true, "name": true, "field_type": true, "text_value": true,
	"number_value": true, "bool_value": true, "date_value": true,
}
var pushCustomFieldUpdateFields = pushCustomFieldCreateFields

func pushItemCustomField(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if m.BaseVersion == 0 {
		if err := pushRequireKnownFields(m.Fields, pushCustomFieldCreateFields); err != nil {
			return PushOutcome{}, err
		}
		if err := pushRequireField(m.Fields, "item_id"); err != nil {
			return PushOutcome{}, err
		}
		itemID, _, err := pushDecodeString(m.Fields, "item_id")
		if err != nil {
			return PushOutcome{}, err
		}
		fieldDefID, _, err := pushDecodeString(m.Fields, "field_def_id")
		if err != nil {
			return PushOutcome{}, err
		}
		name, _, err := pushDecodeString(m.Fields, "name")
		if err != nil {
			return PushOutcome{}, err
		}
		fieldType, _, err := pushDecodeString(m.Fields, "field_type")
		if err != nil {
			return PushOutcome{}, err
		}
		textValue, _, err := pushDecodeNullableString(m.Fields, "text_value")
		if err != nil {
			return PushOutcome{}, err
		}
		numberValue, _, err := pushDecodeNullableFloat64(m.Fields, "number_value")
		if err != nil {
			return PushOutcome{}, err
		}
		boolValue, _, err := pushDecodeNullableBool(m.Fields, "bool_value")
		if err != nil {
			return PushOutcome{}, err
		}
		dateValue, _, err := pushDecodeNullableString(m.Fields, "date_value")
		if err != nil {
			return PushOutcome{}, err
		}

		created, err := createItemCustomFieldTx(ctx, tx, q, group, CreateItemCustomFieldParams{
			ID: m.EntityID, ItemID: itemID, FieldDefID: fieldDefID, Name: name, FieldType: fieldType,
			TextValue: textValue, NumberValue: numberValue, BoolValue: boolValue, DateValue: dateValue, Now: m.Now,
		})
		if err != nil {
			return PushOutcome{}, err
		}
		return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
	}

	if err := pushRequireKnownFields(m.Fields, pushCustomFieldUpdateFields); err != nil {
		return PushOutcome{}, err
	}
	if err := pushRequireField(m.Fields, "item_id"); err != nil {
		return PushOutcome{}, err
	}
	itemID, _, err := pushDecodeString(m.Fields, "item_id")
	if err != nil {
		return PushOutcome{}, err
	}

	current, err := q.GetItemCustomField(ctx, gen.GetItemCustomFieldParams{GroupID: group, ItemID: itemID, ID: m.EntityID})
	if errors.Is(err, sql.ErrNoRows) {
		return PushOutcome{ConflictFields: []string{entityConflictField}}, nil
	}
	if err != nil {
		return PushOutcome{}, fmt.Errorf("storage: push: get custom field %q on item %q: %w", m.EntityID, itemID, err)
	}

	allow := pushAllowAll
	conflict := []string(nil)
	if current.Version != m.BaseVersion {
		names := pushFieldNames(m.Fields)
		var apply []string
		conflict, apply, err = pushVersionMismatchPolicy(ctx, q, group, m.EntityType, m.EntityID, names)
		if err != nil {
			return PushOutcome{}, err
		}
		if len(apply) == 0 {
			return PushOutcome{ConflictFields: conflict}, nil
		}
		allow = pushAllowSet(apply)
	}

	fieldDefID, err := pushMergeString(m.Fields, "field_def_id", allow, stringFromNull(current.FieldDefID))
	if err != nil {
		return PushOutcome{}, err
	}
	name, err := pushMergeString(m.Fields, "name", allow, current.Name)
	if err != nil {
		return PushOutcome{}, err
	}
	fieldType, err := pushMergeString(m.Fields, "field_type", allow, current.FieldType)
	if err != nil {
		return PushOutcome{}, err
	}
	textValue, err := pushMergeNullableString(m.Fields, "text_value", allow, ptrFromNullString(current.TextValue))
	if err != nil {
		return PushOutcome{}, err
	}
	numberValue, err := pushMergeNullableFloat64(m.Fields, "number_value", allow, ptrFromNullFloat64(current.NumberValue))
	if err != nil {
		return PushOutcome{}, err
	}
	boolValue, err := pushMergeNullableBool(m.Fields, "bool_value", allow, ptrFromNullBool(current.BoolValue))
	if err != nil {
		return PushOutcome{}, err
	}
	dateValue, err := pushMergeNullableString(m.Fields, "date_value", allow, ptrFromNullString(current.DateValue))
	if err != nil {
		return PushOutcome{}, err
	}

	updated, err := updateItemCustomFieldTx(ctx, tx, q, group, UpdateItemCustomFieldParams{
		ItemID: itemID, ID: m.EntityID, FieldDefID: fieldDefID, Name: name, FieldType: fieldType,
		TextValue: textValue, NumberValue: numberValue, BoolValue: boolValue, DateValue: dateValue,
		ExpectedVersion: current.Version, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, Version: updated.Version, ConflictFields: conflict}, nil
}

var pushLocationFields = map[string]bool{"name": true, "parent_id": true}

func pushLocation(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if err := pushRequireKnownFields(m.Fields, pushLocationFields); err != nil {
		return PushOutcome{}, err
	}

	if m.BaseVersion == 0 {
		name, _, err := pushDecodeString(m.Fields, "name")
		if err != nil {
			return PushOutcome{}, err
		}
		parentID, _, err := pushDecodeString(m.Fields, "parent_id")
		if err != nil {
			return PushOutcome{}, err
		}
		created, err := createLocationTx(ctx, tx, q, group, CreateLocationParams{
			ID: m.EntityID, Name: name, ParentID: parentID, Now: m.Now,
		})
		if err != nil {
			return PushOutcome{}, err
		}
		return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
	}

	current, err := q.GetLocation(ctx, gen.GetLocationParams{GroupID: group, LocationID: m.EntityID})
	if errors.Is(err, sql.ErrNoRows) {
		return PushOutcome{ConflictFields: []string{entityConflictField}}, nil
	}
	if err != nil {
		return PushOutcome{}, fmt.Errorf("storage: push: get location %q: %w", m.EntityID, err)
	}

	allow := pushAllowAll
	conflict := []string(nil)
	if current.Version != m.BaseVersion {
		names := pushFieldNames(m.Fields)
		var apply []string
		conflict, apply, err = pushVersionMismatchPolicy(ctx, q, group, m.EntityType, m.EntityID, names)
		if err != nil {
			return PushOutcome{}, err
		}
		if len(apply) == 0 {
			return PushOutcome{ConflictFields: conflict}, nil
		}
		allow = pushAllowSet(apply)
	}

	name, err := pushMergeString(m.Fields, "name", allow, current.Name)
	if err != nil {
		return PushOutcome{}, err
	}
	parentID, err := pushMergeString(m.Fields, "parent_id", allow, stringFromNull(current.ParentID))
	if err != nil {
		return PushOutcome{}, err
	}

	updated, err := updateLocationTx(ctx, tx, q, group, UpdateLocationParams{
		LocationID: m.EntityID, Name: name, ParentID: parentID, ExpectedVersion: current.Version, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, Version: updated.Version, ConflictFields: conflict}, nil
}

var pushLabelFields = map[string]bool{"name": true, "color": true}

func pushLabel(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if err := pushRequireKnownFields(m.Fields, pushLabelFields); err != nil {
		return PushOutcome{}, err
	}

	if m.BaseVersion == 0 {
		name, _, err := pushDecodeString(m.Fields, "name")
		if err != nil {
			return PushOutcome{}, err
		}
		color, _, err := pushDecodeString(m.Fields, "color")
		if err != nil {
			return PushOutcome{}, err
		}
		created, err := createLabelTx(ctx, tx, q, group, CreateLabelParams{
			ID: m.EntityID, Name: name, Color: color, Now: m.Now,
		})
		if err != nil {
			return PushOutcome{}, err
		}
		return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
	}

	current, err := q.GetLabel(ctx, gen.GetLabelParams{GroupID: group, ID: m.EntityID})
	if errors.Is(err, sql.ErrNoRows) {
		return PushOutcome{ConflictFields: []string{entityConflictField}}, nil
	}
	if err != nil {
		return PushOutcome{}, fmt.Errorf("storage: push: get label %q: %w", m.EntityID, err)
	}

	allow := pushAllowAll
	conflict := []string(nil)
	if current.Version != m.BaseVersion {
		names := pushFieldNames(m.Fields)
		var apply []string
		conflict, apply, err = pushVersionMismatchPolicy(ctx, q, group, m.EntityType, m.EntityID, names)
		if err != nil {
			return PushOutcome{}, err
		}
		if len(apply) == 0 {
			return PushOutcome{ConflictFields: conflict}, nil
		}
		allow = pushAllowSet(apply)
	}

	name, err := pushMergeString(m.Fields, "name", allow, current.Name)
	if err != nil {
		return PushOutcome{}, err
	}
	color, err := pushMergeString(m.Fields, "color", allow, current.Color)
	if err != nil {
		return PushOutcome{}, err
	}

	updated, err := updateLabelTx(ctx, tx, q, group, UpdateLabelParams{
		ID: m.EntityID, Name: name, Color: color, ExpectedVersion: current.Version, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, Version: updated.Version, ConflictFields: conflict}, nil
}

var pushItemLabelFields = map[string]bool{"item_id": true, "label_id": true}

func pushItemLabel(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if m.BaseVersion != 0 {
		return PushOutcome{}, fmt.Errorf("%w: item_label", ErrPushCreationOnly)
	}
	if err := pushRequireKnownFields(m.Fields, pushItemLabelFields); err != nil {
		return PushOutcome{}, err
	}
	if err := pushRequireField(m.Fields, "item_id"); err != nil {
		return PushOutcome{}, err
	}
	if err := pushRequireField(m.Fields, "label_id"); err != nil {
		return PushOutcome{}, err
	}
	itemID, _, err := pushDecodeString(m.Fields, "item_id")
	if err != nil {
		return PushOutcome{}, err
	}
	labelID, _, err := pushDecodeString(m.Fields, "label_id")
	if err != nil {
		return PushOutcome{}, err
	}

	row, err := attachItemLabelTx(ctx, tx, q, group, AttachLabelParams{
		ID: m.EntityID, ItemID: itemID, LabelID: labelID, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, EntityID: row.ID, Version: row.Version}, nil
}

var pushStockAdjustmentFields = map[string]bool{"item_id": true, "delta": true, "reason": true, "note": true}

func pushStockAdjustment(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, m PushMutation) (PushOutcome, error) {
	if m.BaseVersion != 0 {
		return PushOutcome{}, fmt.Errorf("%w: stock_adjustment", ErrPushCreationOnly)
	}
	if err := pushRequireKnownFields(m.Fields, pushStockAdjustmentFields); err != nil {
		return PushOutcome{}, err
	}
	if err := pushRequireField(m.Fields, "item_id"); err != nil {
		return PushOutcome{}, err
	}
	itemID, _, err := pushDecodeString(m.Fields, "item_id")
	if err != nil {
		return PushOutcome{}, err
	}
	delta, _, err := pushDecodeInt64(m.Fields, "delta")
	if err != nil {
		return PushOutcome{}, err
	}
	reason, _, err := pushDecodeString(m.Fields, "reason")
	if err != nil {
		return PushOutcome{}, err
	}
	note, _, err := pushDecodeString(m.Fields, "note")
	if err != nil {
		return PushOutcome{}, err
	}

	created, err := createStockAdjustmentTx(ctx, tx, q, group, CreateStockAdjustmentParams{
		ID: m.EntityID, ItemID: itemID, Delta: delta, Reason: reason, Note: note, Now: m.Now,
	})
	if err != nil {
		return PushOutcome{}, err
	}
	return PushOutcome{Applied: true, EntityID: created.ID, Version: created.Version}, nil
}

func pushAttachment(_ context.Context, _ *sql.Tx, _ *gen.Queries, _ string, m PushMutation) (PushOutcome, error) {
	return PushOutcome{}, fmt.Errorf("%w: entity_id %q", ErrPushAttachmentRejected, m.EntityID)
}
