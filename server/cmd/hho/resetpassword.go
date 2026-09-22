package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/config"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"golang.org/x/term"
	"io"
	"os"
	"strings"
	"time"
)

const (
	minResetPasswordLen = 8
	maxResetPasswordLen = 256
)

type resetPasswordDeps struct {
	now          func() int64
	readPassword func(stdin *os.File, prompt io.Writer, isTerminal func(fd uintptr) bool) ([]byte, error)
	isTerminal   func(fd uintptr) bool
}

func productionResetPasswordDeps() resetPasswordDeps {
	return resetPasswordDeps{
		now:          func() int64 { return time.Now().UnixMilli() },
		readPassword: readPassword,
		isTerminal:   func(fd uintptr) bool { return term.IsTerminal(int(fd)) },
	}
}

func runResetPassword(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	return runResetPasswordWithDeps(args, stdin, stdout, stderr, productionResetPasswordDeps())
}

func runResetPasswordWithDeps(args []string, stdin *os.File, stdout, stderr io.Writer, deps resetPasswordDeps) int {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	fs.SetOutput(stderr)
	username := fs.String("username", "", "the account's username (required)")
	dataDir := fs.String("data-dir", "", "override the data directory (default: $HHO_DATA_DIR, or /data)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: hho user reset-password --username <name>")
		_, _ = fmt.Fprintln(stderr, "")
		_, _ = fmt.Fprintln(stderr, "The new password is never a flag or a positional argument. It is read from the")
		_, _ = fmt.Fprintln(stderr, "controlling terminal without echo (with confirmation) when stdin is a tty, or")
		_, _ = fmt.Fprintln(stderr, "as a single line from stdin otherwise, e.g.:")
		_, _ = fmt.Fprintf(stderr, "    printf '%%s' \"$NEW_PASSWORD\" | hho user reset-password --username alice\n")
		_, _ = fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "hho: unexpected extra argument(s) %v -- the password is never a positional argument\n", fs.Args())
		fs.Usage()
		return 2
	}

	name := strings.TrimSpace(*username)
	if name == "" {
		_, _ = fmt.Fprintln(stderr, "hho: --username is required")
		fs.Usage()
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho: %v\n", err)
		return 1
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	dbPath := cfg.DatabasePath()

	password, err := deps.readPassword(stdin, stderr, deps.isTerminal)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho: %v\n", err)
		return 1
	}
	defer auth.Zero(password)

	if l := len(password); l < minResetPasswordLen || l > maxResetPasswordLen {
		_, _ = fmt.Fprintf(stderr, "hho: password must be %d-%d bytes, got %d\n", minResetPasswordLen, maxResetPasswordLen, l)
		return 1
	}

	ctx := context.Background()

	store, err := storage.Open(ctx, storage.Config{Path: dbPath})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho: open database at %s: %v\n", dbPath, err)
		return 1
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			_, _ = fmt.Fprintf(stderr, "hho: close database: %v\n", cerr)
		}
	}()

	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho: %v\n", err)
		return 1
	}
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho: hash password: %v\n", err)
		return 1
	}

	result, err := store.ResetPassword(ctx, name, hash, deps.now())
	switch {
	case errors.Is(err, storage.ErrUserNotFound):
		_, _ = fmt.Fprintf(stderr, "hho: no such user %q\n", name)
		return 1
	case isDatabaseLocked(err):
		_, _ = fmt.Fprintf(stderr, "hho: the database is locked -- is \"hho serve\" already running against %s? the write timed out waiting for it; try again: %v\n", dbPath, err)
		return 1
	case err != nil:
		_, _ = fmt.Fprintf(stderr, "hho: reset password: %v\n", err)
		return 1
	}

	if _, err := fmt.Fprintf(stdout, "Password reset for user %q (group %s). Revoked %d session(s) and %d device token(s).\n",
		name, result.GroupID, result.SessionsRevoked, result.DeviceTokensRevoked); err != nil {
		return 1
	}
	return 0
}
