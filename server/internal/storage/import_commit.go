package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"github.com/google/uuid"
)

var ErrImportSessionNotStaged = errors.New("storage: import session is not staged; it was committed, expired, or removed since it was last read")

type ImportCommitRowAction string

const (
	ImportCommitRowCreate    ImportCommitRowAction = "create"
	ImportCommitRowUpdate    ImportCommitRowAction = "update"
	ImportCommitRowUnchanged ImportCommitRowAction = "unchanged"
)

type ImportCommitIdentification struct {
	Kind  string
	Value string
}

type ImportCommitWarranty struct {
	Holder     string
	Provider   string
	StartsOn   string
	ExpiresOn  string
	IsLifetime bool
	Notes      string
}

type ImportCommitPurchase struct {
	Vendor             string
	PurchasedOn        string
	PurchasePriceMinor int64
	OrderReference     string
	Notes              string
}

type ImportCommitSale struct {
	BuyerName      string
	SoldOn         string
	SalePriceMinor int64
	Notes          string
}

type ImportCommitProposedRow struct {
	Name            string
	Description     string
	Quantity        int64
	LocationID      string
	Labels          []string
	Identifications []ImportCommitIdentification
	Warranty        *ImportCommitWarranty
	Purchase        *ImportCommitPurchase
	Sale            *ImportCommitSale
	CustomFields    []ItemCustomField
}

type ImportCommitRow struct {
	Line     int
	Action   ImportCommitRowAction
	ItemID   string
	Changes  []string
	Proposed ImportCommitProposedRow
}

type ImportCommitCreatedItem struct {
	Line   int
	ItemID string
}

type ImportCommitParams struct {
	ImportSessionID string
	Rows            []ImportCommitRow
	Now             int64
}

func (p ImportCommitParams) validate() error {
	switch {
	case p.ImportSessionID == "":
		return errors.New("storage: ImportCommitParams: ImportSessionID is empty")
	case p.Now <= 0:
		return errors.New("storage: ImportCommitParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type ImportCommitResult struct {
	Created      int
	Updated      int
	Unchanged    int
	CreatedItems []ImportCommitCreatedItem
}

type ImportCommitRepository interface {
	Commit(ctx context.Context, p ImportCommitParams) (ImportCommitResult, error)
}

func (s *Storage) ForGroupImportCommit(g GroupID) (ImportCommitRepository, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if g.IsZero() {
		return nil, fmt.Errorf("%w: group id is the zero value", ErrNoGroup)
	}
	return importCommitRepository{group: g.id, store: s.store}, nil
}

type importCommitRepository struct {
	group string
	store *db.Store
}

func (r importCommitRepository) Commit(ctx context.Context, p ImportCommitParams) (ImportCommitResult, error) {
	if err := p.validate(); err != nil {
		return ImportCommitResult{}, err
	}

	var result ImportCommitResult
	err := r.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		rows, err := q.MarkImportSessionCommitted(ctx, gen.MarkImportSessionCommittedParams{
			GroupID: r.group,
			ID:      p.ImportSessionID,
		})
		if err != nil {
			return fmt.Errorf("mark import session %q committed: %w", p.ImportSessionID, err)
		}
		if rows != 1 {
			return ErrImportSessionNotStaged
		}

		for _, row := range p.Rows {
			switch row.Action {
			case ImportCommitRowCreate:
				itemID, err := r.commitCreateRow(ctx, tx, q, row, p.Now)
				if err != nil {
					return fmt.Errorf("line %d: create: %w", row.Line, err)
				}
				result.Created++
				result.CreatedItems = append(result.CreatedItems, ImportCommitCreatedItem{Line: row.Line, ItemID: itemID})

			case ImportCommitRowUpdate:
				if err := r.commitUpdateRow(ctx, tx, q, row, p.Now); err != nil {
					return fmt.Errorf("line %d: update item %q: %w", row.Line, row.ItemID, err)
				}
				result.Updated++

			case ImportCommitRowUnchanged:
				result.Unchanged++

			default:
				return fmt.Errorf("line %d: unrecognised import commit row action %q", row.Line, row.Action)
			}
		}
		return nil
	})
	if err != nil {
		return ImportCommitResult{}, err
	}
	return result, nil
}

func (r importCommitRepository) commitCreateRow(ctx context.Context, tx *sql.Tx, q *gen.Queries, row ImportCommitRow, now int64) (string, error) {
	seq, err := db.AllocChangeSeq(ctx, tx, r.group)
	if err != nil {
		return "", fmt.Errorf("allocate change_seq: %w", err)
	}

	id, err := newImportRowID()
	if err != nil {
		return "", err
	}

	var created bool
	for attempt := 0; attempt < maxImportShortCodeAttempts; attempt++ {
		code, err := newImportShortCode()
		if err != nil {
			return "", err
		}
		if err := q.CreateItem(ctx, gen.CreateItemParams{
			ID:          id,
			GroupID:     r.group,
			Name:        row.Proposed.Name,
			Description: row.Proposed.Description,
			LocationID:  nullString(row.Proposed.LocationID),
			Quantity:    row.Proposed.Quantity,
			ShortCode:   code,
			Now:         now,
			ChangeSeq:   seq,
		}); err != nil {
			if isUniqueConstraintViolation(err) {
				continue
			}
			return "", fmt.Errorf("create item: %w", err)
		}
		created = true
		break
	}
	if !created {
		return "", fmt.Errorf("exhausted %d short-code attempts against a %d-symbol, %d-character alphabet",
			maxImportShortCodeAttempts, len(importShortCodeAlphabet), importShortCodeLength)
	}

	if err := r.replaceLabels(ctx, tx, q, id, row.Proposed.Labels, now, seq); err != nil {
		return "", fmt.Errorf("labels: %w", err)
	}
	if err := replaceIdentifications(ctx, q, r.group, id, row.Proposed.Identifications, now, seq); err != nil {
		return "", fmt.Errorf("identifications: %w", err)
	}
	if err := replaceCustomFields(ctx, q, r.group, id, row.Proposed.CustomFields, now, seq); err != nil {
		return "", fmt.Errorf("custom fields: %w", err)
	}
	if err := upsertWarranty(ctx, q, r.group, id, nil, row.Proposed.Warranty, now, seq); err != nil {
		return "", fmt.Errorf("warranty: %w", err)
	}
	if err := upsertPurchase(ctx, q, r.group, id, nil, row.Proposed.Purchase, now, seq); err != nil {
		return "", fmt.Errorf("purchase: %w", err)
	}
	if err := upsertSale(ctx, q, r.group, id, nil, row.Proposed.Sale, now, seq); err != nil {
		return "", fmt.Errorf("sale: %w", err)
	}

	return id, nil
}

func (r importCommitRepository) commitUpdateRow(ctx context.Context, tx *sql.Tx, q *gen.Queries, row ImportCommitRow, now int64) error {
	current, err := q.GetItem(ctx, gen.GetItemParams{GroupID: r.group, ItemID: row.ItemID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("item %q: %w", row.ItemID, ErrNotFound)
		}
		return fmt.Errorf("get item %q: %w", row.ItemID, err)
	}

	changed := make(map[string]bool, len(row.Changes))
	for _, c := range row.Changes {
		changed[c] = true
	}
	if len(changed) == 0 {
		return nil
	}

	seq, err := db.AllocChangeSeq(ctx, tx, r.group)
	if err != nil {
		return fmt.Errorf("allocate change_seq: %w", err)
	}

	if changed["name"] || changed["description"] || changed["quantity"] || changed["location_id"] {
		rows, err := q.UpdateItem(ctx, gen.UpdateItemParams{
			Name:            row.Proposed.Name,
			Description:     row.Proposed.Description,
			LocationID:      nullString(row.Proposed.LocationID),
			Quantity:        row.Proposed.Quantity,
			Now:             now,
			ChangeSeq:       seq,
			GroupID:         r.group,
			ItemID:          row.ItemID,
			ExpectedVersion: current.Version,
		})
		if err != nil {
			return fmt.Errorf("update item core fields: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("update item core fields: matched %d rows, want 1 (read version %d); concurrent modification during import commit", rows, current.Version)
		}
	}

	if changed["labels"] {
		if err := r.replaceLabels(ctx, tx, q, row.ItemID, row.Proposed.Labels, now, seq); err != nil {
			return fmt.Errorf("labels: %w", err)
		}
	}
	if changed["identifications"] {
		if err := replaceIdentifications(ctx, q, r.group, row.ItemID, row.Proposed.Identifications, now, seq); err != nil {
			return fmt.Errorf("identifications: %w", err)
		}
	}
	if changed["custom_fields"] {
		if err := replaceCustomFields(ctx, q, r.group, row.ItemID, row.Proposed.CustomFields, now, seq); err != nil {
			return fmt.Errorf("custom fields: %w", err)
		}
	}
	if changed["warranty"] {
		cur, err := getOptionalWarranty(ctx, q, r.group, row.ItemID)
		if err != nil {
			return fmt.Errorf("read current warranty: %w", err)
		}
		if err := upsertWarranty(ctx, q, r.group, row.ItemID, cur, row.Proposed.Warranty, now, seq); err != nil {
			return fmt.Errorf("warranty: %w", err)
		}
	}
	if changed["purchase"] {
		cur, err := getOptionalPurchase(ctx, q, r.group, row.ItemID)
		if err != nil {
			return fmt.Errorf("read current purchase: %w", err)
		}
		if err := upsertPurchase(ctx, q, r.group, row.ItemID, cur, row.Proposed.Purchase, now, seq); err != nil {
			return fmt.Errorf("purchase: %w", err)
		}
	}
	if changed["sale"] {
		cur, err := getOptionalSale(ctx, q, r.group, row.ItemID)
		if err != nil {
			return fmt.Errorf("read current sale: %w", err)
		}
		if err := upsertSale(ctx, q, r.group, row.ItemID, cur, row.Proposed.Sale, now, seq); err != nil {
			return fmt.Errorf("sale: %w", err)
		}
	}

	return nil
}

func (r importCommitRepository) replaceLabels(ctx context.Context, tx *sql.Tx, q *gen.Queries, itemID string, names []string, now, seq int64) error {
	labelIDs, err := r.resolveLabelIDs(ctx, tx, q, names, now)
	if err != nil {
		return fmt.Errorf("resolve label names: %w", err)
	}

	if _, err := q.CascadeDeleteItemLabels(ctx, gen.CascadeDeleteItemLabelsParams{
		DeletedAt: sql.NullInt64{Int64: now, Valid: true}, Now: now, ChangeSeq: seq, GroupID: r.group, ItemID: itemID,
	}); err != nil {
		return fmt.Errorf("cascade-tombstone existing label edges: %w", err)
	}
	for _, labelID := range labelIDs {
		edgeID, err := newImportRowID()
		if err != nil {
			return err
		}
		if err := q.CreateItemLabel(ctx, gen.CreateItemLabelParams{
			ID: edgeID, GroupID: r.group, ItemID: itemID, LabelID: labelID, Now: now, ChangeSeq: seq,
		}); err != nil {
			return fmt.Errorf("attach label %q: %w", labelID, err)
		}
	}
	return nil
}

const importDefaultLabelColor = "#888888"

func (r importCommitRepository) resolveLabelIDs(ctx context.Context, tx *sql.Tx, q *gen.Queries, names []string, now int64) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		label, err := q.GetLabelByName(ctx, gen.GetLabelByNameParams{GroupID: r.group, Name: name})
		switch {
		case err == nil:
			ids = append(ids, label.ID)
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return nil, fmt.Errorf("check label %q: %w", name, err)
		}

		labelSeq, err := db.AllocChangeSeq(ctx, tx, r.group)
		if err != nil {
			return nil, fmt.Errorf("allocate change_seq for new label %q: %w", name, err)
		}
		id, err := newImportRowID()
		if err != nil {
			return nil, err
		}
		if err := q.CreateLabel(ctx, gen.CreateLabelParams{
			ID: id, GroupID: r.group, Name: name, Color: importDefaultLabelColor, Now: now, ChangeSeq: labelSeq,
		}); err != nil {
			return nil, fmt.Errorf("create label %q: %w", name, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func replaceIdentifications(ctx context.Context, q *gen.Queries, group, itemID string, proposed []ImportCommitIdentification, now, seq int64) error {
	if _, err := q.CascadeDeleteItemIdentifications(ctx, gen.CascadeDeleteItemIdentificationsParams{
		DeletedAt: sql.NullInt64{Int64: now, Valid: true}, Now: now, ChangeSeq: seq, GroupID: group, ItemID: itemID,
	}); err != nil {
		return fmt.Errorf("cascade-tombstone existing identifications: %w", err)
	}
	for _, idn := range proposed {
		id, err := newImportRowID()
		if err != nil {
			return err
		}
		if err := q.CreateItemIdentification(ctx, gen.CreateItemIdentificationParams{
			ID: id, GroupID: group, ItemID: itemID, Kind: idn.Kind, Value: idn.Value, Now: now, ChangeSeq: seq,
		}); err != nil {
			return fmt.Errorf("create identification: %w", err)
		}
	}
	return nil
}

func replaceCustomFields(ctx context.Context, q *gen.Queries, group, itemID string, proposed []ItemCustomField, now, seq int64) error {
	if _, err := q.CascadeDeleteItemCustomFields(ctx, gen.CascadeDeleteItemCustomFieldsParams{
		DeletedAt: sql.NullInt64{Int64: now, Valid: true}, Now: now, ChangeSeq: seq, GroupID: group, ItemID: itemID,
	}); err != nil {
		return fmt.Errorf("cascade-tombstone existing custom fields: %w", err)
	}
	for _, v := range proposed {
		id, err := newImportRowID()
		if err != nil {
			return err
		}
		if err := q.CreateItemCustomField(ctx, gen.CreateItemCustomFieldParams{
			ID:          id,
			GroupID:     group,
			ItemID:      itemID,
			FieldDefID:  v.FieldDefID,
			Name:        v.Name,
			FieldType:   v.FieldType,
			TextValue:   v.TextValue,
			NumberValue: v.NumberValue,
			BoolValue:   v.BoolValue,
			DateValue:   v.DateValue,
			Now:         now,
			ChangeSeq:   seq,
		}); err != nil {
			return fmt.Errorf("create custom field %q: %w", v.Name, err)
		}
	}
	return nil
}

func getOptionalWarranty(ctx context.Context, q *gen.Queries, group, itemID string) (*Warranty, error) {
	w, err := q.GetItemWarranty(ctx, gen.GetItemWarrantyParams{GroupID: group, ItemID: itemID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &w, nil
}

func getOptionalPurchase(ctx context.Context, q *gen.Queries, group, itemID string) (*gen.ItemPurchase, error) {
	p, err := q.GetItemPurchase(ctx, gen.GetItemPurchaseParams{GroupID: group, ItemID: itemID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &p, nil
}

func getOptionalSale(ctx context.Context, q *gen.Queries, group, itemID string) (*gen.ItemSale, error) {
	s, err := q.GetItemSale(ctx, gen.GetItemSaleParams{GroupID: group, ItemID: itemID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &s, nil
}

func upsertWarranty(ctx context.Context, q *gen.Queries, group, itemID string, current *Warranty, proposed *ImportCommitWarranty, now, seq int64) error {
	switch {
	case proposed == nil && current == nil:
		return nil
	case proposed == nil:
		if _, err := q.DeleteItemWarranty(ctx, gen.DeleteItemWarrantyParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true}, Now: now, ChangeSeq: seq, GroupID: group, ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		return nil
	case current == nil:
		id, err := newImportRowID()
		if err != nil {
			return err
		}
		if err := q.CreateItemWarranty(ctx, gen.CreateItemWarrantyParams{
			ID: id, GroupID: group, ItemID: itemID,
			Holder: proposed.Holder, Provider: proposed.Provider,
			StartsOn: nullString(proposed.StartsOn), ExpiresOn: nullString(proposed.ExpiresOn),
			IsLifetime: boolToInt(proposed.IsLifetime), Notes: proposed.Notes,
			Now: now, ChangeSeq: seq,
		}); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		return nil
	default:
		rows, err := q.UpdateItemWarranty(ctx, gen.UpdateItemWarrantyParams{
			Holder: proposed.Holder, Provider: proposed.Provider,
			StartsOn: nullString(proposed.StartsOn), ExpiresOn: nullString(proposed.ExpiresOn),
			IsLifetime: boolToInt(proposed.IsLifetime), Notes: proposed.Notes,
			Now: now, ChangeSeq: seq, GroupID: group, ItemID: itemID, ExpectedVersion: current.Version,
		})
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("update: matched %d rows, want 1 (read version %d); concurrent modification during import commit", rows, current.Version)
		}
		return nil
	}
}

func upsertPurchase(ctx context.Context, q *gen.Queries, group, itemID string, current *gen.ItemPurchase, proposed *ImportCommitPurchase, now, seq int64) error {
	switch {
	case proposed == nil && current == nil:
		return nil
	case proposed == nil:
		if _, err := q.DeleteItemPurchase(ctx, gen.DeleteItemPurchaseParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true}, Now: now, ChangeSeq: seq, GroupID: group, ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		return nil
	case current == nil:
		id, err := newImportRowID()
		if err != nil {
			return err
		}
		if err := q.CreateItemPurchase(ctx, gen.CreateItemPurchaseParams{
			ID: id, GroupID: group, ItemID: itemID,
			Vendor: proposed.Vendor, PurchasedOn: nullString(proposed.PurchasedOn),
			PurchasePriceMinor: proposed.PurchasePriceMinor, OrderReference: proposed.OrderReference, Notes: proposed.Notes,
			Now: now, ChangeSeq: seq,
		}); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		return nil
	default:
		rows, err := q.UpdateItemPurchase(ctx, gen.UpdateItemPurchaseParams{
			Vendor: proposed.Vendor, PurchasedOn: nullString(proposed.PurchasedOn),
			PurchasePriceMinor: proposed.PurchasePriceMinor, OrderReference: proposed.OrderReference, Notes: proposed.Notes,
			Now: now, ChangeSeq: seq, GroupID: group, ItemID: itemID, ExpectedVersion: current.Version,
		})
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("update: matched %d rows, want 1 (read version %d); concurrent modification during import commit", rows, current.Version)
		}
		return nil
	}
}

func upsertSale(ctx context.Context, q *gen.Queries, group, itemID string, current *gen.ItemSale, proposed *ImportCommitSale, now, seq int64) error {
	switch {
	case proposed == nil && current == nil:
		return nil
	case proposed == nil:
		if _, err := q.DeleteItemSale(ctx, gen.DeleteItemSaleParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true}, Now: now, ChangeSeq: seq, GroupID: group, ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		return nil
	case current == nil:
		id, err := newImportRowID()
		if err != nil {
			return err
		}
		if err := q.CreateItemSale(ctx, gen.CreateItemSaleParams{
			ID: id, GroupID: group, ItemID: itemID,
			BuyerName: proposed.BuyerName, SoldOn: nullString(proposed.SoldOn),
			SalePriceMinor: proposed.SalePriceMinor, Notes: proposed.Notes,
			Now: now, ChangeSeq: seq,
		}); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		return nil
	default:
		rows, err := q.UpdateItemSale(ctx, gen.UpdateItemSaleParams{
			BuyerName: proposed.BuyerName, SoldOn: nullString(proposed.SoldOn),
			SalePriceMinor: proposed.SalePriceMinor, Notes: proposed.Notes,
			Now: now, ChangeSeq: seq, GroupID: group, ItemID: itemID, ExpectedVersion: current.Version,
		})
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("update: matched %d rows, want 1 (read version %d); concurrent modification during import commit", rows, current.Version)
		}
		return nil
	}
}

const (
	importShortCodeAlphabet    = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
	importShortCodeLength      = 8
	maxImportShortCodeAttempts = 10
)

func newImportShortCode() (string, error) {
	raw := make([]byte, importShortCodeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("storage: draw a short code for import commit: %w", err)
	}
	out := make([]byte, importShortCodeLength)
	for i, b := range raw {
		out[i] = importShortCodeAlphabet[int(b)%len(importShortCodeAlphabet)]
	}
	return string(out), nil
}

func newImportRowID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("storage: generate id for import commit row: %w", err)
	}
	return id.String(), nil
}
