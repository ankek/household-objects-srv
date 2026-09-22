package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const resultSchemaVersion = "hho.spike-sqlite/1"

const (
	nfr001IdleRSSBudgetKB = 50 * 1024
	nfr002P95ReadBudgetMS = 100.0
)

type options struct {
	dbPath      string
	keepDB      bool
	items       int
	locations   int
	labels      int
	seedBatch   int
	duration    time.Duration
	writers     int
	readers     int
	writeRate   float64
	readRate    float64
	pageSize    int
	rngSeed     int64
	readConns   int
	writeConns  int
	rssInterval time.Duration
	buildTiming bool
	ftsTriggers bool
	moduleDir   string
	out         string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "spike-sqlite: FAILED: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseFlags()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res := &result{
		SchemaVersion: resultSchemaVersion,
		Task:          "D0.1 (T001-T003 harness; measurements recorded by T004/T006)",
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
		Host: hostInfo{
			GOOS:       runtime.GOOS,
			GOARCH:     runtime.GOARCH,
			NumCPU:     runtime.NumCPU(),
			GOMAXPROCS: runtime.GOMAXPROCS(0),
			GoVersion:  runtime.Version(),
		},
		NFRReference: nfrReference{
			IdleRSSBudgetKB: nfr001IdleRSSBudgetKB,
			P95ReadBudgetMS: nfr002P95ReadBudgetMS,
			Note: "Reference thresholds only. This harness records measurements; the " +
				"PASS/ESCALATE verdict against NFR-001/NFR-002 is recorded by T006 (assumption A9).",
		},
	}
	res.Driver = readDriverInfo()

	dbPath, cleanup, err := resolveDBPath(opts)
	if err != nil {
		return err
	}
	defer cleanup()
	res.Config = configOut{
		DBPath:            dbPath,
		Items:             opts.items,
		Locations:         opts.locations,
		Labels:            opts.labels,
		SeedBatch:         opts.seedBatch,
		DurationSec:       opts.duration.Seconds(),
		Writers:           opts.writers,
		Readers:           opts.readers,
		WriteRatePerWSec:  opts.writeRate,
		ReadRatePerRSec:   opts.readRate,
		PageSize:          opts.pageSize,
		RNGSeed:           opts.rngSeed,
		ReadConns:         opts.readConns,
		WriteConns:        opts.writeConns,
		RSSIntervalMillis: opts.rssInterval.Milliseconds(),
		FTSTriggers:       opts.ftsTriggers,
	}
	if !opts.ftsTriggers {
		res.RSS.Note = "FTS5 triggers were DISABLED (--fts-triggers=false): search latency and " +
			"fts_consistency in this document are meaningless. Cost-attribution run only."
	}

	writeDB, writeDSN, err := openPool(poolConfig{path: dbPath, maxOpen: opts.writeConns, roleName: "write"})
	if err != nil {
		return err
	}
	defer writeDB.Close()

	readDB, _, err := openPool(poolConfig{path: dbPath, maxOpen: opts.readConns, roleName: "read"})
	if err != nil {
		return err
	}
	defer readDB.Close()

	res.Pragmas.Requested = append([]string(nil), walPragmas...)
	res.Pragmas.TxLock = txLock
	res.Pragmas.DSN = redactPath(writeDSN, dbPath)

	if err := assertFTS5(ctx, writeDB); err != nil {
		return err
	}

	schemaStart := time.Now()
	if _, err := writeDB.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if opts.ftsTriggers {
		if _, err := writeDB.ExecContext(ctx, ftsTriggerDDL); err != nil {
			return fmt.Errorf("create FTS5 triggers: %w", err)
		}
	}
	res.Timings.SchemaSec = time.Since(schemaStart).Seconds()

	if res.Pragmas.InForceWrite, err = readPragmas(ctx, writeDB); err != nil {
		return err
	}
	if res.Pragmas.InForceRead, err = readPragmas(ctx, readDB); err != nil {
		return err
	}
	if jm := res.Pragmas.InForceWrite["journal_mode"]; !strings.EqualFold(jm, "wal") {
		return fmt.Errorf("journal_mode is %q, not WAL — the measurement would not describe the intended configuration", jm)
	}

	idle, err := readRSS()
	if err != nil {
		return fmt.Errorf("read idle RSS: %w", err)
	}
	res.RSS.Idle = idle
	runtime.GC()
	debug.FreeOSMemory()
	idleAfterGC, err := readRSS()
	if err != nil {
		return fmt.Errorf("read idle RSS after GC: %w", err)
	}
	res.RSS.IdleAfterGC = idleAfterGC

	watcher := startRSSWatcher(opts.rssInterval)

	gen := newUUIDGen(opts.rngSeed)
	ix := &itemIndex{}
	var shortCodes atomic.Int64
	groupID := gen.nextAt(seedBaseMillis)

	seedStat, err := seed(ctx, writeDB, gen, ix, seedConfig{
		groupID:    groupID,
		items:      opts.items,
		locations:  opts.locations,
		labels:     opts.labels,
		batchSize:  opts.seedBatch,
		rngSeed:    opts.rngSeed,
		shortCodes: &shortCodes,
	})
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	res.Seed = *seedStat
	res.Timings.SeedSec = seedStat.DurationSec

	if seeded, err := readRSS(); err == nil {
		res.RSS.AfterSeed = seeded
	} else {
		return fmt.Errorf("read RSS after seed: %w", err)
	}

	coll := newCollector()
	workloadStart := time.Now()
	wl, err := runWorkload(ctx, writeDB, readDB, gen, ix, coll, workloadConfig{
		groupID:   groupID,
		duration:  opts.duration,
		writers:   opts.writers,
		readers:   opts.readers,
		writeRate: opts.writeRate,
		readRate:  opts.readRate,
		pageSize:  opts.pageSize,
		rngSeed:   opts.rngSeed,
	})
	if err != nil {
		return fmt.Errorf("workload: %w", err)
	}
	res.Timings.WorkloadSec = time.Since(workloadStart).Seconds()
	res.Workload = *wl
	res.Latency = coll.stats(readKinds)

	peak, watchErr := watcher.close()
	if watchErr != nil {
		return fmt.Errorf("RSS sampling: %w", watchErr)
	}
	res.RSS.LoadedPeak = peak
	final, err := readRSS()
	if err != nil {
		return fmt.Errorf("read final RSS: %w", err)
	}
	res.RSS.Final = final

	runtime.GC()
	debug.FreeOSMemory()
	settled, err := readRSS()
	if err != nil {
		return fmt.Errorf("read settled RSS: %w", err)
	}
	res.RSS.AfterSettle = settled

	if res.FTS, err = checkFTSConsistency(ctx, readDB); err != nil {
		return err
	}
	res.DBSize = measureDBSize(dbPath)
	res.ItemsAtEnd = int64(ix.size())

	if opts.buildTiming {
		moduleDir := opts.moduleDir
		if moduleDir == "" {
			exe, exeErr := os.Executable()
			base := "."
			if exeErr == nil {
				base = filepath.Dir(exe)
			}
			if md, mdErr := findModuleDir(base); mdErr == nil {
				moduleDir = md
			} else if md, mdErr := findModuleDir("."); mdErr == nil {
				moduleDir = md
			}
		}
		if moduleDir == "" {
			res.Build = buildTiming{SkipReason: "could not locate the go.mod directory; pass --module-dir"}
		} else {
			res.Build = measureBuild(ctx, moduleDir, "./cmd/spike-sqlite")
		}
	} else {
		res.Build = buildTiming{SkipReason: "disabled via --build-timing=false"}
	}

	printSummary(res)

	if opts.out != "" {
		if err := writeJSON(opts.out, res); err != nil {
			return err
		}
		fmt.Printf("\nresults written to %s\n", opts.out)
	}
	return nil
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.dbPath, "db", "", "path to the spike database file (default: a fresh temp directory, removed on exit)")
	flag.BoolVar(&o.keepDB, "keep-db", false, "keep the temp database directory after the run")
	flag.IntVar(&o.items, "items", 10000, "number of items to seed (D0.1 specifies 10,000)")
	flag.IntVar(&o.locations, "locations", 60, "number of locations to seed")
	flag.IntVar(&o.labels, "labels", 25, "number of labels to seed")
	flag.IntVar(&o.seedBatch, "seed-batch", 500, "items per seeding transaction")
	flag.DurationVar(&o.duration, "duration", 30*time.Second, "duration of the concurrent workload phase")
	flag.IntVar(&o.writers, "writers", 10, "concurrent writer goroutines (D0.1 specifies 10)")
	flag.IntVar(&o.readers, "readers", 8, "concurrent reader goroutines running the read mix")
	flag.Float64Var(&o.writeRate, "write-rate", 5, "target writes/sec per writer; 0 = unthrottled (saturation)")
	flag.Float64Var(&o.readRate, "read-rate", 20, "target reads/sec per reader; 0 = unthrottled (saturation)")
	flag.IntVar(&o.pageSize, "page-size", 25, "rows per list/pagination query")
	flag.Int64Var(&o.rngSeed, "rng-seed", 1, "RNG seed; identical seeds produce identical seeded datasets")
	flag.IntVar(&o.readConns, "read-conns", 4, "max open connections in the read pool")
	flag.IntVar(&o.writeConns, "write-conns", 1, "max open connections in the write pool (SQLite admits one writer)")
	flag.DurationVar(&o.rssInterval, "rss-interval", 50*time.Millisecond, "RSS sampling interval")
	flag.BoolVar(&o.ftsTriggers, "fts-triggers", true,
		"maintain the FTS5 index via triggers; false attributes cost only and invalidates search/consistency figures")
	flag.BoolVar(&o.buildTiming, "build-timing", true, "measure cold and warm build time in a private GOCACHE")
	flag.StringVar(&o.moduleDir, "module-dir", "", "directory containing server/go.mod (default: auto-detect)")
	flag.StringVar(&o.out, "out", "", "write the JSON result document to this path")
	flag.Parse()
	return o
}

func resolveDBPath(o options) (string, func(), error) {
	if o.dbPath != "" {
		if err := os.MkdirAll(filepath.Dir(o.dbPath), 0o755); err != nil {
			return "", func() {}, fmt.Errorf("create db directory: %w", err)
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if _, err := os.Stat(o.dbPath + suffix); err == nil {
				return "", func() {}, fmt.Errorf("%s already exists; remove it or choose another --db path", o.dbPath+suffix)
			}
		}
		return o.dbPath, func() {}, nil
	}

	dir, err := os.MkdirTemp("", "hho-spike-sqlite-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp db directory: %w", err)
	}
	path := filepath.Join(dir, "hho-spike.db")
	cleanup := func() {
		if o.keepDB {
			fmt.Printf("spike database kept at %s\n", dir)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", dir, err)
		}
	}
	return path, cleanup, nil
}

func readDriverInfo() driverInfo {
	d := driverInfo{Module: "modernc.org/sqlite", Version: "unknown", CGOEnabled: "0 (pure Go)"}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return d
	}
	for _, dep := range info.Deps {
		if dep == nil {
			continue
		}
		if dep.Path == "modernc.org/sqlite" {
			d.Version = dep.Version
			if dep.Replace != nil {
				d.Version = dep.Replace.Version + " (replaced)"
			}
		}
		if dep.Path == "modernc.org/libc" {
			d.LibcVersion = dep.Version
		}
	}
	return d
}

func redactPath(dsn, path string) string {
	return strings.Replace(dsn, path, "<db-path>", 1)
}

func checkFTSConsistency(ctx context.Context, db *sql.DB) (ftsCheck, error) {
	var c ftsCheck
	err := db.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM items WHERE deleted_at IS NULL),
		       (SELECT count(*) FROM items_fts)`).Scan(&c.LiveItems, &c.FTSRows)
	if err != nil {
		return c, fmt.Errorf("fts consistency counts: %w", err)
	}
	c.Delta = c.FTSRows - c.LiveItems
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM items_fts WHERE items_fts MATCH 'drill OR kettle OR lamp'`).Scan(&c.SampleMatchRows); err != nil {
		return c, fmt.Errorf("fts sample match: %w", err)
	}
	c.Note = "A non-zero delta is a finding for T005 (FTS5 correctness under concurrent writes), not a harness error."
	return c, nil
}

func measureDBSize(path string) dbSizeInfo {
	var d dbSizeInfo
	for _, s := range []struct {
		suffix string
		dst    *int64
	}{{"", &d.MainBytes}, {"-wal", &d.WALBytes}, {"-shm", &d.SHMBytes}} {
		if fi, err := os.Stat(path + s.suffix); err == nil {
			*s.dst = fi.Size()
		}
	}
	d.TotalBytes = d.MainBytes + d.WALBytes + d.SHMBytes
	return d
}

func writeJSON(path string, res *result) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	return nil
}
