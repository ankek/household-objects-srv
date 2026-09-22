package sync

import (
	"context"
	"fmt"
	"time"
)

const defaultTombstoneRetention = 90 * 24 * time.Hour

func (r *Reader) TooOld(ctx context.Context, since int64) (bool, error) {
	if r == nil || r.repo == nil {
		return false, fmt.Errorf("%w", ErrNoRepository)
	}
	if since < 0 {
		return false, fmt.Errorf("%w: got %d", ErrInvalidSince, since)
	}
	if since == 0 {
		return false, nil
	}

	lowWatermark, err := r.repo.LowWatermark(ctx)
	if err != nil {
		return false, fmt.Errorf("sync: read tombstone low watermark: %w", err)
	}
	return since < lowWatermark, nil
}

func (r *Reader) PullOrTooOld(ctx context.Context, since, limit int64) (page Page, cursorTooOld bool, err error) {
	tooOld, err := r.TooOld(ctx, since)
	if err != nil {
		return Page{}, false, err
	}
	if tooOld {
		return Page{}, true, nil
	}

	page, err = r.Pull(ctx, since, limit)
	if err != nil {
		return Page{}, false, err
	}
	return page, false, nil
}
