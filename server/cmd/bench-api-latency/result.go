package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const resultSchemaVersion = "hho.bench-api-latency/1"

type RunResult struct {
	List            latencyStats `json:"list"`
	Search          latencyStats `json:"search"`
	Detail          latencyStats `json:"detail"`
	SearchHitsTotal int          `json:"search_hits_total"`
}

type hostInfo struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	NumCPU     int    `json:"num_cpu"`
	GOMAXPROCS int    `json:"gomaxprocs"`
	GoVersion  string `json:"go_version"`
}

type configOut struct {
	Items             int   `json:"items"`
	Runs              int   `json:"runs"`
	WarmupPerClass    int   `json:"warmup_per_class"`
	MeasuredPerClass  int   `json:"measured_per_class"`
	PageSize          int   `json:"page_size"`
	SearchResultLimit int   `json:"search_result_limit"`
	RNGSeed           int64 `json:"rng_seed"`
}

type Result struct {
	SchemaVersion string      `json:"schema_version"`
	StartedAt     string      `json:"started_at"`
	Host          hostInfo    `json:"host"`
	Config        configOut   `json:"config"`
	Corpus        corpusStats `json:"corpus"`
	Runs          []RunResult `json:"runs"`
	P95BudgetMS   float64     `json:"p95_budget_ms_reference_only"`
}

func newHostInfo() hostInfo {
	return hostInfo{
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		NumCPU:     runtime.NumCPU(),
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		GoVersion:  runtime.Version(),
	}
}

func writeResultJSON(path string, res Result) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return nil
}
