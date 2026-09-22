package devicetoken

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"strings"
	"time"
)

const maxDeviceLabelLength = 200

var ErrDeviceLabelRequired = errors.New("devicetoken: device label is required")

var ErrDeviceLabelTooLong = errors.New("devicetoken: device label is too long")

type Service struct {
	storage *storage.Storage
}

func NewService(store *storage.Storage) (*Service, error) {
	if store == nil {
		return nil, errors.New("devicetoken: NewService needs a *storage.Storage")
	}
	return &Service{storage: store}, nil
}

type IssueRequest struct {
	GroupID     string
	UserID      string
	DeviceLabel string
}

type Issued struct {
	ID          string
	DeviceLabel string
	Token       string
}

func (s *Service) Issue(ctx context.Context, req IssueRequest) (Issued, error) {
	if req.GroupID == "" || req.UserID == "" {
		return Issued{}, errors.New("devicetoken: Issue needs a non-empty GroupID and UserID")
	}

	label := strings.TrimSpace(req.DeviceLabel)
	if label == "" {
		return Issued{}, ErrDeviceLabelRequired
	}
	if len(label) > maxDeviceLabelLength {
		return Issued{}, fmt.Errorf("%w: must be at most %d bytes", ErrDeviceLabelTooLong, maxDeviceLabelLength)
	}

	token, err := bearertoken.New()
	if err != nil {
		return Issued{}, fmt.Errorf("devicetoken: draw a token: %w", err)
	}

	id, err := newID()
	if err != nil {
		return Issued{}, err
	}

	if err := s.storage.CreateDeviceToken(ctx, storage.DeviceTokenParams{
		ID:          id,
		GroupID:     req.GroupID,
		UserID:      req.UserID,
		TokenHash:   bearertoken.Hash(token),
		DeviceLabel: label,
		Now:         time.Now().UnixMilli(),
	}); err != nil {
		return Issued{}, fmt.Errorf("devicetoken: create device token: %w", err)
	}

	return Issued{ID: id, DeviceLabel: label, Token: token}, nil
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("devicetoken: generate id: %w", err)
	}
	return id.String(), nil
}
