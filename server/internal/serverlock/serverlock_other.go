//go:build !unix

package serverlock

import (
	"errors"
	"os"
)

func flockNB(f *os.File) error {
	return errors.New("serverlock: flock(2) is not available on this platform")
}
