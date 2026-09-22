package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type Attachment = gen.Attachment

var ErrCategoryInvalid = errors.New("storage: category must be one of image, manual, warranty, receipt, general")

var validCategories = map[string]bool{
	"image":    true,
	"manual":   true,
	"warranty": true,
	"receipt":  true,
	"general":  true,
}

func validCategory(category string) bool { return validCategories[category] }

type AttachmentRepository interface {
	Create(ctx context.Context, p CreateAttachmentParams) (Attachment, error)

	SetThumbnailPath(ctx context.Context, itemID, id, thumbnailPath string, now int64) (Attachment, error)

	Get(ctx context.Context, itemID, id string) (Attachment, error)

	Delete(ctx context.Context, itemID, id string, now int64) error

	ListForItem(ctx context.Context, itemID string) ([]Attachment, error)

	ListAll(ctx context.Context) ([]Attachment, error)

	ClearThumbnailPath(ctx context.Context, itemID, id string, now int64) error
}

type CreateAttachmentParams struct {
	ID               string
	ItemID           string
	Category         string
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	StoragePath      string
	SHA256           string
	Now              int64
}

func (p CreateAttachmentParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateAttachmentParams: ID is empty")
	case p.ItemID == "":
		return errors.New("storage: CreateAttachmentParams: ItemID is empty")
	case p.OriginalFilename == "":
		return errors.New("storage: CreateAttachmentParams: OriginalFilename is empty")
	case p.ContentType == "":
		return errors.New("storage: CreateAttachmentParams: ContentType is empty")
	case p.StoragePath == "":
		return errors.New("storage: CreateAttachmentParams: StoragePath is empty")
	case p.SHA256 == "":
		return errors.New("storage: CreateAttachmentParams: SHA256 is empty")
	case p.SizeBytes < 0:
		return errors.New("storage: CreateAttachmentParams: SizeBytes is negative")
	case p.Now <= 0:
		return errors.New("storage: CreateAttachmentParams: Now must be a positive Unix-millisecond timestamp")
	case !validCategory(p.Category):
		return fmt.Errorf("%w (got %q)", ErrCategoryInvalid, p.Category)
	}
	return nil
}

type attachmentRepository struct {
	binding
}

func (r attachmentRepository) Create(ctx context.Context, p CreateAttachmentParams) (Attachment, error) {
	if err := p.validate(); err != nil {
		return Attachment{}, err
	}

	var created Attachment
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		if _, err := q.GetItem(ctx, gen.GetItemParams{GroupID: r.group(), ItemID: p.ItemID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
			}
			return fmt.Errorf("storage: get item %q for attachment create: %w", p.ItemID, err)
		}

		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for attachment: %w", err)
		}

		if err := q.CreateAttachment(ctx, gen.CreateAttachmentParams{
			ID:               p.ID,
			GroupID:          r.group(),
			ItemID:           p.ItemID,
			Category:         p.Category,
			OriginalFilename: p.OriginalFilename,
			ContentType:      p.ContentType,
			SizeBytes:        p.SizeBytes,
			StoragePath:      p.StoragePath,
			Sha256:           p.SHA256,
			Now:              p.Now,
			ChangeSeq:        seq,
		}); err != nil {
			return fmt.Errorf("storage: create attachment for item %q: %w", p.ItemID, err)
		}

		created, err = q.GetAttachment(ctx, gen.GetAttachmentParams{GroupID: r.group(), ItemID: p.ItemID, ID: p.ID})
		if err != nil {
			return fmt.Errorf("storage: read back created attachment %q for item %q: %w", p.ID, p.ItemID, err)
		}
		return nil
	})
	if err != nil {
		return Attachment{}, err
	}
	return created, nil
}

func (r attachmentRepository) SetThumbnailPath(ctx context.Context, itemID, id, thumbnailPath string, now int64) (Attachment, error) {
	switch {
	case itemID == "":
		return Attachment{}, errors.New("storage: SetThumbnailPath: itemID is empty")
	case id == "":
		return Attachment{}, errors.New("storage: SetThumbnailPath: id is empty")
	case thumbnailPath == "":
		return Attachment{}, errors.New("storage: SetThumbnailPath: thumbnailPath is empty")
	case now <= 0:
		return Attachment{}, errors.New("storage: SetThumbnailPath: now must be a positive Unix-millisecond timestamp")
	}

	var updated Attachment
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for attachment thumbnail: %w", err)
		}

		rows, err := q.SetAttachmentThumbnailPath(ctx, gen.SetAttachmentThumbnailPathParams{
			ThumbnailPath: sql.NullString{String: thumbnailPath, Valid: true},
			Now:           now,
			ChangeSeq:     seq,
			GroupID:       r.group(),
			ItemID:        itemID,
			ID:            id,
		})
		if err != nil {
			return fmt.Errorf("storage: set thumbnail path for attachment %q on item %q: %w", id, itemID, err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: attachment %q on item %q: %w", id, itemID, ErrNotFound)
		}

		updated, err = q.GetAttachment(ctx, gen.GetAttachmentParams{GroupID: r.group(), ItemID: itemID, ID: id})
		if err != nil {
			return fmt.Errorf("storage: read back attachment %q on item %q after setting thumbnail path: %w", id, itemID, err)
		}
		return nil
	})
	if err != nil {
		return Attachment{}, err
	}
	return updated, nil
}

func (r attachmentRepository) Get(ctx context.Context, itemID, id string) (Attachment, error) {
	if itemID == "" {
		return Attachment{}, errors.New("storage: Get: itemID is empty")
	}
	if id == "" {
		return Attachment{}, errors.New("storage: Get: id is empty")
	}

	row, err := r.queries().GetAttachment(ctx, gen.GetAttachmentParams{GroupID: r.group(), ItemID: itemID, ID: id})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Attachment{}, fmt.Errorf("storage: attachment %q on item %q: %w", id, itemID, ErrNotFound)
		}
		return Attachment{}, fmt.Errorf("storage: get attachment %q on item %q: %w", id, itemID, err)
	}
	return row, nil
}

func (r attachmentRepository) Delete(ctx context.Context, itemID, id string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: Delete attachment: itemID is empty")
	case id == "":
		return errors.New("storage: Delete attachment: id is empty")
	case now <= 0:
		return errors.New("storage: Delete attachment: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for attachment delete: %w", err)
		}

		rows, err := q.DeleteAttachment(ctx, gen.DeleteAttachmentParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
			ID:        id,
		})
		if err != nil {
			return fmt.Errorf("storage: delete attachment %q on item %q: %w", id, itemID, err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: attachment %q on item %q: %w", id, itemID, ErrNotFound)
		}
		return nil
	})
}

func (r attachmentRepository) ListForItem(ctx context.Context, itemID string) ([]Attachment, error) {
	if _, err := r.queries().GetItem(ctx, gen.GetItemParams{GroupID: r.group(), ItemID: itemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("storage: item %q: %w", itemID, ErrNotFound)
		}
		return nil, fmt.Errorf("storage: get item %q for attachment list: %w", itemID, err)
	}

	rows, err := r.queries().ListAttachmentsForItem(ctx, gen.ListAttachmentsForItemParams{
		GroupID: r.group(),
		ItemID:  itemID,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list attachments for item %q: %w", itemID, err)
	}
	return rows, nil
}

func (r attachmentRepository) ListAll(ctx context.Context) ([]Attachment, error) {
	rows, err := r.queries().ListAttachmentsForGroup(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: list attachments for group: %w", err)
	}
	return rows, nil
}

func (r attachmentRepository) ClearThumbnailPath(ctx context.Context, itemID, id string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: ClearThumbnailPath: itemID is empty")
	case id == "":
		return errors.New("storage: ClearThumbnailPath: id is empty")
	case now <= 0:
		return errors.New("storage: ClearThumbnailPath: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for attachment thumbnail clear: %w", err)
		}

		rows, err := q.ClearAttachmentThumbnailPath(ctx, gen.ClearAttachmentThumbnailPathParams{
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
			ID:        id,
		})
		if err != nil {
			return fmt.Errorf("storage: clear thumbnail path for attachment %q on item %q: %w", id, itemID, err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: attachment %q on item %q: %w", id, itemID, ErrNotFound)
		}
		return nil
	})
}
