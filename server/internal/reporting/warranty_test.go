package reporting

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
	"time"
)

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(warrantyDateLayout, s)
	if err != nil {
		t.Fatalf("parse fixture date %q: %v", s, err)
	}
	return d
}

func TestWarrantyExpiringRejectsNilRepository(t *testing.T) {
	if _, err := WarrantyExpiring(t.Context(), nil, time.Now(), DefaultWithinDays); err == nil {
		t.Error("WarrantyExpiring(nil repository) succeeded; want an error")
	}
}

func TestWarrantyExpiringRejectsZeroNow(t *testing.T) {
	repo := fakeReportRepository{}
	if _, err := WarrantyExpiring(t.Context(), repo, time.Time{}, DefaultWithinDays); err == nil {
		t.Error("WarrantyExpiring(zero now) succeeded; want an error")
	}
}

func TestWarrantyExpiringRejectsNegativeWithinDays(t *testing.T) {
	repo := fakeReportRepository{}
	_, err := WarrantyExpiring(t.Context(), repo, mustParseDate(t, "2026-09-15"), -1)
	if !errors.Is(err, ErrWithinDaysInvalid) {
		t.Fatalf("WarrantyExpiring(withinDays=-1) error = %v, want ErrWithinDaysInvalid", err)
	}
}

func TestWarrantyExpiringComputesTheWindow(t *testing.T) {
	var gotFrom, gotTo string
	repo := fakeReportRepository{
		warrantyExpiringFn: func(_ context.Context, from, to string) ([]storage.WarrantyExpiring, error) {
			gotFrom, gotTo = from, to
			return nil, nil
		},
	}

	now := mustParseDate(t, "2026-09-15")
	if _, err := WarrantyExpiring(t.Context(), repo, now, 30); err != nil {
		t.Fatalf("WarrantyExpiring() = %v, want success", err)
	}
	if gotFrom != "2026-09-15" {
		t.Errorf("from = %q, want today (2026-09-15)", gotFrom)
	}
	if gotTo != "2026-10-15" {
		t.Errorf("to = %q, want today + 30 days (2026-10-15)", gotTo)
	}
}

func TestWarrantyExpiringWithinDaysZeroIsTodayOnly(t *testing.T) {
	var gotFrom, gotTo string
	repo := fakeReportRepository{
		warrantyExpiringFn: func(_ context.Context, from, to string) ([]storage.WarrantyExpiring, error) {
			gotFrom, gotTo = from, to
			return nil, nil
		},
	}
	now := mustParseDate(t, "2026-09-15")
	if _, err := WarrantyExpiring(t.Context(), repo, now, 0); err != nil {
		t.Fatalf("WarrantyExpiring(withinDays=0) = %v, want success", err)
	}
	if gotFrom != "2026-09-15" || gotTo != "2026-09-15" {
		t.Errorf("from/to = %q/%q, want both 2026-09-15", gotFrom, gotTo)
	}
}

func TestWarrantyExpiringComputesDaysRemaining(t *testing.T) {
	repo := fakeReportRepository{
		warrantyExpiringFn: func(context.Context, string, string) ([]storage.WarrantyExpiring, error) {
			return []storage.WarrantyExpiring{
				{ItemID: "item-1", ItemName: "Fridge", ExpiresOn: "2026-09-15"},
				{ItemID: "item-2", ItemName: "Oven", ExpiresOn: "2026-09-20"},
			}, nil
		},
	}

	now := mustParseDate(t, "2026-09-15")
	got, err := WarrantyExpiring(t.Context(), repo, now, 30)
	if err != nil {
		t.Fatalf("WarrantyExpiring() = %v, want success", err)
	}
	if len(got) != 2 {
		t.Fatalf("WarrantyExpiring() returned %d rows, want 2", len(got))
	}
	if got[0].DaysRemaining != 0 {
		t.Errorf("row 0 (expires today) DaysRemaining = %d, want 0", got[0].DaysRemaining)
	}
	if got[1].DaysRemaining != 5 {
		t.Errorf("row 1 (expires in 5 days) DaysRemaining = %d, want 5", got[1].DaysRemaining)
	}
}

func TestWarrantyExpiringRejectsAnUnparsableExpiresOn(t *testing.T) {
	repo := fakeReportRepository{
		warrantyExpiringFn: func(context.Context, string, string) ([]storage.WarrantyExpiring, error) {
			return []storage.WarrantyExpiring{{ItemID: "item-1", ItemName: "Fridge", ExpiresOn: "not-a-date"}}, nil
		},
	}
	if _, err := WarrantyExpiring(t.Context(), repo, mustParseDate(t, "2026-09-15"), 30); err == nil {
		t.Error("WarrantyExpiring() with a corrupted expires_on succeeded; want an error")
	}
}

func TestWarrantyExpiringPropagatesRepositoryError(t *testing.T) {
	sentinel := errors.New("boom")
	repo := fakeReportRepository{
		warrantyExpiringFn: func(context.Context, string, string) ([]storage.WarrantyExpiring, error) {
			return nil, sentinel
		},
	}
	if _, err := WarrantyExpiring(t.Context(), repo, mustParseDate(t, "2026-09-15"), 30); !errors.Is(err, sentinel) {
		t.Fatalf("WarrantyExpiring() error = %v, want it to wrap the repository's own error", err)
	}
}
