package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if _, err := fmt.Fprintf(stdout, "hho %s (%s)\n", version, buildRevision()); err != nil {
			return 1
		}
		return 0
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "healthcheck":
		return runHealthcheck(args[1:], stdout, stderr)
	case "routes":
		return runRoutes(args[1:], stdout, stderr)
	case "backup":
		return runBackup(args[1:], stdout, stderr)
	case "restore":
		return runRestore(args[1:], stdin, stdout, stderr)
	case "user":
		return runUser(args[1:], stdin, stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "hho: unknown command %q\n", args[0])
		return 2
	}
}

func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return "unknown"
}
