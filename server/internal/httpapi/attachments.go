package httpapi

import (
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"time"
)

const multipartMemoryBytes = 1 << 20

const multipartOverheadBytes int64 = 64 << 10

func maxAttachmentBytes(cfg Config) int64 {
	if cfg.MaxAttachmentBytes <= 0 {
		return attachments.DefaultMaxUploadBytes
	}
	return cfg.MaxAttachmentBytes
}

func uploadBodyLimit(cfg Config) int64 {
	return maxAttachmentBytes(cfg) + multipartOverheadBytes
}

type attachmentBody struct {
	ID               string `json:"id"`
	ItemID           string `json:"item_id"`
	Category         string `json:"category"`
	OriginalFilename string `json:"original_filename"`
	ContentType      string `json:"content_type"`
	SizeBytes        int64  `json:"size_bytes"`
	SHA256           string `json:"sha256"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
	Version          int64  `json:"version"`

	HasThumbnail bool `json:"has_thumbnail"`
}

func toAttachmentBody(row storage.Attachment) attachmentBody {
	return attachmentBody{
		ID:               row.ID,
		ItemID:           row.ItemID,
		Category:         row.Category,
		OriginalFilename: row.OriginalFilename,
		ContentType:      row.ContentType,
		SizeBytes:        row.SizeBytes,
		SHA256:           row.Sha256,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		Version:          row.Version,
		HasThumbnail:     row.ThumbnailPath.Valid,
	}
}

type attachmentListResponse struct {
	Attachments []attachmentBody `json:"attachments"`
}

func attachmentListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		rows, err := scope.Attachments().ListForItem(r.Context(), itemID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list item attachments failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		out := make([]attachmentBody, 0, len(rows))
		for _, row := range rows {
			out = append(out, toAttachmentBody(row))
		}
		writeJSON(w, r, http.StatusOK, attachmentListResponse{Attachments: out})
	}
}

func attachmentUploadHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		if err := r.ParseMultipartForm(multipartMemoryBytes); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				problem.Write(w, r, problem.ContentTooLarge())
				return
			}
			problem.Write(w, r, problem.BadRequest("the request is not a valid multipart/form-data upload"))
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		fileHeaders := r.MultipartForm.File["file"]
		if len(fileHeaders) == 0 {
			problem.Write(w, r, problem.BadRequest(`the request must include a "file" part`))
			return
		}
		fh := fileHeaders[0]

		file, err := fh.Open()
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "open uploaded file part failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}
		defer func() { _ = file.Close() }()

		created, err := attachments.Upload(r.Context(), scope.Items(), scope.Attachments(), cfg.DataDir, maxAttachmentBytes(cfg), attachments.UploadParams{
			ItemID:           itemID,
			GroupID:          scope.GroupID().String(),
			Category:         r.FormValue("category"),
			OriginalFilename: fh.Filename,
		}, file)
		switch {
		case errors.Is(err, attachments.ErrFilenameRequired), errors.Is(err, attachments.ErrCategoryInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, attachments.ErrTooLarge):
			problem.Write(w, r, problem.ContentTooLarge())
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "upload attachment failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toAttachmentBody(created))
	}
}

func getAttachmentOrNotFound(w http.ResponseWriter, r *http.Request, cfg Config, scope storage.Scope, itemID, attachmentID string) (storage.Attachment, bool) {
	row, err := scope.Attachments().Get(r.Context(), itemID, attachmentID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		problem.Write(w, r, problem.NotFound())
		return storage.Attachment{}, false
	case err != nil:
		cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "get attachment failed",
			slog.String("request_id", requestid.FromContext(r.Context())),
			slog.String("error", err.Error()),
		)
		problem.Write(w, r, problem.Internal())
		return storage.Attachment{}, false
	}
	return row, true
}

func attachmentDownloadHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")
		attachmentID := chi.URLParam(r, "attachmentID")

		row, ok := getAttachmentOrNotFound(w, r, cfg, scope, itemID, attachmentID)
		if !ok {
			return
		}

		f, err := attachments.OpenOriginal(cfg.DataDir, scope.GroupID().String(), row)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "open attachment for download failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}
		defer func() { _ = f.Close() }()

		serveAttachmentFile(w, r, f, row.ContentType, row.OriginalFilename)
	}
}

func attachmentThumbnailDownloadHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")
		attachmentID := chi.URLParam(r, "attachmentID")

		row, ok := getAttachmentOrNotFound(w, r, cfg, scope, itemID, attachmentID)
		if !ok {
			return
		}

		f, err := attachments.OpenThumbnail(cfg.DataDir, scope.GroupID().String(), row)
		if err != nil {
			if errors.Is(err, attachments.ErrNoThumbnail) {
				problem.Write(w, r, problem.NotFound())
				return
			}
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "open attachment thumbnail for download failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}
		defer func() { _ = f.Close() }()

		serveAttachmentFile(w, r, f, "image/jpeg", row.ID+"-thumb.jpg")
	}
}

func serveAttachmentFile(w http.ResponseWriter, r *http.Request, f *os.File, contentType, filename string) {
	info, err := f.Stat()
	if err != nil {
		problem.Write(w, r, problem.Internal())
		return
	}

	disposition := "attachment"
	if attachments.InlineSafeContentType(contentType) {
		disposition = "inline"
	}

	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", attachmentContentDisposition(disposition, filename))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")

	http.ServeContent(w, r, "", info.ModTime(), f)
}

func attachmentContentDisposition(disposition, filename string) string {
	if v := mime.FormatMediaType(disposition, map[string]string{"filename": filename}); v != "" {
		return v
	}
	return disposition
}

func attachmentDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")
		attachmentID := chi.URLParam(r, "attachmentID")

		row, ok := getAttachmentOrNotFound(w, r, cfg, scope, itemID, attachmentID)
		if !ok {
			return
		}

		err := scope.Attachments().Delete(r.Context(), itemID, attachmentID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete attachment failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		for _, unlinkErr := range attachments.Delete(cfg.DataDir, scope.GroupID().String(), row) {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelWarn, "unlink attachment file after delete failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("attachment_id", attachmentID),
				slog.String("error", unlinkErr.Error()),
			)
		}

		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
