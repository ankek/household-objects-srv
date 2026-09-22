package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type buildTiming struct {
	Measured     bool    `json:"measured"`
	SkipReason   string  `json:"skip_reason,omitempty"`
	ColdSec      float64 `json:"cold_build_sec"`
	WarmSec      float64 `json:"warm_build_sec"`
	BinaryBytes  int64   `json:"binary_bytes"`
	GoVersion    string  `json:"go_version,omitempty"`
	CGOEnabled   string  `json:"cgo_enabled"`
	PrivateCache bool    `json:"private_build_cache"`
}

func measureBuild(ctx context.Context, moduleDir, pkg string) buildTiming {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return buildTiming{SkipReason: "go toolchain not on PATH: " + err.Error()}
	}

	tmp, err := os.MkdirTemp("", "hho-spike-build-")
	if err != nil {
		return buildTiming{SkipReason: "create temp build dir: " + err.Error()}
	}
	defer os.RemoveAll(tmp)

	cacheDir := filepath.Join(tmp, "gocache")
	outBin := filepath.Join(tmp, "spike-sqlite")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return buildTiming{SkipReason: "create temp GOCACHE: " + err.Error()}
	}

	run := func() (time.Duration, error) {
		cmd := exec.CommandContext(ctx, goBin, "build", "-o", outBin, pkg)
		cmd.Dir = moduleDir
		cmd.Env = append(os.Environ(),
			"GOCACHE="+cacheDir,
			"CGO_ENABLED=0",
			"GOFLAGS=",
		)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		started := time.Now()
		if err := cmd.Run(); err != nil {
			return 0, fmt.Errorf("go build failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return time.Since(started), nil
	}

	cold, err := run()
	if err != nil {
		return buildTiming{SkipReason: err.Error()}
	}
	warm, err := run()
	if err != nil {
		return buildTiming{SkipReason: err.Error()}
	}

	t := buildTiming{
		Measured:     true,
		ColdSec:      cold.Seconds(),
		WarmSec:      warm.Seconds(),
		CGOEnabled:   "0",
		PrivateCache: true,
	}
	if fi, err := os.Stat(outBin); err == nil {
		t.BinaryBytes = fi.Size()
	}
	if out, err := exec.CommandContext(ctx, goBin, "version").Output(); err == nil {
		t.GoVersion = strings.TrimSpace(string(out))
	}
	return t
}

func findModuleDir(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found at or above %s", start)
		}
		dir = parent
	}
}
