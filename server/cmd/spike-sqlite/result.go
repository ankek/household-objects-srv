package main

import (
	"fmt"
	"sort"
)

type result struct {
	SchemaVersion string                  `json:"schema_version"`
	Task          string                  `json:"task"`
	StartedAt     string                  `json:"started_at"`
	Host          hostInfo                `json:"host"`
	Driver        driverInfo              `json:"driver"`
	Config        configOut               `json:"config"`
	Pragmas       pragmaReport            `json:"pragmas"`
	Timings       timings                 `json:"timings"`
	Seed          seedStats               `json:"seed"`
	Workload      workloadStats           `json:"workload"`
	ItemsAtEnd    int64                   `json:"items_at_end"`
	RSS           rssReport               `json:"rss"`
	Latency       map[string]latencyStats `json:"latency"`
	FTS           ftsCheck                `json:"fts_consistency"`
	DBSize        dbSizeInfo              `json:"db_size"`
	Build         buildTiming             `json:"build"`
	NFRReference  nfrReference            `json:"nfr_reference"`
}

type hostInfo struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	NumCPU     int    `json:"num_cpu"`
	GOMAXPROCS int    `json:"gomaxprocs"`
	GoVersion  string `json:"go_version"`
}

type driverInfo struct {
	Module      string `json:"module"`
	Version     string `json:"version"`
	LibcVersion string `json:"libc_version,omitempty"`
	CGOEnabled  string `json:"cgo"`
}

type configOut struct {
	DBPath            string  `json:"db_path"`
	Items             int     `json:"items"`
	Locations         int     `json:"locations"`
	Labels            int     `json:"labels"`
	SeedBatch         int     `json:"seed_batch"`
	DurationSec       float64 `json:"workload_duration_sec"`
	Writers           int     `json:"writers"`
	Readers           int     `json:"readers"`
	WriteRatePerWSec  float64 `json:"write_rate_per_writer_sec"`
	ReadRatePerRSec   float64 `json:"read_rate_per_reader_sec"`
	PageSize          int     `json:"page_size"`
	RNGSeed           int64   `json:"rng_seed"`
	ReadConns         int     `json:"read_conns"`
	WriteConns        int     `json:"write_conns"`
	RSSIntervalMillis int64   `json:"rss_sample_interval_ms"`
	FTSTriggers       bool    `json:"fts_triggers_enabled"`
}

type pragmaReport struct {
	Requested    []string          `json:"requested"`
	TxLock       string            `json:"txlock"`
	DSN          string            `json:"dsn"`
	InForceWrite map[string]string `json:"in_force_write_pool"`
	InForceRead  map[string]string `json:"in_force_read_pool"`
}

type timings struct {
	SchemaSec   float64 `json:"schema_sec"`
	SeedSec     float64 `json:"seed_sec"`
	WorkloadSec float64 `json:"workload_sec"`
}

type rssReport struct {
	Idle        rssSample `json:"idle"`
	IdleAfterGC rssSample `json:"idle_after_gc"`
	AfterSeed   rssSample `json:"after_seed"`
	LoadedPeak  rssSample `json:"loaded_peak"`
	Final       rssSample `json:"final"`
	AfterSettle rssSample `json:"after_settle"`
	Note        string    `json:"note"`
}

type ftsCheck struct {
	LiveItems       int64  `json:"live_items"`
	FTSRows         int64  `json:"fts_rows"`
	Delta           int64  `json:"delta"`
	SampleMatchRows int64  `json:"sample_match_rows"`
	Note            string `json:"note"`
}

type dbSizeInfo struct {
	MainBytes  int64 `json:"main_bytes"`
	WALBytes   int64 `json:"wal_bytes"`
	SHMBytes   int64 `json:"shm_bytes"`
	TotalBytes int64 `json:"total_bytes"`
}

type nfrReference struct {
	IdleRSSBudgetKB int64   `json:"nfr_001_idle_rss_budget_kb"`
	P95ReadBudgetMS float64 `json:"nfr_002_p95_read_budget_ms"`
	Note            string  `json:"note"`
}

func printSummary(r *result) {
	fmt.Println()
	fmt.Println("=== HHO Phase-0 SQLite driver spike (D0.1) ===")
	fmt.Printf("driver: %s %s (libc %s, %s)\n", r.Driver.Module, r.Driver.Version, r.Driver.LibcVersion, r.Driver.CGOEnabled)
	fmt.Printf("host:   %s/%s, %d CPU, %s\n", r.Host.GOOS, r.Host.GOARCH, r.Host.NumCPU, r.Host.GoVersion)
	fmt.Printf("load:   %d items seeded, %d writers @ %.0f/s, %d readers @ %.0f/s, %.0fs\n",
		r.Config.Items, r.Config.Writers, r.Config.WriteRatePerWSec,
		r.Config.Readers, r.Config.ReadRatePerRSec, r.Config.DurationSec)

	fmt.Println("\n-- WAL settings in force (read back from the database) --")
	printPragmas(r.Pragmas.InForceWrite)

	fmt.Println("\n-- Resident set size --")
	fmt.Printf("  idle (after open+schema, before data): %s\n", rssLine(r.RSS.Idle))
	fmt.Printf("  idle after forced GC:                  %s\n", rssLine(r.RSS.IdleAfterGC))
	fmt.Printf("  after seeding %6d items:            %s\n", r.Config.Items, rssLine(r.RSS.AfterSeed))
	fmt.Printf("  loaded peak during workload:           %s\n", rssLine(r.RSS.LoadedPeak))
	fmt.Printf("  final:                                 %s\n", rssLine(r.RSS.Final))
	fmt.Printf("  after settle (GC + FreeOSMemory):      %s\n", rssLine(r.RSS.AfterSettle))

	fmt.Println("\n-- Latency (ms) --")
	fmt.Printf("  %-14s %8s %8s %8s %8s %8s\n", "operation", "count", "mean", "p50", "p95", "p99")
	for _, kind := range sortedKeys(r.Latency) {
		s := r.Latency[kind]
		fmt.Printf("  %-14s %8d %8.2f %8.2f %8.2f %8.2f\n", kind, s.Count, s.MeanMS, s.P50MS, s.P95MS, s.P99MS)
	}

	fmt.Println("\n-- Throughput / integrity --")
	fmt.Printf("  reads=%d writes=%d inserted=%d soft_deleted=%d detail_misses=%d\n",
		r.Workload.Reads, r.Workload.Writes, r.Workload.ItemsInserted,
		r.Workload.ItemsSoftDeleted, r.Workload.DetailMisses)
	fmt.Printf("  fts: live_items=%d fts_rows=%d delta=%d sample_match_rows=%d\n",
		r.FTS.LiveItems, r.FTS.FTSRows, r.FTS.Delta, r.FTS.SampleMatchRows)
	fmt.Printf("  db: main=%s wal=%s shm=%s\n",
		bytesHuman(r.DBSize.MainBytes), bytesHuman(r.DBSize.WALBytes), bytesHuman(r.DBSize.SHMBytes))
	fmt.Printf("  seed: %.2fs (%.0f items/s), schema: %.3fs\n",
		r.Timings.SeedSec, r.Seed.ItemsPerSec, r.Timings.SchemaSec)

	fmt.Println("\n-- Build --")
	if r.Build.Measured {
		fmt.Printf("  cold=%.1fs warm=%.1fs binary=%s cgo=%s (%s)\n",
			r.Build.ColdSec, r.Build.WarmSec, bytesHuman(r.Build.BinaryBytes), r.Build.CGOEnabled, r.Build.GoVersion)
	} else {
		fmt.Printf("  not measured: %s\n", r.Build.SkipReason)
	}

	fmt.Println("\n-- Machine-readable summary --")
	fmt.Printf("SPIKE_SCHEMA_VERSION=%s\n", r.SchemaVersion)
	fmt.Printf("SPIKE_DRIVER=%s@%s\n", r.Driver.Module, r.Driver.Version)
	fmt.Printf("SPIKE_JOURNAL_MODE=%s\n", r.Pragmas.InForceWrite["journal_mode"])
	fmt.Printf("SPIKE_SYNCHRONOUS=%s\n", r.Pragmas.InForceWrite["synchronous"])
	fmt.Printf("SPIKE_BUSY_TIMEOUT=%s\n", r.Pragmas.InForceWrite["busy_timeout"])
	fmt.Printf("SPIKE_FOREIGN_KEYS=%s\n", r.Pragmas.InForceWrite["foreign_keys"])
	fmt.Printf("SPIKE_IDLE_RSS_KB=%d\n", r.RSS.Idle.CurrentKB)
	fmt.Printf("SPIKE_IDLE_RSS_AFTER_GC_KB=%d\n", r.RSS.IdleAfterGC.CurrentKB)
	fmt.Printf("SPIKE_LOADED_RSS_PEAK_KB=%d\n", r.RSS.LoadedPeak.CurrentKB)
	fmt.Printf("SPIKE_SETTLED_RSS_KB=%d\n", r.RSS.AfterSettle.CurrentKB)
	fmt.Printf("SPIKE_RSS_HIGH_WATER_KB=%d\n", r.RSS.Final.HighWaterKB)
	fmt.Printf("SPIKE_FTS_TRIGGERS=%t\n", r.Config.FTSTriggers)
	for _, kind := range sortedKeys(r.Latency) {
		fmt.Printf("SPIKE_P95_MS_%s=%.3f\n", upperKey(kind), r.Latency[kind].P95MS)
	}
	fmt.Printf("SPIKE_READS=%d\n", r.Workload.Reads)
	fmt.Printf("SPIKE_WRITES=%d\n", r.Workload.Writes)
	fmt.Printf("SPIKE_FTS_DELTA=%d\n", r.FTS.Delta)
	fmt.Printf("SPIKE_DB_MAIN_BYTES=%d\n", r.DBSize.MainBytes)
	fmt.Printf("SPIKE_BUILD_COLD_SEC=%.2f\n", r.Build.ColdSec)
	fmt.Printf("SPIKE_BUILD_WARM_SEC=%.2f\n", r.Build.WarmSec)
	fmt.Printf("SPIKE_BINARY_BYTES=%d\n", r.Build.BinaryBytes)
	fmt.Printf("SPIKE_NFR001_BUDGET_KB=%d\n", r.NFRReference.IdleRSSBudgetKB)
	fmt.Printf("SPIKE_NFR002_BUDGET_MS=%.0f\n", r.NFRReference.P95ReadBudgetMS)

	fmt.Println("\nNOTE: these are process-level numbers for a measurement harness, not the")
	fmt.Println("      whole-container idle RSS that NFR-001 gates (that gate is D1.14/T0xx).")
	fmt.Println("      This harness records no verdict; T006 owns the PASS/ESCALATE decision (A9).")
}

func printPragmas(in map[string]string) {
	if len(in) == 0 {
		fmt.Println("  (none read back)")
		return
	}
	for _, k := range sortedKeys(in) {
		fmt.Printf("  %-20s %s\n", k, in[k])
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func upperKey(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c-32)
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

func rssLine(s rssSample) string {
	if !s.Available {
		if s.Unavailable != "" {
			return "unavailable (" + s.Unavailable + ")"
		}
		return "unavailable"
	}
	return fmt.Sprintf("%7.1f MB  (%d kB, high-water %d kB)",
		float64(s.CurrentKB)/1024, s.CurrentKB, s.HighWaterKB)
}

func bytesHuman(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
