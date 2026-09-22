package main

import (
	"testing"
	"time"
)

func TestSummarizeEmpty(t *testing.T) {
	got := summarize(nil)
	if got != (latencyStats{}) {
		t.Fatalf("summarize(nil) = %+v, want the zero value", got)
	}
}

func TestSummarizeDoesNotMutateInput(t *testing.T) {
	in := []time.Duration{5 * time.Millisecond, 1 * time.Millisecond, 3 * time.Millisecond}
	want := []time.Duration{5 * time.Millisecond, 1 * time.Millisecond, 3 * time.Millisecond}
	_ = summarize(in)
	for i := range in {
		if in[i] != want[i] {
			t.Fatalf("summarize mutated its input slice: got %v, want %v", in, want)
		}
	}
}

func TestSummarizeKnownDistribution(t *testing.T) {
	ds := make([]time.Duration, 100)
	for i := range ds {
		ds[i] = time.Duration(i+1) * time.Millisecond
	}
	got := summarize(ds)
	switch {
	case got.Count != 100:
		t.Errorf("Count = %d, want 100", got.Count)
	case got.MinMS != 1:
		t.Errorf("MinMS = %v, want 1", got.MinMS)
	case got.MaxMS != 100:
		t.Errorf("MaxMS = %v, want 100", got.MaxMS)
	case got.P50MS != 50:
		t.Errorf("P50MS = %v, want 50", got.P50MS)
	case got.P95MS != 95:
		t.Errorf("P95MS = %v, want 95", got.P95MS)
	case got.P99MS != 99:
		t.Errorf("P99MS = %v, want 99", got.P99MS)
	}
}

func TestSummarizeOrderIndependent(t *testing.T) {
	sorted := summarize([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond})
	shuffled := summarize([]time.Duration{3 * time.Millisecond, 1 * time.Millisecond, 4 * time.Millisecond, 2 * time.Millisecond})
	if sorted != shuffled {
		t.Fatalf("summarize is order-dependent: sorted-input=%+v shuffled-input=%+v", sorted, shuffled)
	}
}

func TestPercentileClampsRank(t *testing.T) {
	sorted := []time.Duration{1, 2, 3}
	if got := percentile(sorted, 0); got != 1 {
		t.Errorf("percentile(0) = %v, want the minimum (rank clamped to >=1)", got)
	}
	if got := percentile(sorted, 1); got != 3 {
		t.Errorf("percentile(1) = %v, want the maximum (rank clamped to <=len)", got)
	}
	if got := percentile(nil, 0.95); got != 0 {
		t.Errorf("percentile(nil, ...) = %v, want 0", got)
	}
}

func TestMsRoundsToMicrosecondPrecision(t *testing.T) {
	got := ms(1500 * time.Microsecond)
	if got != 1.5 {
		t.Errorf("ms(1500us) = %v, want 1.5", got)
	}
}
