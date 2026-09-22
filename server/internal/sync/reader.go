package sync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"sort"
)

type EntityType string

const (
	EntityItem               EntityType = "item"
	EntityWarrantyBlock      EntityType = "warranty_block"
	EntitySoldToBlock        EntityType = "sold_to_block"
	EntityPurchasedFromBlock EntityType = "purchased_from_block"
	EntityItemIdentification EntityType = "item_identification"
	EntityItemCustomField    EntityType = "item_custom_field"
	EntityStockAdjustment    EntityType = "stock_adjustment"
	EntityLocation           EntityType = "location"
	EntityLabel              EntityType = "label"
	EntityItemLabel          EntityType = "item_label"
	EntityAttachment         EntityType = "attachment"
)

type Change struct {
	EntityType     EntityType
	ID             string
	GroupChangeSeq int64
	Data           any
}

type Tombstone struct {
	EntityType EntityType
	ID         string
	DeletedAt  int64
}

type Page struct {
	Changes       []Change
	Tombstones    []Tombstone
	NextWatermark int64
	HasMore       bool
}

var ErrNoRepository = errors.New("sync: reader has no repository")

var ErrInvalidLimit = errors.New("sync: limit must be positive")

var ErrInvalidSince = errors.New("sync: since must not be negative")

type Reader struct {
	repo storage.SyncRepository
}

func NewReader(repo storage.SyncRepository) *Reader {
	return &Reader{repo: repo}
}

type candidate struct {
	entityType EntityType
	id         string
	changeSeq  int64
	deletedAt  sql.NullInt64
	data       any
}

type tableSource struct {
	entityType EntityType
	since      func(ctx context.Context, since, limit int64) ([]candidate, error)
	atSeq      func(ctx context.Context, seq int64) ([]candidate, error)
}

func (r *Reader) sources() []tableSource {
	return []tableSource{
		{
			entityType: EntityItem,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.Items(ctx, since, limit)
				return itemCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.ItemsAtSeq(ctx, seq)
				return itemCandidates(rows), err
			},
		},
		{
			entityType: EntityWarrantyBlock,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.Warranty(ctx, since, limit)
				return warrantyCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.WarrantyAtSeq(ctx, seq)
				return warrantyCandidates(rows), err
			},
		},
		{
			entityType: EntitySoldToBlock,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.Sale(ctx, since, limit)
				return saleCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.SaleAtSeq(ctx, seq)
				return saleCandidates(rows), err
			},
		},
		{
			entityType: EntityPurchasedFromBlock,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.Purchase(ctx, since, limit)
				return purchaseCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.PurchaseAtSeq(ctx, seq)
				return purchaseCandidates(rows), err
			},
		},
		{
			entityType: EntityItemIdentification,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.Identifications(ctx, since, limit)
				return identificationCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.IdentificationsAtSeq(ctx, seq)
				return identificationCandidates(rows), err
			},
		},
		{
			entityType: EntityItemCustomField,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.ItemCustomFields(ctx, since, limit)
				return customFieldCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.ItemCustomFieldsAtSeq(ctx, seq)
				return customFieldCandidates(rows), err
			},
		},
		{
			entityType: EntityStockAdjustment,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.StockAdjustments(ctx, since, limit)
				return stockAdjustmentCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.StockAdjustmentsAtSeq(ctx, seq)
				return stockAdjustmentCandidates(rows), err
			},
		},
		{
			entityType: EntityLocation,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.Locations(ctx, since, limit)
				return locationCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.LocationsAtSeq(ctx, seq)
				return locationCandidates(rows), err
			},
		},
		{
			entityType: EntityLabel,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.Labels(ctx, since, limit)
				return labelCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.LabelsAtSeq(ctx, seq)
				return labelCandidates(rows), err
			},
		},
		{
			entityType: EntityItemLabel,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.ItemLabels(ctx, since, limit)
				return itemLabelCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.ItemLabelsAtSeq(ctx, seq)
				return itemLabelCandidates(rows), err
			},
		},
		{
			entityType: EntityAttachment,
			since: func(ctx context.Context, since, limit int64) ([]candidate, error) {
				rows, err := r.repo.Attachments(ctx, since, limit)
				return attachmentCandidates(rows), err
			},
			atSeq: func(ctx context.Context, seq int64) ([]candidate, error) {
				rows, err := r.repo.AttachmentsAtSeq(ctx, seq)
				return attachmentCandidates(rows), err
			},
		},
	}
}

func itemCandidates(rows []storage.Item) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityItem, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func warrantyCandidates(rows []storage.Warranty) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityWarrantyBlock, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func saleCandidates(rows []storage.Sale) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntitySoldToBlock, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func purchaseCandidates(rows []storage.Purchase) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityPurchasedFromBlock, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func identificationCandidates(rows []storage.Identification) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityItemIdentification, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func customFieldCandidates(rows []storage.ItemCustomField) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityItemCustomField, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func stockAdjustmentCandidates(rows []storage.StockAdjustment) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityStockAdjustment, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func locationCandidates(rows []storage.Location) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityLocation, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func labelCandidates(rows []storage.Label) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityLabel, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func itemLabelCandidates(rows []storage.ItemLabelAssignment) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityItemLabel, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

func attachmentCandidates(rows []storage.Attachment) []candidate {
	out := make([]candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate{EntityAttachment, row.ID, row.ChangeSeq, row.DeletedAt, row})
	}
	return out
}

type seqGroup struct {
	seq     int64
	members []candidate
}

func groupByChangeSeq(sorted []candidate) []seqGroup {
	var groups []seqGroup
	for _, c := range sorted {
		if n := len(groups); n > 0 && groups[n-1].seq == c.changeSeq {
			groups[n-1].members = append(groups[n-1].members, c)
			continue
		}
		groups = append(groups, seqGroup{seq: c.changeSeq, members: []candidate{c}})
	}
	return groups
}

func (r *Reader) fetchSince(ctx context.Context, since, limit int64) ([]candidate, error) {
	var candidates []candidate
	for _, src := range r.sources() {
		rows, err := src.since(ctx, since, limit)
		if err != nil {
			return nil, fmt.Errorf("sync: pull %s changes since %d: %w", src.entityType, since, err)
		}
		candidates = append(candidates, rows...)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].changeSeq < candidates[j].changeSeq })
	return candidates, nil
}

func (r *Reader) fetchAtSeq(ctx context.Context, seq int64) ([]candidate, error) {
	var members []candidate
	for _, src := range r.sources() {
		rows, err := src.atSeq(ctx, seq)
		if err != nil {
			return nil, fmt.Errorf("sync: pull %s changes at seq %d: %w", src.entityType, seq, err)
		}
		members = append(members, rows...)
	}
	return members, nil
}

func (r *Reader) probeHasMore(ctx context.Context, watermark int64) (bool, error) {
	for _, src := range r.sources() {
		rows, err := src.since(ctx, watermark, 1)
		if err != nil {
			return false, fmt.Errorf("sync: probe %s changes beyond %d: %w", src.entityType, watermark, err)
		}
		if len(rows) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func buildPage(ordered []candidate, nextWatermark int64, hasMore bool) Page {
	page := Page{NextWatermark: nextWatermark, HasMore: hasMore}
	for _, c := range ordered {
		if c.deletedAt.Valid {
			page.Tombstones = append(page.Tombstones, Tombstone{
				EntityType: c.entityType,
				ID:         c.id,
				DeletedAt:  c.deletedAt.Int64,
			})
		} else {
			page.Changes = append(page.Changes, Change{
				EntityType:     c.entityType,
				ID:             c.id,
				GroupChangeSeq: c.changeSeq,
				Data:           c.data,
			})
		}
	}
	return page
}

func (r *Reader) Pull(ctx context.Context, since int64, limit int64) (Page, error) {
	if r == nil || r.repo == nil {
		return Page{}, fmt.Errorf("%w", ErrNoRepository)
	}
	if limit <= 0 {
		return Page{}, fmt.Errorf("%w: got %d", ErrInvalidLimit, limit)
	}
	if since < 0 {
		return Page{}, fmt.Errorf("%w: got %d", ErrInvalidSince, since)
	}

	fetchLimit := limit + 1
	candidates, err := r.fetchSince(ctx, since, fetchLimit)
	if err != nil {
		return Page{}, err
	}
	if len(candidates) == 0 {
		return Page{NextWatermark: since, HasMore: false}, nil
	}

	groups := groupByChangeSeq(candidates)
	s0 := groups[0].seq

	firstGroup, err := r.fetchAtSeq(ctx, s0)
	if err != nil {
		return Page{}, err
	}

	if int64(len(firstGroup)) > limit {
		hasMore, err := r.probeHasMore(ctx, s0)
		if err != nil {
			return Page{}, err
		}
		return buildPage(firstGroup, s0, hasMore), nil
	}

	included := append([]candidate(nil), firstGroup...)
	cumulative := int64(len(firstGroup))
	lastSeq := s0

	for _, g := range groups[1:] {
		if cumulative+int64(len(g.members)) > limit {
			return buildPage(included, lastSeq, true), nil
		}
		included = append(included, g.members...)
		cumulative += int64(len(g.members))
		lastSeq = g.seq
	}

	hasMore, err := r.probeHasMore(ctx, lastSeq)
	if err != nil {
		return Page{}, err
	}
	return buildPage(included, lastSeq, hasMore), nil
}
