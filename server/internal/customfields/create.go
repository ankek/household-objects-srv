package customfields

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

type CreateCustomFieldDefRequest struct {
	Name         string
	FieldType    string
	DisplayOrder int64
}

type UpdateCustomFieldDefRequest struct {
	FieldDefID      string
	Name            string
	FieldType       string
	DisplayOrder    int64
	ExpectedVersion int64
}

func CreateCustomFieldDef(ctx context.Context, repo storage.CustomFieldDefRepository, req CreateCustomFieldDefRequest) (storage.CustomFieldDef, error) {
	if repo == nil {
		return storage.CustomFieldDef{}, errors.New("customfields: CreateCustomFieldDef needs a non-nil storage.CustomFieldDefRepository")
	}
	if req.Name == "" {
		return storage.CustomFieldDef{}, ErrNameRequired
	}
	if err := ValidateFieldType(req.FieldType); err != nil {
		return storage.CustomFieldDef{}, err
	}

	id, err := newID()
	if err != nil {
		return storage.CustomFieldDef{}, err
	}

	return repo.Create(ctx, storage.CreateCustomFieldDefParams{
		ID:           id,
		Name:         req.Name,
		FieldType:    req.FieldType,
		DisplayOrder: req.DisplayOrder,
		Now:          time.Now().UnixMilli(),
	})
}

func UpdateCustomFieldDef(ctx context.Context, repo storage.CustomFieldDefRepository, req UpdateCustomFieldDefRequest) (storage.CustomFieldDef, error) {
	if repo == nil {
		return storage.CustomFieldDef{}, errors.New("customfields: UpdateCustomFieldDef needs a non-nil storage.CustomFieldDefRepository")
	}
	if req.ExpectedVersion <= 0 {
		return storage.CustomFieldDef{}, ErrVersionRequired
	}
	if req.Name == "" {
		return storage.CustomFieldDef{}, ErrNameRequired
	}
	if err := ValidateFieldType(req.FieldType); err != nil {
		return storage.CustomFieldDef{}, err
	}

	return repo.Update(ctx, storage.UpdateCustomFieldDefParams{
		ID:              req.FieldDefID,
		Name:            req.Name,
		FieldType:       req.FieldType,
		DisplayOrder:    req.DisplayOrder,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}
