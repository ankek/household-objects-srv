package reporting

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

func TestPurchasesInRangeRejectsNilRepository(t *testing.T) {
	if _, err := PurchasesInRange(t.Context(), nil, "2026-01-01", "2026-12-31"); err == nil {
		t.Error("PurchasesInRange(nil repository) succeeded; want an error")
	}
}

func TestPurchasesInRangeRejectsMalformedDates(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
	}{
		{name: "from malformed", from: "not-a-date", to: "2026-12-31"},
		{name: "to malformed", from: "2026-01-01", to: "not-a-date"},
		{name: "from is not a real calendar day", from: "2026-02-30", to: "2026-12-31"},
		{name: "from after to", from: "2026-12-31", to: "2026-01-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := fakeReportRepository{
				purchasesInRangeFn: func(context.Context, string, string) ([]storage.PurchaseInRange, error) {
					t.Fatal("repo.PurchasesInRange was called; the invalid range should have been rejected first")
					return nil, nil
				},
			}
			_, err := PurchasesInRange(t.Context(), repo, tc.from, tc.to)
			if !errors.Is(err, ErrDateRangeInvalid) {
				t.Fatalf("PurchasesInRange(%q, %q) error = %v, want ErrDateRangeInvalid", tc.from, tc.to, err)
			}
		})
	}
}

func TestPurchasesInRangeAcceptsAnInclusiveSingleDayRange(t *testing.T) {
	var gotFrom, gotTo string
	repo := fakeReportRepository{
		purchasesInRangeFn: func(_ context.Context, from, to string) ([]storage.PurchaseInRange, error) {
			gotFrom, gotTo = from, to
			return nil, nil
		},
	}
	if _, err := PurchasesInRange(t.Context(), repo, "2026-09-15", "2026-09-15"); err != nil {
		t.Fatalf("PurchasesInRange(same day) = %v, want success", err)
	}
	if gotFrom != "2026-09-15" || gotTo != "2026-09-15" {
		t.Errorf("repo.PurchasesInRange saw (%q, %q), want (2026-09-15, 2026-09-15) passed through unmodified", gotFrom, gotTo)
	}
}

func TestPurchasesInRangePassesThroughRows(t *testing.T) {
	repo := fakeReportRepository{
		purchasesInRangeFn: func(context.Context, string, string) ([]storage.PurchaseInRange, error) {
			return []storage.PurchaseInRange{
				{ItemID: "item-1", ItemName: "Sofa", PurchasedOn: "2026-03-01", Vendor: "IKEA", PurchasePriceMinor: 79900},
			}, nil
		},
	}

	got, err := PurchasesInRange(t.Context(), repo, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("PurchasesInRange() = %v, want success", err)
	}
	want := []PurchaseRow{{ItemID: "item-1", ItemName: "Sofa", PurchasedOn: "2026-03-01", Vendor: "IKEA", PurchasePriceMinor: 79900}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("PurchasesInRange() = %+v, want %+v", got, want)
	}
}

func TestPurchasesInRangePropagatesRepositoryError(t *testing.T) {
	sentinel := errors.New("boom")
	repo := fakeReportRepository{
		purchasesInRangeFn: func(context.Context, string, string) ([]storage.PurchaseInRange, error) {
			return nil, sentinel
		},
	}
	if _, err := PurchasesInRange(t.Context(), repo, "2026-01-01", "2026-12-31"); !errors.Is(err, sentinel) {
		t.Fatalf("PurchasesInRange() error = %v, want it to wrap the repository's own error", err)
	}
}
