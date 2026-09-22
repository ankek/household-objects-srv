package items

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

var customFieldTypes = map[string]bool{
	"text":    true,
	"number":  true,
	"boolean": true,
	"date":    true,
}

var ErrCustomFieldTypeInvalid = errors.New("items: field_type must be one of text, number, boolean, date")

var ErrCustomFieldNameRequired = errors.New("items: name is required")

var ErrCustomFieldValueInvalid = errors.New("items: exactly one of text_value, number_value, bool_value, date_value must be set, matching field_type")

var ErrCustomFieldDefNotFound = errors.New("items: field_def_id does not name a live custom field definition in this group")

var ErrCustomFieldTypeMismatch = errors.New("items: field_type does not match the referenced custom field definition's field_type")

func ValidateCustomFieldType(fieldType string) error {
	if !customFieldTypes[fieldType] {
		return fmt.Errorf("%w (got %q)", ErrCustomFieldTypeInvalid, fieldType)
	}
	return nil
}

func validateCustomFieldValueColumns(fieldType string, text *string, number *float64, boolean *bool, date *string) error {
	populated := 0
	if text != nil {
		populated++
	}
	if number != nil {
		populated++
	}
	if boolean != nil {
		populated++
	}
	if date != nil {
		populated++
	}
	if populated != 1 {
		return ErrCustomFieldValueInvalid
	}

	var ok bool
	switch fieldType {
	case "text":
		ok = text != nil
	case "number":
		ok = number != nil
	case "boolean":
		ok = boolean != nil
	case "date":
		ok = date != nil
	}
	if !ok {
		return ErrCustomFieldValueInvalid
	}
	return nil
}

type CreateItemCustomFieldRequest struct {
	ItemID      string
	FieldDefID  string
	Name        string
	FieldType   string
	TextValue   *string
	NumberValue *float64
	BoolValue   *bool
	DateValue   *string
}

type UpdateItemCustomFieldRequest struct {
	ItemID          string
	CustomFieldID   string
	FieldDefID      string
	Name            string
	FieldType       string
	TextValue       *string
	NumberValue     *float64
	BoolValue       *bool
	DateValue       *string
	ExpectedVersion int64
}

func validateCustomFieldContent(name, fieldType string, text *string, number *float64, boolean *bool, date *string) error {
	if name == "" {
		return ErrCustomFieldNameRequired
	}
	if err := ValidateCustomFieldType(fieldType); err != nil {
		return err
	}
	return validateCustomFieldValueColumns(fieldType, text, number, boolean, date)
}

func resolveFieldDef(ctx context.Context, defRepo storage.CustomFieldDefRepository, fieldDefID, fieldType string) error {
	if fieldDefID == "" {
		return nil
	}
	if defRepo == nil {
		return errors.New("items: a non-nil storage.CustomFieldDefRepository is required when field_def_id is set")
	}
	def, err := defRepo.Get(ctx, fieldDefID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return ErrCustomFieldDefNotFound
	case err != nil:
		return fmt.Errorf("items: read custom field def %q: %w", fieldDefID, err)
	}
	if def.FieldType != fieldType {
		return ErrCustomFieldTypeMismatch
	}
	return nil
}

func CreateItemCustomField(ctx context.Context, repo storage.ItemCustomFieldRepository, defRepo storage.CustomFieldDefRepository, req CreateItemCustomFieldRequest) (storage.ItemCustomField, error) {
	if repo == nil {
		return storage.ItemCustomField{}, errors.New("items: CreateItemCustomField needs a non-nil storage.ItemCustomFieldRepository")
	}
	if err := validateCustomFieldContent(req.Name, req.FieldType, req.TextValue, req.NumberValue, req.BoolValue, req.DateValue); err != nil {
		return storage.ItemCustomField{}, err
	}
	if err := resolveFieldDef(ctx, defRepo, req.FieldDefID, req.FieldType); err != nil {
		return storage.ItemCustomField{}, err
	}

	id, err := newID()
	if err != nil {
		return storage.ItemCustomField{}, err
	}

	return repo.Create(ctx, storage.CreateItemCustomFieldParams{
		ID:          id,
		ItemID:      req.ItemID,
		FieldDefID:  req.FieldDefID,
		Name:        req.Name,
		FieldType:   req.FieldType,
		TextValue:   req.TextValue,
		NumberValue: req.NumberValue,
		BoolValue:   req.BoolValue,
		DateValue:   req.DateValue,
		Now:         time.Now().UnixMilli(),
	})
}

func UpdateItemCustomField(ctx context.Context, repo storage.ItemCustomFieldRepository, defRepo storage.CustomFieldDefRepository, req UpdateItemCustomFieldRequest) (storage.ItemCustomField, error) {
	if repo == nil {
		return storage.ItemCustomField{}, errors.New("items: UpdateItemCustomField needs a non-nil storage.ItemCustomFieldRepository")
	}
	if req.ExpectedVersion <= 0 {
		return storage.ItemCustomField{}, ErrVersionRequired
	}
	if err := validateCustomFieldContent(req.Name, req.FieldType, req.TextValue, req.NumberValue, req.BoolValue, req.DateValue); err != nil {
		return storage.ItemCustomField{}, err
	}
	if err := resolveFieldDef(ctx, defRepo, req.FieldDefID, req.FieldType); err != nil {
		return storage.ItemCustomField{}, err
	}

	return repo.Update(ctx, storage.UpdateItemCustomFieldParams{
		ItemID:          req.ItemID,
		ID:              req.CustomFieldID,
		FieldDefID:      req.FieldDefID,
		Name:            req.Name,
		FieldType:       req.FieldType,
		TextValue:       req.TextValue,
		NumberValue:     req.NumberValue,
		BoolValue:       req.BoolValue,
		DateValue:       req.DateValue,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}
