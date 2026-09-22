package main

import (
	"math"
	"sort"
	"sync"
	"time"
)

type rssSample struct {
	Available   bool   `json:"available"`
	Unavailable string `json:"unavailable_reason,omitempty"`
	CurrentKB   int64  `json:"current_kb"`
	HighWaterKB int64  `json:"high_water_kb"`
}

type latencyStats struct {
	Count  int     `json:"count"`
	MeanMS float64 `json:"mean_ms"`
	P50MS  float64 `json:"p50_ms"`
	P95MS  float64 `json:"p95_ms"`
	P99MS  float64 `json:"p99_ms"`
	MaxMS  float64 `json:"max_ms"`
}

type samples struct {
	byKind map[string][]time.Duration
}

func newSamples() *samples {
	return &samples{byKind: make(map[string][]time.Duration, 8)}
}

func (s *samples) add(kind string, d time.Duration) {
	s.byKind[kind] = append(s.byKind[kind], d)
}

type collector struct {
	mu  sync.Mutex
	all *samples
}

func newCollector() *collector {
	return &collector{all: newSamples()}
}

func (c *collector) merge(s *samples) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for kind, ds := range s.byKind {
		c.all.byKind[kind] = append(c.all.byKind[kind], ds...)
	}
}

func (c *collector) stats(overallKinds []string) map[string]latencyStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make(map[string]latencyStats, len(c.all.byKind)+1)
	var overall []time.Duration
	inOverall := make(map[string]bool, len(overallKinds))
	for _, k := range overallKinds {
		inOverall[k] = true
	}
	for kind, ds := range c.all.byKind {
		out[kind] = summarize(ds)
		if inOverall[kind] {
			overall = append(overall, ds...)
		}
	}
	if len(overall) > 0 {
		out[kindReadOverall] = summarize(overall)
	}
	return out
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

type rssWatcher struct {
	mu      sync.Mutex
	peak    rssSample
	lastErr error
	stop    chan struct{}
	done    chan struct{}
}

func startRSSWatcher(interval time.Duration) *rssWatcher {
	w := &rssWatcher{stop: make(chan struct{}), done: make(chan struct{})}
	w.observe()
	go func() {
		defer close(w.done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-w.stop:
				w.observe()
				return
			case <-t.C:
				w.observe()
			}
		}
	}()
	return w
}

func (w *rssWatcher) observe() {
	s, err := readRSS()
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.lastErr = err
		return
	}
	if !s.Available {
		w.peak = s
		return
	}
	if s.CurrentKB > w.peak.CurrentKB {
		w.peak.CurrentKB = s.CurrentKB
	}
	if s.HighWaterKB > w.peak.HighWaterKB {
		w.peak.HighWaterKB = s.HighWaterKB
	}
	w.peak.Available = true
}

func (w *rssWatcher) close() (rssSample, error) {
	close(w.stop)
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.peak, w.lastErr
}
