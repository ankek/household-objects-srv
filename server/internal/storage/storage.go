package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

var ErrNoGroup = errors.New("storage: repositories requested without a group scope")

var ErrNotFound = errors.New("storage: not found")

type Config struct {
	Path string

	ReadConns int
}

type Storage struct {
	store *db.Store
}

func Open(ctx context.Context, cfg Config) (*Storage, error) {
	store, err := db.Open(ctx, db.Config{Path: cfg.Path, ReadConns: cfg.ReadConns})
	if err != nil {
		return nil, err
	}
	return &Storage{store: store}, nil
}

func (s *Storage) Close() error { return s.store.Close() }

func (s *Storage) SchemaVersion() int64 { return s.store.SchemaVersion() }

func (s *Storage) GroupIDs(ctx context.Context) ([]GroupID, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}

	rows, err := gen.New(s.store.Reader()).ListGroupIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: list group ids: %w", err)
	}

	ids := make([]GroupID, 0, len(rows))
	for _, raw := range rows {
		gid, err := NewGroupID(raw)
		if err != nil {
			return nil, fmt.Errorf("storage: group id %q read from groups.id: %w", raw, err)
		}
		ids = append(ids, gid)
	}
	return ids, nil
}

func (s *Storage) VacuumInto(ctx context.Context, dstPath string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if dstPath == "" {
		return errors.New("storage: vacuum into: destination path is empty")
	}
	if _, err := s.store.Writer().ExecContext(ctx, `VACUUM INTO ?`, dstPath); err != nil {
		return fmt.Errorf("storage: vacuum into %q: %w", dstPath, err)
	}
	return nil
}

func (s *Storage) ForGroup(g GroupID) (Scope, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if g.IsZero() {
		return nil, fmt.Errorf("%w: group id is the zero value", ErrNoGroup)
	}
	return groupScope{binding{q: gen.New(s.store.Reader()), gid: g, store: s.store}}, nil
}

func (s *Storage) ForGroupReports(g GroupID) (ReportRepository, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if g.IsZero() {
		return nil, fmt.Errorf("%w: group id is the zero value", ErrNoGroup)
	}
	return reportRepository{binding{q: gen.New(s.store.Reader()), gid: g, store: s.store}}, nil
}

func (s *Storage) ForGroupImportSessions(g GroupID) (ImportSessionRepository, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if g.IsZero() {
		return nil, fmt.Errorf("%w: group id is the zero value", ErrNoGroup)
	}
	return importSessionRepository{binding{q: gen.New(s.store.Reader()), gid: g, store: s.store}}, nil
}

type Scope interface {
	GroupID() GroupID

	Items() ItemRepository

	Warranty() WarrantyRepository

	Sale() SaleRepository

	Purchase() PurchaseRepository

	Visibility() GroupVisibilityRepository

	Identifications() IdentificationRepository

	CustomFieldDefs() CustomFieldDefRepository

	ItemCustomFields() ItemCustomFieldRepository

	StockAdjustments() StockAdjustmentRepository

	Labels() LabelRepository

	ItemLabels() ItemLabelRepository

	Locations() LocationRepository

	Attachments() AttachmentRepository

	Members() MemberRepository
}

type groupScope struct {
	binding
}

func (s groupScope) GroupID() GroupID { return s.gid }

func (s groupScope) Items() ItemRepository { return itemRepository{s.binding} } //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Warranty() WarrantyRepository { return warrantyRepository{s.binding} } //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Sale() SaleRepository { return saleRepository{s.binding} } //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Purchase() PurchaseRepository { return purchaseRepository{s.binding} } //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Visibility() GroupVisibilityRepository {
	return groupVisibilityRepository{s.binding}
} //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Identifications() IdentificationRepository {
	return identificationRepository{s.binding}
} //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) CustomFieldDefs() CustomFieldDefRepository {
	return customFieldDefRepository{s.binding}
} //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) ItemCustomFields() ItemCustomFieldRepository {
	return itemCustomFieldRepository{s.binding}
} //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) StockAdjustments() StockAdjustmentRepository {
	return stockAdjustmentRepository{s.binding}
} //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Labels() LabelRepository         { return labelRepository{s.binding} }     //nolint:staticcheck // S1016: naming the field is the point
func (s groupScope) ItemLabels() ItemLabelRepository { return itemLabelRepository{s.binding} } //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Locations() LocationRepository { return locationRepository{s.binding} } //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Attachments() AttachmentRepository { return attachmentRepository{s.binding} } //nolint:staticcheck // S1016: naming the field is the point

func (s groupScope) Members() MemberRepository { return memberRepository{s.binding} } //nolint:staticcheck // S1016: naming the field is the point

type binding struct {
	q     *gen.Queries
	gid   GroupID
	store *db.Store
}

func (b binding) queries() *gen.Queries { return b.q }

func (b binding) group() string { return b.gid.id }

func (b binding) readPool() *sql.DB { return b.store.Reader() }

func (b binding) writeTx(ctx context.Context, fn func(tx *sql.Tx, q *gen.Queries) error) error {
	return b.store.Tx(ctx, func(tx *sql.Tx) error {
		return fn(tx, gen.New(tx))
	})
}
