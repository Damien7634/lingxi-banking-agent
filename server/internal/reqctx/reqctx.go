// Package reqctx carries per-request identity through context.Context.
package reqctx

import (
	"context"

	"lingxi/server/internal/apperr"
)

type key int

const (
	keyMeta key = iota
	keyUser
)

type Meta struct {
	RequestID string
	IP        string
	UserAgent string
}

type User struct {
	ID          uint64
	Username    string
	DisplayName string
	SessionHash string
}

func WithMeta(ctx context.Context, m Meta) context.Context { return context.WithValue(ctx, keyMeta, m) }

func MetaFrom(ctx context.Context) Meta {
	m, _ := ctx.Value(keyMeta).(Meta)
	return m
}

func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, keyUser, u)
}

func UserFrom(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(keyUser).(*User)
	return u, ok && u != nil
}

// UserID returns the authenticated user's id or ErrUnauthorized. Services call
// this instead of accepting a user id from input, which is what prevents IDOR.
func UserID(ctx context.Context) (uint64, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return 0, apperr.ErrUnauthorized
	}
	return u.ID, nil
}
