package components

import "context"

type adminContextKey struct{}

func WithAdmin(ctx context.Context, admin bool) context.Context {
	return context.WithValue(ctx, adminContextKey{}, admin)
}
func IsAdminContext(ctx context.Context) bool { v, _ := ctx.Value(adminContextKey{}).(bool); return v }
