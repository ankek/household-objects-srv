package main

import (
	"os"
	"testing"
)

func osPipe(t *testing.T) (r, w *os.File, err error) {
	t.Helper()
	r, w, err = os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r, w, nil
}
