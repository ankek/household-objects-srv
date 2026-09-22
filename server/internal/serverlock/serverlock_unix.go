//go:build unix

package serverlock

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func flockNB(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return errLocked
	}
	return err
}
