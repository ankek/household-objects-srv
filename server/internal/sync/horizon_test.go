package sync

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type countingRepo struct {
	storage.SyncRepository
	entityCalls int
}

func (c *countingRepo) Items(ctx context.Context, since, limit int64) ([]storage.Item, error) {
	c.entityCalls++
	return c.SyncRepository.Items(ctx, since, limit)
}

func (c *countingRepo) ItemsAtSeq(ctx context.Context, seq int64) ([]storage.Item, error) {
	c.entityCalls++
	return c.SyncRepository.ItemsAtSeq(ctx, seq)
}

func (c *countingRepo) Warranty(ctx context.Context, since, limit int64) ([]storage.Warranty, error) {
	c.entityCalls++
	return c.SyncRepository.Warranty(ctx, since, limit)
}

func (c *countingRepo) WarrantyAtSeq(ctx context.Context, seq int64) ([]storage.Warranty, error) {
	c.entityCalls++
	return c.SyncRepository.WarrantyAtSeq(ctx, seq)
}

func (c *countingRepo) Sale(ctx context.Context, since, limit int64) ([]storage.Sale, error) {
	c.entityCalls++
	return c.SyncRepository.Sale(ctx, since, limit)
}

func (c *countingRepo) SaleAtSeq(ctx context.Context, seq int64) ([]storage.Sale, error) {
	c.entityCalls++
	return c.SyncRepository.SaleAtSeq(ctx, seq)
}

func (c *countingRepo) Purchase(ctx context.Context, since, limit int64) ([]storage.Purchase, error) {
	c.entityCalls++
	return c.SyncRepository.Purchase(ctx, since, limit)
}

func (c *countingRepo) PurchaseAtSeq(ctx context.Context, seq int64) ([]storage.Purchase, error) {
	c.entityCalls++
	return c.SyncRepository.PurchaseAtSeq(ctx, seq)
}

func (c *countingRepo) Identifications(ctx context.Context, since, limit int64) ([]storage.Identification, error) {
	c.entityCalls++
	return c.SyncRepository.Identifications(ctx, since, limit)
}

func (c *countingRepo) IdentificationsAtSeq(ctx context.Context, seq int64) ([]storage.Identification, error) {
	c.entityCalls++
	return c.SyncRepository.IdentificationsAtSeq(ctx, seq)
}

func (c *countingRepo) ItemCustomFields(ctx context.Context, since, limit int64) ([]storage.ItemCustomField, error) {
	c.entityCalls++
	return c.SyncRepository.ItemCustomFields(ctx, since, limit)
}

func (c *countingRepo) ItemCustomFieldsAtSeq(ctx context.Context, seq int64) ([]storage.ItemCustomField, error) {
	c.entityCalls++
	return c.SyncRepository.ItemCustomFieldsAtSeq(ctx, seq)
}

func (c *countingRepo) StockAdjustments(ctx context.Context, since, limit int64) ([]storage.StockAdjustment, error) {
	c.entityCalls++
	return c.SyncRepository.StockAdjustments(ctx, since, limit)
}

func (c *countingRepo) StockAdjustmentsAtSeq(ctx context.Context, seq int64) ([]storage.StockAdjustment, error) {
	c.entityCalls++
	return c.SyncRepository.StockAdjustmentsAtSeq(ctx, seq)
}

func (c *countingRepo) Locations(ctx context.Context, since, limit int64) ([]storage.Location, error) {
	c.entityCalls++
	return c.SyncRepository.Locations(ctx, since, limit)
}

func (c *countingRepo) LocationsAtSeq(ctx context.Context, seq int64) ([]storage.Location, error) {
	c.entityCalls++
	return c.SyncRepository.LocationsAtSeq(ctx, seq)
}

func (c *countingRepo) Labels(ctx context.Context, since, limit int64) ([]storage.Label, error) {
	c.entityCalls++
	return c.SyncRepository.Labels(ctx, since, limit)
}

func (c *countingRepo) LabelsAtSeq(ctx context.Context, seq int64) ([]storage.Label, error) {
	c.entityCalls++
	return c.SyncRepository.LabelsAtSeq(ctx, seq)
}

func (c *countingRepo) ItemLabels(ctx context.Context, since, limit int64) ([]storage.ItemLabelAssignment, error) {
	c.entityCalls++
	return c.SyncRepository.ItemLabels(ctx, since, limit)
}

func (c *countingRepo) ItemLabelsAtSeq(ctx context.Context, seq int64) ([]storage.ItemLabelAssignment, error) {
	c.entityCalls++
	return c.SyncRepository.ItemLabelsAtSeq(ctx, seq)
}

func (c *countingRepo) Attachments(ctx context.Context, since, limit int64) ([]storage.Attachment, error) {
	c.entityCalls++
	return c.SyncRepository.Attachments(ctx, since, limit)
}

func (c *countingRepo) AttachmentsAtSeq(ctx context.Context, seq int64) ([]storage.Attachment, error) {
	c.entityCalls++
	return c.SyncRepository.AttachmentsAtSeq(ctx, seq)
}

func readerWithCounter(t *testing.T, s *storage.Storage, groupID string) (*Reader, *countingRepo) {
	t.Helper()
	repo, err := s.ForGroupSync(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupSync(%s): %v", groupID, err)
	}
	counted := &countingRepo{SyncRepository: repo}
	return NewReader(counted), counted
}

func TestReaderTooOldBoundaries(t *testing.T) {
	cases := []struct {
		name         string
		since        int64
		lowWatermark int64
		wantTooOld   bool
	}{
		{
			name:         "since zero is always a valid full resync regardless of watermark",
			since:        0,
			lowWatermark: 1_000_000,
			wantTooOld:   false,
		},
		{
			name:         "since zero is valid even against a zero watermark (nothing purged)",
			since:        0,
			lowWatermark: 0,
			wantTooOld:   false,
		},
		{
			name:         "since equal to the watermark is still valid -- the boundary itself is reconstructable",
			since:        1_000_000,
			lowWatermark: 1_000_000,
			wantTooOld:   false,
		},
		{
			name:         "since one below the watermark has already been purged",
			since:        999_999,
			lowWatermark: 1_000_000,
			wantTooOld:   true,
		},
		{
			name:         "since above the watermark is valid",
			since:        1_000_001,
			lowWatermark: 1_000_000,
			wantTooOld:   false,
		},
		{
			name:         "default watermark (nothing ever purged) never trips",
			since:        1,
			lowWatermark: 0,
			wantTooOld:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSyncTestStorage(t)
			seedGroup(t, s, "groupA")
			repo, err := s.ForGroupSync(storage.MustGroupID("groupA"))
			if err != nil {
				t.Fatalf("ForGroupSync: %v", err)
			}
			if err := repo.SetLowWatermark(t.Context(), tc.lowWatermark); err != nil {
				t.Fatalf("SetLowWatermark(%d): %v", tc.lowWatermark, err)
			}

			r := NewReader(repo)
			got, err := r.TooOld(t.Context(), tc.since)
			if err != nil {
				t.Fatalf("TooOld(%d): %v", tc.since, err)
			}
			if got != tc.wantTooOld {
				t.Errorf("TooOld(since=%d, lowWatermark=%d) = %v, want %v", tc.since, tc.lowWatermark, got, tc.wantTooOld)
			}
		})
	}
}

func TestReaderTooOldRejectsNegativeSince(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "groupA")
	r := readerFor(t, s, "groupA")

	if _, err := r.TooOld(t.Context(), -1); !errors.Is(err, ErrInvalidSince) {
		t.Errorf("TooOld(-1): err = %v, want ErrInvalidSince", err)
	}
}

func TestReaderTooOldNilRepository(t *testing.T) {
	var r *Reader
	if _, err := r.TooOld(context.Background(), 5); !errors.Is(err, ErrNoRepository) {
		t.Errorf("TooOld on a nil *Reader: err = %v, want ErrNoRepository", err)
	}

	r = NewReader(nil)
	if _, err := r.TooOld(context.Background(), 5); !errors.Is(err, ErrNoRepository) {
		t.Errorf("TooOld with a nil repo: err = %v, want ErrNoRepository", err)
	}
}

func TestPullOrTooOldRunsNoEntityQueryWhenTripped(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "groupA")
	seedItem(t, scope, "item-1", "Item One", 10)

	repo, err := s.ForGroupSync(storage.MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroupSync: %v", err)
	}
	if err := repo.SetLowWatermark(t.Context(), 1_000_000); err != nil {
		t.Fatalf("SetLowWatermark: %v", err)
	}

	counted := &countingRepo{SyncRepository: repo}
	r := NewReader(counted)

	page, tooOld, err := r.PullOrTooOld(t.Context(), 999_999, 50)
	if err != nil {
		t.Fatalf("PullOrTooOld: %v", err)
	}
	if !tooOld {
		t.Fatal("PullOrTooOld: cursorTooOld = false, want true (since is one below the watermark)")
	}
	if len(page.Changes) != 0 || len(page.Tombstones) != 0 {
		t.Errorf("PullOrTooOld returned a non-empty page (%+v) alongside cursorTooOld=true", page)
	}
	if counted.entityCalls != 0 {
		t.Errorf("PullOrTooOld executed %d entity-table query call(s) after tripping cursor_too_old, want 0", counted.entityCalls)
	}
}

func TestPullOrTooOldRunsPullWhenNotTripped(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "groupA")
	seedItem(t, scope, "item-1", "Item One", 10)

	r, counted := readerWithCounter(t, s, "groupA")

	page, tooOld, err := r.PullOrTooOld(t.Context(), 0, 50)
	if err != nil {
		t.Fatalf("PullOrTooOld: %v", err)
	}
	if tooOld {
		t.Fatal("PullOrTooOld: cursorTooOld = true, want false (default watermark 0, since 0 is always valid)")
	}
	if len(page.Changes) != 1 {
		t.Errorf("PullOrTooOld page.Changes = %d entries, want 1 (the seeded item)", len(page.Changes))
	}
	if counted.entityCalls == 0 {
		t.Error("PullOrTooOld ran zero entity-table queries on the non-tripped path; the counting instrumentation itself is not working")
	}
}
