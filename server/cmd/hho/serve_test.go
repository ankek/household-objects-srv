package main

import (
	"context"
	"github.com/ankek/Household-Objects-Dev/server/internal/serverlock"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestServeRefusesWhenServerLockAlreadyHeld(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HHO_DATA_DIR", dataDir)

	lock, err := serverlock.Acquire(dataDir)
	if err != nil {
		t.Fatalf("serverlock.Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lock.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err = serve(context.Background(), "", logger, io.Discard)
	if err == nil {
		t.Fatal("serve = nil, want an error while another process holds the server lock")
	}
	if !strings.Contains(err.Error(), "hho serve") {
		t.Errorf("serve error = %q, want it to name \"hho serve\"", err.Error())
	}
}
