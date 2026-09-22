package reporting

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type fakeReportRepository struct {
	valuationByLocationFn func(ctx context.Context) ([]storage.LocationValuation, error)
	valuationByLabelFn    func(ctx context.Context) ([]storage.LabelValuation, error)
	warrantyExpiringFn    func(ctx context.Context, from, to string) ([]storage.WarrantyExpiring, error)
	purchasesInRangeFn    func(ctx context.Context, from, to string) ([]storage.PurchaseInRange, error)
}

func (f fakeReportRepository) ValuationByLocation(ctx context.Context) ([]storage.LocationValuation, error) {
	if f.valuationByLocationFn == nil {
		return nil, errors.New("fakeReportRepository: ValuationByLocation not implemented by this test")
	}
	return f.valuationByLocationFn(ctx)
}

func (f fakeReportRepository) ValuationByLabel(ctx context.Context) ([]storage.LabelValuation, error) {
	if f.valuationByLabelFn == nil {
		return nil, errors.New("fakeReportRepository: ValuationByLabel not implemented by this test")
	}
	return f.valuationByLabelFn(ctx)
}

func (f fakeReportRepository) WarrantyExpiring(ctx context.Context, from, to string) ([]storage.WarrantyExpiring, error) {
	if f.warrantyExpiringFn == nil {
		return nil, errors.New("fakeReportRepository: WarrantyExpiring not implemented by this test")
	}
	return f.warrantyExpiringFn(ctx, from, to)
}

func (f fakeReportRepository) PurchasesInRange(ctx context.Context, from, to string) ([]storage.PurchaseInRange, error) {
	if f.purchasesInRangeFn == nil {
		return nil, errors.New("fakeReportRepository: PurchasesInRange not implemented by this test")
	}
	return f.purchasesInRangeFn(ctx, from, to)
}

func TestParseGroupBy(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    GroupBy
		wantErr bool
	}{
		{name: "location", raw: "location", want: GroupByLocation},
		{name: "label", raw: "label", want: GroupByLabel},
		{name: "empty is invalid", raw: "", wantErr: true},
		{name: "unknown is invalid", raw: "room", wantErr: true},
		{name: "case sensitive", raw: "Location", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseGroupBy(tc.raw)
			if tc.wantErr {
				if !errors.Is(err, ErrGroupByInvalid) {
					t.Fatalf("ParseGroupBy(%q) error = %v, want ErrGroupByInvalid", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGroupBy(%q) = %v, want success", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("ParseGroupBy(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestValuationRejectsNilRepository(t *testing.T) {
	if _, err := Valuation(t.Context(), nil, GroupByLocation); err == nil {
		t.Error("Valuation(nil repository) succeeded; want an error")
	}
}

func TestValuationRejectsInvalidGroupBy(t *testing.T) {
	repo := fakeReportRepository{}
	_, err := Valuation(t.Context(), repo, GroupBy("bogus"))
	if !errors.Is(err, ErrGroupByInvalid) {
		t.Fatalf("Valuation(bogus) error = %v, want ErrGroupByInvalid", err)
	}
}

func TestValuationByLocationLabelsTheUnassignedBucket(t *testing.T) {
	repo := fakeReportRepository{
		valuationByLocationFn: func(context.Context) ([]storage.LocationValuation, error) {
			return []storage.LocationValuation{
				{LocationID: "", LocationName: "", ItemCount: 2, TotalValueMinor: 500},
				{LocationID: "loc-1", LocationName: "Garage", ItemCount: 3, TotalValueMinor: 12345},
			}, nil
		},
	}

	got, err := Valuation(t.Context(), repo, GroupByLocation)
	if err != nil {
		t.Fatalf("Valuation(location) = %v, want success", err)
	}
	if len(got) != 2 {
		t.Fatalf("Valuation(location) returned %d rows, want 2", len(got))
	}

	unassigned := got[0]
	if unassigned.GroupKey != "" {
		t.Errorf("unassigned row GroupKey = %q, want \"\"", unassigned.GroupKey)
	}
	if unassigned.GroupLabel != unassignedLocationLabel {
		t.Errorf("unassigned row GroupLabel = %q, want %q", unassigned.GroupLabel, unassignedLocationLabel)
	}
	if unassigned.ItemCount != 2 || unassigned.TotalValueMinor != 500 {
		t.Errorf("unassigned row = %+v, want ItemCount 2 and TotalValueMinor 500", unassigned)
	}

	garage := got[1]
	if garage.GroupKey != "loc-1" || garage.GroupLabel != "Garage" {
		t.Errorf("garage row = %+v, want GroupKey \"loc-1\" and GroupLabel \"Garage\"", garage)
	}
	if garage.ItemCount != 3 || garage.TotalValueMinor != 12345 {
		t.Errorf("garage row = %+v, want ItemCount 3 and TotalValueMinor 12345", garage)
	}
}

func TestValuationByLabelPassesThroughLabelRows(t *testing.T) {
	repo := fakeReportRepository{
		valuationByLabelFn: func(context.Context) ([]storage.LabelValuation, error) {
			return []storage.LabelValuation{
				{LabelID: "lbl-1", LabelName: "Fragile", ItemCount: 4, TotalValueMinor: 9900},
			}, nil
		},
	}

	got, err := Valuation(t.Context(), repo, GroupByLabel)
	if err != nil {
		t.Fatalf("Valuation(label) = %v, want success", err)
	}
	want := []ValuationRow{{GroupKey: "lbl-1", GroupLabel: "Fragile", ItemCount: 4, TotalValueMinor: 9900}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("Valuation(label) = %+v, want %+v", got, want)
	}
}

func TestValuationPropagatesRepositoryError(t *testing.T) {
	sentinel := errors.New("boom")
	repo := fakeReportRepository{
		valuationByLocationFn: func(context.Context) ([]storage.LocationValuation, error) {
			return nil, sentinel
		},
	}
	if _, err := Valuation(t.Context(), repo, GroupByLocation); !errors.Is(err, sentinel) {
		t.Fatalf("Valuation() error = %v, want it to wrap the repository's own error", err)
	}
}
