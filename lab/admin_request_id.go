package lab

import (
	"context"
	"strings"
)

type adminRequestIDKey struct{}

// WithAdminRequestID carries a server-generated request reference into
// command admission and audit writes. Invalid values are ignored.
func WithAdminRequestID(ctx context.Context, id string) context.Context {
	if len(id) < 8 || len(id) > 128 || strings.IndexFunc(id, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_')
	}) >= 0 {
		return ctx
	}
	return context.WithValue(ctx, adminRequestIDKey{}, id)
}

func adminRequestID(ctx context.Context) string {
	id, _ := ctx.Value(adminRequestIDKey{}).(string)
	return id
}

func commandRequestID(ctx context.Context) (string, error) {
	if id := adminRequestID(ctx); id != "" {
		return id, nil
	}
	return newID("req_")
}
