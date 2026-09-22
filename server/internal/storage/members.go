package storage

import (
	"context"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type Member struct {
	ID                string
	Username          string
	Role              string
	JoinedAtUnixMilli int64
}

func toMember(row gen.ListGroupMembersRow) Member {
	return Member{
		ID:                row.ID,
		Username:          row.Username,
		Role:              row.Role,
		JoinedAtUnixMilli: row.CreatedAt,
	}
}

type MemberRepository interface {
	List(ctx context.Context) ([]Member, error)
}

type memberRepository struct {
	binding
}

func (r memberRepository) List(ctx context.Context) ([]Member, error) {
	rows, err := r.queries().ListGroupMembers(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: list group members: %w", err)
	}

	out := make([]Member, 0, len(rows))
	for _, row := range rows {
		out = append(out, toMember(row))
	}
	return out, nil
}
