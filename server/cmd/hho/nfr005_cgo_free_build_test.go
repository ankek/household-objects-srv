package main

import (
	"context"
	"debug/buildinfo"
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildsCGOFreeStaticBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("a full binary build+link is slow; skipped under -short")
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go toolchain not on PATH: %v", err)
	}

	binPath := filepath.Join(t.TempDir(), "hho-nfr005-cgo-check")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, goBin, "build", "-o", binPath, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("`CGO_ENABLED=0 go build -o %s .` failed (%v): NFR-005 requires cmd/hho to build "+
			"CGO-free, but this build did not. A cgo-requiring dependency has most likely been "+
			"introduced somewhere in cmd/hho's import graph -- ordinary CI (go-build with ambient "+
			"CGO_ENABLED) would not have caught this, since cgo compiles fine on a runner with a C "+
			"toolchain present.\n--- go build stderr ---\n%s", binPath, err, stderr.String())
	}

	bi, err := buildinfo.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read embedded build info from %s: %v", binPath, err)
	}
	var cgoSetting string
	var haveSetting bool
	for _, s := range bi.Settings {
		if s.Key == "CGO_ENABLED" {
			cgoSetting, haveSetting = s.Value, true
			break
		}
	}
	if !haveSetting {
		t.Fatalf("built binary %s carries no CGO_ENABLED entry in its embedded build settings "+
			"(`go version -m %s` would show this) -- cannot confirm NFR-005 without it", binPath, binPath)
	}
	if cgoSetting != "0" {
		t.Errorf("built binary %s embeds CGO_ENABLED=%q, want \"0\" -- NFR-005 requires a CGO-free "+
			"build; run `go version -m %s` to inspect it directly", binPath, cgoSetting, binPath)
	}

	if runtime.GOOS == "linux" {
		ef, err := elf.Open(binPath)
		if err != nil {
			t.Fatalf("open %s as ELF: %v", binPath, err)
		}
		defer ef.Close()

		if ef.Section(".dynamic") != nil {
			t.Errorf("built binary %s has an ELF .dynamic section -- it is dynamically linked, not "+
				"the single static binary NFR-005 requires", binPath)
		}
		for _, prog := range ef.Progs {
			if prog.Type == elf.PT_INTERP {
				t.Errorf("built binary %s has a PT_INTERP program header, naming a dynamic "+
					"loader/interpreter -- it is not statically linked as NFR-005 requires", binPath)
			}
		}
	} else {
		t.Logf("skipping the ELF static-linkage check on GOOS=%s; the CGO_ENABLED build-info "+
			"assertion above already covers this platform", runtime.GOOS)
	}
}
