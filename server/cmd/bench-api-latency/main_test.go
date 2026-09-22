package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRunBenchmarkEndToEnd(t *testing.T) {
	opts := options{
		items:       30,
		runs:        1,
		warmup:      1,
		measure:     5,
		pageSize:    10,
		searchLimit: 10,
		seed:        99,
		out:         filepath.Join(t.TempDir(), "result.json"),
	}

	res, err := runBenchmark(context.Background(), opts)
	if err != nil {
		t.Fatalf("runBenchmark: %v", err)
	}

	if res.Corpus.TotalItems != opts.items {
		t.Errorf("Corpus.TotalItems = %d, want %d", res.Corpus.TotalItems, opts.items)
	}
	if res.Corpus.DistinctNames == 0 {
		t.Errorf("Corpus.DistinctNames = 0, want > 0 -- the seeded corpus looks degenerate")
	}
	if len(res.Runs) != opts.runs {
		t.Fatalf("len(res.Runs) = %d, want %d", len(res.Runs), opts.runs)
	}

	rr := res.Runs[0]
	for _, tc := range []struct {
		name string
		s    latencyStats
	}{
		{"list", rr.List},
		{"search", rr.Search},
		{"detail", rr.Detail},
	} {
		if tc.s.Count != opts.measure {
			t.Errorf("%s.Count = %d, want %d", tc.name, tc.s.Count, opts.measure)
		}
		if tc.s.P95MS <= 0 {
			t.Errorf("%s.P95MS = %v, want > 0", tc.name, tc.s.P95MS)
		}
		if tc.s.MaxMS < tc.s.MinMS {
			t.Errorf("%s: MaxMS (%v) < MinMS (%v)", tc.name, tc.s.MaxMS, tc.s.MinMS)
		}
	}
	if rr.SearchHitsTotal <= 0 {
		t.Errorf("SearchHitsTotal = %d, want > 0 -- search requests matched nothing", rr.SearchHitsTotal)
	}

	if err := writeResultJSON(opts.out, res); err != nil {
		t.Fatalf("writeResultJSON: %v", err)
	}
}
