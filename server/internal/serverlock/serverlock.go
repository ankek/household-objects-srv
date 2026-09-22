package serverlock

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const FileName = ".hho-serve.lock"

var errLocked = errors.New("serverlock: already locked")

var ErrUnsupported = errors.New("serverlock: advisory locking is not available for this data directory")

type HeldError struct {
	Path string
	PID  int
}

func (e *HeldError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("%s is held by another process (pid %d, most likely a running \"hho serve\")", e.Path, e.PID)
	}
	return fmt.Sprintf("%s is held by another process (most likely a running \"hho serve\")", e.Path)
}

func IsHeld(err error) bool {
	var held *HeldError
	return errors.As(err, &held)
}

type Lock struct {
	f *os.File
}

func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

func Acquire(dataDir string) (*Lock, error) {
	path := filepath.Join(dataDir, FileName)
	f, err := acquireFile(path)
	if err != nil {
		return nil, err
	}
	if err := writeHolderInfo(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("serverlock: record holder info in %s: %w", path, err)
	}
	return &Lock{f: f}, nil
}

func Probe(dataDir string) error {
	path := filepath.Join(dataDir, FileName)
	f, err := acquireFile(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func acquireFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("serverlock: open %s: %w", path, err)
	}

	switch lockErr := flockNB(f); {
	case lockErr == nil:
		return f, nil
	case errors.Is(lockErr, errLocked):
		pid := readHolderPID(path)
		_ = f.Close()
		return nil, &HeldError{Path: path, PID: pid}
	default:
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s: %v", ErrUnsupported, path, lockErr)
	}
}

func writeHolderInfo(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "pid=%d\nstarted=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return f.Sync()
}

func readHolderPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for line := range strings.Lines(string(data)) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "pid=")
		if !ok {
			continue
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}
