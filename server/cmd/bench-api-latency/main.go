package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const nfr002P95BudgetMS = 100.0

type options struct {
	items       int
	runs        int
	warmup      int
	measure     int
	pageSize    int
	searchLimit int
	seed        int64
	out         string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "bench-api-latency: FAILED: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var o options
	flag.IntVar(&o.items, "items", 5000, "number of items to seed (NFR-002 specifies 5,000)")
	flag.IntVar(&o.runs, "runs", 3, "independent measurement rounds against the same seeded corpus (>=3 for a stable p95, per this task's brief)")
	flag.IntVar(&o.warmup, "warmup", 200, "discarded warm-up requests per endpoint class, per round")
	flag.IntVar(&o.measure, "measure", 1000, "measured requests per endpoint class, per round")
	flag.IntVar(&o.pageSize, "page-size", 50, "rows per list page (matches listItems' own default limit)")
	flag.IntVar(&o.searchLimit, "search-limit", 25, "limit applied to search requests")
	flag.Int64Var(&o.seed, "seed", 1, "RNG seed; identical seeds reproduce an identical corpus and an identical per-round request sequence")
	flag.StringVar(&o.out, "out", "", "write the JSON result document to this path (optional)")
	flag.Parse()
	return o
}

func run() error {
	opts := parseFlags()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res, err := runBenchmark(ctx, opts)
	if err != nil {
		return err
	}

	if opts.out != "" {
		if err := writeResultJSON(opts.out, res); err != nil {
			return err
		}
		fmt.Printf("bench-api-latency: results written to %s\n", opts.out)
	}
	return nil
}

func runBenchmark(ctx context.Context, opts options) (Result, error) {
	res := Result{
		SchemaVersion: resultSchemaVersion,
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
		Host:          newHostInfo(),
		Config: configOut{
			Items:             opts.items,
			Runs:              opts.runs,
			WarmupPerClass:    opts.warmup,
			MeasuredPerClass:  opts.measure,
			PageSize:          opts.pageSize,
			SearchResultLimit: opts.searchLimit,
			RNGSeed:           opts.seed,
		},
		P95BudgetMS: nfr002P95BudgetMS,
	}

	fmt.Printf("bench-api-latency: building real server (httpapi.NewRouter over a real loopback socket)\n")
	srv, err := newBenchServer(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("build server: %w", err)
	}
	defer srv.Close()

	client, groupID, err := registerAndLogin(ctx, srv.httpServer.URL)
	if err != nil {
		return Result{}, fmt.Errorf("register+login: %w", err)
	}

	gid, err := storage.NewGroupID(groupID)
	if err != nil {
		return Result{}, fmt.Errorf("parse group id %q: %w", groupID, err)
	}
	scope, err := srv.store.ForGroup(gid)
	if err != nil {
		return Result{}, fmt.Errorf("bind group scope: %w", err)
	}

	fmt.Printf("bench-api-latency: seeding %d items (seed=%d)\n", opts.items, opts.seed)
	seedStart := time.Now()
	ids, corpus, err := seedCorpus(ctx, scope.Items(), opts.items, opts.seed)
	if err != nil {
		return Result{}, fmt.Errorf("seed corpus: %w", err)
	}
	res.Corpus = corpus
	if corpus.TotalItems != opts.items {
		return Result{}, fmt.Errorf("seeded %d items, want exactly %d -- the corpus is not the size NFR-002 asks for", corpus.TotalItems, opts.items)
	}
	fmt.Printf("bench-api-latency: seeded %d items (%d distinct names) in %s\n",
		corpus.TotalItems, corpus.DistinctNames, time.Since(seedStart).Round(time.Millisecond))

	for round := range opts.runs {
		cfg := roundConfig{
			pageSize:    opts.pageSize,
			searchLimit: opts.searchLimit,
			totalItems:  opts.items,
			warmup:      opts.warmup,
			measure:     opts.measure,
			rngSeed:     opts.seed + int64(round) + 1,
		}
		fmt.Printf("bench-api-latency: round %d/%d (warmup=%d, measured=%d per class)\n", round+1, opts.runs, opts.warmup, opts.measure)
		roundStart := time.Now()
		rr, err := runRound(ctx, client, srv.httpServer.URL, ids, searchTerms, cfg)
		if err != nil {
			return Result{}, fmt.Errorf("round %d: %w", round+1, err)
		}
		fmt.Printf("bench-api-latency: round %d done in %s\n", round+1, time.Since(roundStart).Round(time.Millisecond))
		printRunResult(round+1, rr)
		res.Runs = append(res.Runs, rr)
	}

	return res, nil
}

func printRunResult(round int, rr RunResult) {
	print1 := func(label string, s latencyStats) {
		fmt.Printf("  round %d %-8s: n=%-5d min=%7.3fms p50=%7.3fms p95=%7.3fms p99=%7.3fms max=%7.3fms\n",
			round, label, s.Count, s.MinMS, s.P50MS, s.P95MS, s.P99MS, s.MaxMS)
	}
	print1("list", rr.List)
	print1("search", rr.Search)
	print1("detail", rr.Detail)
	fmt.Printf("  round %d search_hits_total=%d (0 here would mean search never hit anything real)\n",
		round, rr.SearchHitsTotal)
}
