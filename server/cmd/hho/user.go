package main

import (
	"fmt"
	"io"
	"os"
)

func runUser(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "hho: \"user\" needs a subcommand (reset-password)")
		return 2
	}

	switch args[0] {
	case "reset-password":
		return runResetPassword(args[1:], stdin, stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "hho: unknown \"user\" subcommand %q\n", args[0])
		return 2
	}
}
