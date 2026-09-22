package attachments

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode test JPEG: %v", err)
	}
	return buf.Bytes()
}

func encodePNGWithAlpha(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.Set(x, y, color.NRGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.NRGBA{})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func writeTempImage(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write test image: %v", err)
	}
	return path
}

func decodedDimensions(t *testing.T, b []byte) (width, height int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode thumbnail output: %v", err)
	}
	return cfg.Width, cfg.Height
}

func TestGenerateThumbnailScalesToFitPreservingAspectRatio(t *testing.T) {
	for _, tc := range []struct {
		name       string
		srcW, srcH int
		wantW      int
		wantH      int
	}{
		{"landscape", 800, 400, 320, 160},
		{"portrait", 400, 800, 160, 320},
		{"square", 640, 640, 320, 320},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempImage(t, encodeJPEG(t, tc.srcW, tc.srcH))

			out, err := generateThumbnail(path)
			if err != nil {
				t.Fatalf("generateThumbnail: %v", err)
			}
			gotW, gotH := decodedDimensions(t, out)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Errorf("thumbnail dimensions = %dx%d, want %dx%d", gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestGenerateThumbnailNeverUpscales(t *testing.T) {
	path := writeTempImage(t, encodeJPEG(t, 100, 50))

	out, err := generateThumbnail(path)
	if err != nil {
		t.Fatalf("generateThumbnail: %v", err)
	}
	gotW, gotH := decodedDimensions(t, out)
	if gotW != 100 || gotH != 50 {
		t.Errorf("thumbnail dimensions = %dx%d, want the ORIGINAL 100x50 (no upscale)", gotW, gotH)
	}
}

func TestGenerateThumbnailProducesDecodableJPEGRegardlessOfSourceFormat(t *testing.T) {
	path := writeTempImage(t, encodePNGWithAlpha(t, 200, 100))

	out, err := generateThumbnail(path)
	if err != nil {
		t.Fatalf("generateThumbnail: %v", err)
	}
	if _, formatName, err := image.Decode(bytes.NewReader(out)); err != nil || formatName != "jpeg" {
		t.Errorf("output format = %q, err = %v; want a decodable jpeg", formatName, err)
	}
}

func pngChunk(buf *bytes.Buffer, typ string, data []byte) {
	var lenField [4]byte
	binary.BigEndian.PutUint32(lenField[:], uint32(len(data)))
	buf.Write(lenField[:])

	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(data)

	buf.WriteString(typ)
	buf.Write(data)

	var crcField [4]byte
	binary.BigEndian.PutUint32(crcField[:], crc.Sum32())
	buf.Write(crcField[:])
}

func pngBomb(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 2
	ihdr[10] = 0
	ihdr[11] = 0
	ihdr[12] = 0
	pngChunk(&buf, "IHDR", ihdr)

	return buf.Bytes()
}

func TestGenerateThumbnailRejectsDeclaredDimensionsOverThePixelCeiling(t *testing.T) {
	path := writeTempImage(t, pngBomb(20_000, 20_000))

	_, err := generateThumbnail(path)
	if !errors.Is(err, ErrThumbnailSourceTooLarge) {
		t.Fatalf("generateThumbnail error = %v, want ErrThumbnailSourceTooLarge", err)
	}
}

func TestGenerateThumbnailAcceptsDimensionsAtExactlyTheCeiling(t *testing.T) {
	const w, h = 8000, 5000
	if int64(w)*int64(h) != maxThumbnailSourcePixels {
		t.Fatalf("test fixture is wrong: %d*%d != maxThumbnailSourcePixels (%d)", w, h, int64(maxThumbnailSourcePixels))
	}

	path := writeTempImage(t, pngBomb(w, h))

	_, err := generateThumbnail(path)
	if errors.Is(err, ErrThumbnailSourceTooLarge) {
		t.Fatalf("generateThumbnail rejected exactly maxThumbnailSourcePixels (%d) as too large; the ceiling must accept dimensions AT the limit, only reject OVER it", int64(maxThumbnailSourcePixels))
	}
}

func TestGenerateThumbnailRejectsACorruptFile(t *testing.T) {
	path := writeTempImage(t, []byte("\x89PNG\r\n\x1a\nthis is not a real PNG file body"))

	if _, err := generateThumbnail(path); err == nil {
		t.Fatal("generateThumbnail on a corrupt file succeeded, want an error")
	}
}

type panicReadSeeker struct{}

func (panicReadSeeker) Read([]byte) (int, error) {
	panic("simulated decoder panic: TestDecodeAndScaleRecoversFromAPanic")
}

func (panicReadSeeker) Seek(int64, int) (int64, error) { return 0, nil }

func TestDecodeAndScaleRecoversFromAPanic(t *testing.T) {
	_, err := decodeAndScale(panicReadSeeker{})
	if err == nil {
		t.Fatal("decodeAndScale with a panicking reader returned no error, want the recovered panic reported as one")
	}
}

type fakeThumbnailAttachmentRepository struct {
	setThumbnailPathFn func(ctx context.Context, itemID, id, thumbnailPath string, now int64) (storage.Attachment, error)
	calls              int
}

func (f *fakeThumbnailAttachmentRepository) Create(context.Context, storage.CreateAttachmentParams) (storage.Attachment, error) {
	panic("fakeThumbnailAttachmentRepository: Create: generateAndRecordThumbnail never calls this")
}

func (f *fakeThumbnailAttachmentRepository) SetThumbnailPath(ctx context.Context, itemID, id, thumbnailPath string, now int64) (storage.Attachment, error) {
	f.calls++
	if f.setThumbnailPathFn != nil {
		return f.setThumbnailPathFn(ctx, itemID, id, thumbnailPath, now)
	}
	return storage.Attachment{ID: id, ItemID: itemID, ThumbnailPath: sql.NullString{String: thumbnailPath, Valid: true}}, nil
}

func (f *fakeThumbnailAttachmentRepository) Get(context.Context, string, string) (storage.Attachment, error) {
	panic("fakeThumbnailAttachmentRepository: Get: generateAndRecordThumbnail never calls this")
}

func (f *fakeThumbnailAttachmentRepository) Delete(context.Context, string, string, int64) error {
	panic("fakeThumbnailAttachmentRepository: Delete: generateAndRecordThumbnail never calls this")
}

func (f *fakeThumbnailAttachmentRepository) ListForItem(context.Context, string) ([]storage.Attachment, error) {
	panic("fakeThumbnailAttachmentRepository: ListForItem: generateAndRecordThumbnail never calls this")
}

func (f *fakeThumbnailAttachmentRepository) ListAll(context.Context) ([]storage.Attachment, error) {
	panic("fakeThumbnailAttachmentRepository: ListAll: generateAndRecordThumbnail never calls this")
}

func (f *fakeThumbnailAttachmentRepository) ClearThumbnailPath(context.Context, string, string, int64) error {
	panic("fakeThumbnailAttachmentRepository: ClearThumbnailPath: generateAndRecordThumbnail never calls this")
}

func TestGenerateAndRecordThumbnailSkipsUnsupportedContentTypes(t *testing.T) {
	root := testRoot(t)
	groupDir := filepath.Join(root, "attachments", "group-1")
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "att-1"), []byte("not an image"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	repo := &fakeThumbnailAttachmentRepository{}
	_, ok := generateAndRecordThumbnail(t.Context(), repo, root, "group-1", "item-1", "att-1", "application/pdf")
	if ok {
		t.Error("generateAndRecordThumbnail reported success for an unsupported content type")
	}
	if repo.calls != 0 {
		t.Errorf("SetThumbnailPath was called %d times, want 0 -- an unsupported content type must never reach it", repo.calls)
	}
}

func TestGenerateAndRecordThumbnailSucceedsForARealImage(t *testing.T) {
	root := testRoot(t)
	groupDir := filepath.Join(root, "attachments", "group-1")
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "att-1"), encodeJPEG(t, 640, 480), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	repo := &fakeThumbnailAttachmentRepository{}
	updated, ok := generateAndRecordThumbnail(t.Context(), repo, root, "group-1", "item-1", "att-1", "image/jpeg")
	if !ok {
		t.Fatal("generateAndRecordThumbnail reported failure for a valid image")
	}
	if repo.calls != 1 {
		t.Errorf("SetThumbnailPath was called %d times, want exactly 1", repo.calls)
	}
	if !updated.ThumbnailPath.Valid {
		t.Errorf("returned attachment has no ThumbnailPath set: %+v", updated)
	}

	wantThumbPath := filepath.Join(groupDir, "att-1"+thumbnailFileSuffix)
	if _, err := os.Stat(wantThumbPath); err != nil {
		t.Errorf("thumbnail file not found at %s: %v", wantThumbPath, err)
	}
}

func TestGenerateAndRecordThumbnailFailsGracefullyOnACorruptSource(t *testing.T) {
	root := testRoot(t)
	groupDir := filepath.Join(root, "attachments", "group-1")
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "att-1"), []byte("\xff\xd8\xff\xe0not a real jpeg body"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	repo := &fakeThumbnailAttachmentRepository{}
	_, ok := generateAndRecordThumbnail(t.Context(), repo, root, "group-1", "item-1", "att-1", "image/jpeg")
	if ok {
		t.Error("generateAndRecordThumbnail reported success for a corrupt source image")
	}
	if repo.calls != 0 {
		t.Errorf("SetThumbnailPath was called %d times, want 0 -- a failed decode must never reach it", repo.calls)
	}
}

func TestUploadSucceedsAndRecordsAThumbnailForARealImage(t *testing.T) {
	repo := &fakeAttachmentRepository{}
	p := validParams()
	p.Category = CategoryImage

	created, err := Upload(t.Context(), liveItem(), repo, testRoot(t), DefaultMaxUploadBytes, p, bytes.NewReader(encodeJPEG(t, 640, 480)))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if len(repo.thumbnailPathCalls) != 1 {
		t.Fatalf("SetThumbnailPath was called %d times, want exactly 1 for a real JPEG upload", len(repo.thumbnailPathCalls))
	}
	if !created.ThumbnailPath.Valid {
		t.Errorf("Upload's returned attachment has no ThumbnailPath, want the fake's recorded value")
	}
}

func TestUploadSucceedsWithNoThumbnailForANonImageAttachment(t *testing.T) {
	repo := &fakeAttachmentRepository{}

	created, err := Upload(t.Context(), liveItem(), repo, testRoot(t), DefaultMaxUploadBytes, validParams(), bytes.NewReader([]byte("just a plain text warranty note")))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if len(repo.thumbnailPathCalls) != 0 {
		t.Errorf("SetThumbnailPath was called %d times, want 0 for a non-image upload", len(repo.thumbnailPathCalls))
	}
	if created.ThumbnailPath.Valid {
		t.Errorf("ThumbnailPath = %+v, want NULL for a non-image upload", created.ThumbnailPath)
	}
}

func TestUploadSucceedsWithNoThumbnailForACorruptImage(t *testing.T) {
	repo := &fakeAttachmentRepository{}
	corrupt := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0x00}, 64)...)

	created, err := Upload(t.Context(), liveItem(), repo, testRoot(t), DefaultMaxUploadBytes, validParams(), bytes.NewReader(corrupt))
	if err != nil {
		t.Fatalf("Upload on a corrupt-but-image-sniffing file failed, want success with no thumbnail: %v", err)
	}
	if len(repo.thumbnailPathCalls) != 0 {
		t.Errorf("SetThumbnailPath was called %d times, want 0 -- a failed decode must not reach it", len(repo.thumbnailPathCalls))
	}
	if created.ThumbnailPath.Valid {
		t.Errorf("ThumbnailPath = %+v, want NULL for a corrupt image", created.ThumbnailPath)
	}
}

func TestGenerateThumbnailFailsWhenTheSourceFileDoesNotExist(t *testing.T) {
	_, err := generateThumbnail(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("generateThumbnail on a nonexistent path succeeded, want an error")
	}
}

type seekFailsAfterConfigReader struct {
	*bytes.Reader
	err error
}

func (r seekFailsAfterConfigReader) Seek(int64, int) (int64, error) { return 0, r.err }

func TestDecodeAndScaleFailsWhenTheRewindSeekFails(t *testing.T) {
	wantErr := errors.New("attachments_test: simulated seek fault")
	src := seekFailsAfterConfigReader{Reader: bytes.NewReader(encodeJPEG(t, 10, 10)), err: wantErr}

	_, err := decodeAndScale(src)
	if !errors.Is(err, wantErr) {
		t.Fatalf("decodeAndScale error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestScaleToThumbnailClampsExtremeAspectRatiosToOnePixel(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"extremely_wide", 100_000, 1},
		{"extremely_tall", 1, 100_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := image.NewRGBA(image.Rect(0, 0, tc.w, tc.h))
			dst := scaleToThumbnail(src)
			if dst.Bounds().Dx() < 1 || dst.Bounds().Dy() < 1 {
				t.Fatalf("scaleToThumbnail(%dx%d) = %dx%d, want both dimensions clamped to at least 1", tc.w, tc.h, dst.Bounds().Dx(), dst.Bounds().Dy())
			}
		})
	}
}

func TestGenerateAndRecordThumbnailFailsWhenTheStagingDirectoryCannotBeWrittenTo(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	groupDir := filepath.Join(root, "attachments", "group-1")
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "att-1"), encodeJPEG(t, 64, 64), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	tmpDir := filepath.Join(root, "tmp")
	if err := os.Chmod(tmpDir, 0o500); err != nil {
		t.Fatalf("chmod tmp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, 0o700) })

	repo := &fakeThumbnailAttachmentRepository{}
	_, ok := generateAndRecordThumbnail(t.Context(), repo, root, "group-1", "item-1", "att-1", "image/jpeg")
	if ok {
		t.Error("generateAndRecordThumbnail reported success despite an unwritable staging directory")
	}
	if repo.calls != 0 {
		t.Errorf("SetThumbnailPath was called %d times, want 0", repo.calls)
	}
}

func TestGenerateAndRecordThumbnailFailsWhenRenameCannotWriteIntoTheFinalDirectory(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	groupDir := filepath.Join(root, "attachments", "group-1")
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "att-1"), encodeJPEG(t, 64, 64), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Chmod(groupDir, 0o500); err != nil {
		t.Fatalf("chmod group dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(groupDir, 0o700) })

	repo := &fakeThumbnailAttachmentRepository{}
	_, ok := generateAndRecordThumbnail(t.Context(), repo, root, "group-1", "item-1", "att-1", "image/jpeg")
	if ok {
		t.Error("generateAndRecordThumbnail reported success despite an unwritable final group directory")
	}
	if repo.calls != 0 {
		t.Errorf("SetThumbnailPath was called %d times, want 0", repo.calls)
	}
}

func TestGenerateAndRecordThumbnailFailsWhenFsyncDirCannotOpenTheFinalDirectory(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	groupDir := filepath.Join(root, "attachments", "group-1")
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "att-1"), encodeJPEG(t, 64, 64), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Chmod(groupDir, 0o300); err != nil {
		t.Fatalf("chmod group dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(groupDir, 0o700) })

	repo := &fakeThumbnailAttachmentRepository{}
	_, ok := generateAndRecordThumbnail(t.Context(), repo, root, "group-1", "item-1", "att-1", "image/jpeg")
	if ok {
		t.Error("generateAndRecordThumbnail reported success despite fsyncDir being unable to open the final directory")
	}
	if repo.calls != 0 {
		t.Errorf("SetThumbnailPath was called %d times, want 0 -- a durability failure must never be recorded as success", repo.calls)
	}

	if err := os.Chmod(groupDir, 0o700); err != nil {
		t.Fatalf("restore group dir permissions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(groupDir, "att-1"+thumbnailFileSuffix)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Stat(thumbnail) = %v, want a not-exist error -- a fsyncDir failure removes the just-renamed thumbnail synchronously", err)
	}
}

func TestGenerateAndRecordThumbnailRemovesTheRenamedThumbnailWhenSetThumbnailPathFails(t *testing.T) {
	root := testRoot(t)
	groupDir := filepath.Join(root, "attachments", "group-1")
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "att-1"), encodeJPEG(t, 64, 64), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	wantErr := errors.New("attachments_test: simulated SetThumbnailPath fault")
	repo := &fakeThumbnailAttachmentRepository{
		setThumbnailPathFn: func(context.Context, string, string, string, int64) (storage.Attachment, error) {
			return storage.Attachment{}, wantErr
		},
	}

	_, ok := generateAndRecordThumbnail(t.Context(), repo, root, "group-1", "item-1", "att-1", "image/jpeg")
	if ok {
		t.Error("generateAndRecordThumbnail reported success despite SetThumbnailPath failing")
	}

	thumbPath := filepath.Join(groupDir, "att-1"+thumbnailFileSuffix)
	if _, err := os.Stat(thumbPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Stat(%s) = %v, want a not-exist error -- the orphaned thumbnail should have been removed synchronously", thumbPath, err)
	}
}
