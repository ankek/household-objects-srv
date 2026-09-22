package requestid

import (
	"context"
	"crypto/rand"
)

const HeaderName = "X-Request-ID"

type contextKey struct{}

func New() string {
	return rand.Text()
}

func NewContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
