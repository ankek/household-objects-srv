package storage

import (
	"errors"
	"fmt"
)

var ErrInvalidGroupID = errors.New("storage: invalid group id")

const maxGroupIDLen = 64

type GroupID struct {
	id string
}

func NewGroupID(raw string) (GroupID, error) {
	if raw == "" {
		return GroupID{}, fmt.Errorf("%w: empty", ErrInvalidGroupID)
	}
	if len(raw) > maxGroupIDLen {
		return GroupID{}, fmt.Errorf("%w: %d bytes exceeds the %d-byte limit", ErrInvalidGroupID, len(raw), maxGroupIDLen)
	}
	for i := 0; i < len(raw); i++ {
		if !isGroupIDByte(raw[i]) {
			return GroupID{}, fmt.Errorf("%w: byte %d is %q; ids are ASCII letters, digits, '-' and '_'", ErrInvalidGroupID, i, raw[i])
		}
	}
	return GroupID{id: raw}, nil
}

func MustGroupID(raw string) GroupID {
	g, err := NewGroupID(raw)
	if err != nil {
		panic(err)
	}
	return g
}

func (g GroupID) String() string { return g.id }

func (g GroupID) IsZero() bool { return g.id == "" }

func isGroupIDByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_':
		return true
	default:
		return false
	}
}
