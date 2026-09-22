//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func readRSS() (rss rssSample, err error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return rssSample{}, fmt.Errorf("open /proc/self/status: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	var sawRSS, sawHWM bool
	for sc.Scan() {
		line := sc.Text()
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch key {
		case "VmRSS":
			kb, perr := parseKB(value)
			if perr != nil {
				return rssSample{}, fmt.Errorf("parse VmRSS %q: %w", value, perr)
			}
			rss.CurrentKB, sawRSS = kb, true
		case "VmHWM":
			kb, perr := parseKB(value)
			if perr != nil {
				return rssSample{}, fmt.Errorf("parse VmHWM %q: %w", value, perr)
			}
			rss.HighWaterKB, sawHWM = kb, true
		}
		if sawRSS && sawHWM {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return rssSample{}, fmt.Errorf("scan /proc/self/status: %w", err)
	}
	if !sawRSS {
		return rssSample{}, fmt.Errorf("VmRSS not present in /proc/self/status")
	}
	rss.Available = true
	return rss, nil
}

func parseKB(value string) (int64, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty value")
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, err
	}
	if len(fields) > 1 && !strings.EqualFold(fields[1], "kB") {
		return 0, fmt.Errorf("unexpected unit %q", fields[1])
	}
	return n, nil
}
