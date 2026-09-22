package main

import (
	"math"
	"sort"
	"time"
)

type latencyStats struct {
	Count  int     `json:"count"`
	MeanMS float64 `json:"mean_ms"`
	MinMS  float64 `json:"min_ms"`
	P50MS  float64 `json:"p50_ms"`
	P95MS  float64 `json:"p95_ms"`
	P99MS  float64 `json:"p99_ms"`
	MaxMS  float64 `json:"max_ms"`
}

func summarize(ds []time.Duration) latencyStats {
	if len(ds) == 0 {
		return latencyStats{}
	}
	sorted := make([]time.Duration, len(ds))
	copy(sorted, ds)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	return latencyStats{
		Count:  len(sorted),
		MeanMS: ms(total / time.Duration(len(sorted))),
		MinMS:  ms(sorted[0]),
		P50MS:  ms(percentile(sorted, 0.50)),
		P95MS:  ms(percentile(sorted, 0.95)),
		P99MS:  ms(percentile(sorted, 0.99)),
		MaxMS:  ms(sorted[len(sorted)-1]),
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func ms(d time.Duration) float64 {
	return math.Round(float64(d.Nanoseconds())/1e3) / 1e3
}
