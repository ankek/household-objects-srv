package attachments

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"time"
)

var thumbnailContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

const thumbnailFileSuffix = "-thumb.jpg"

const thumbnailMaxDimension = 320

const thumbnailJPEGQuality = 82

const maxThumbnailSourcePixels = 40_000_000

var ErrThumbnailSourceTooLarge = errors.New("attachments: image dimensions exceed the thumbnailing pixel ceiling")

func generateThumbnail(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("attachments: open %s for thumbnailing: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return decodeAndScale(f)
}

func decodeAndScale(r io.ReadSeeker) (_ []byte, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("attachments: thumbnail decode panicked: %v", p)
		}
	}()

	cfg, _, cfgErr := image.DecodeConfig(r)
	if cfgErr != nil {
		return nil, fmt.Errorf("attachments: decode image header: %w", cfgErr)
	}
	if pixels := int64(cfg.Width) * int64(cfg.Height); pixels > maxThumbnailSourcePixels {
		return nil, fmt.Errorf("attachments: %dx%d (%d pixels): %w", cfg.Width, cfg.Height, pixels, ErrThumbnailSourceTooLarge)
	}

	if _, seekErr := r.Seek(0, io.SeekStart); seekErr != nil {
		return nil, fmt.Errorf("attachments: rewind for full decode: %w", seekErr)
	}
	img, _, decodeErr := image.Decode(r)
	if decodeErr != nil {
		return nil, fmt.Errorf("attachments: decode image: %w", decodeErr)
	}

	var buf bytes.Buffer
	if encodeErr := jpeg.Encode(&buf, scaleToThumbnail(img), &jpeg.Options{Quality: thumbnailJPEGQuality}); encodeErr != nil {
		return nil, fmt.Errorf("attachments: encode thumbnail: %w", encodeErr)
	}
	return buf.Bytes(), nil
}

func scaleToThumbnail(img image.Image) *image.RGBA {
	src := img.Bounds()
	w, h := src.Dx(), src.Dy()
	tw, th := w, h
	if w > thumbnailMaxDimension || h > thumbnailMaxDimension {
		if w >= h {
			tw = thumbnailMaxDimension
			th = int(math.Round(float64(h) * float64(thumbnailMaxDimension) / float64(w)))
		} else {
			th = thumbnailMaxDimension
			tw = int(math.Round(float64(w) * float64(thumbnailMaxDimension) / float64(h)))
		}
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, src, draw.Over, nil)
	return dst
}

func generateAndRecordThumbnail(ctx context.Context, repo storage.AttachmentRepository, root, groupID, itemID, id, contentType string) (storage.Attachment, bool) {
	if !thumbnailContentTypes[contentType] {
		return storage.Attachment{}, false
	}

	finalDir := filepath.Join(root, datadir.AttachmentsSubdir, groupID)
	data, err := generateThumbnail(filepath.Join(finalDir, id))
	if err != nil {
		return storage.Attachment{}, false
	}

	tmpFile, err := os.CreateTemp(filepath.Join(root, datadir.TmpSubdir), "thumbnail-*.tmp")
	if err != nil {
		return storage.Attachment{}, false
	}
	tmpPath := tmpFile.Name()
	removeTmpOnReturn := true
	defer func() {
		if removeTmpOnReturn {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return storage.Attachment{}, false
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return storage.Attachment{}, false
	}
	if err := tmpFile.Close(); err != nil {
		return storage.Attachment{}, false
	}

	thumbName := id + thumbnailFileSuffix
	finalThumbPath := filepath.Join(finalDir, thumbName)
	if err := os.Rename(tmpPath, finalThumbPath); err != nil {
		return storage.Attachment{}, false
	}
	removeTmpOnReturn = false

	if err := fsyncDir(finalDir); err != nil {
		_ = os.Remove(finalThumbPath)
		return storage.Attachment{}, false
	}

	updated, err := repo.SetThumbnailPath(ctx, itemID, id, path.Join(groupID, thumbName), time.Now().UnixMilli())
	if err != nil {
		_ = os.Remove(finalThumbPath)
		return storage.Attachment{}, false
	}
	return updated, true
}
