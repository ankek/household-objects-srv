//go:build !linux

package main

import "runtime"

func readRSS() (rssSample, error) {
	return rssSample{
		Available:   false,
		Unavailable: "VmRSS is only read on linux; this build ran on " + runtime.GOOS,
	}, nil
}
