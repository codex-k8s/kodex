package emailbridgeapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const ExecutionHeader = "X-Kodex-Email-Execution"

type executionContextKey struct{}

func ValidExecutionBinding(b *ExecutionBinding) bool {
	if b == nil || (b.InvocationRef == nil) == (b.ConnectionTestRef == nil) || len(b.Lease.Ref) > 128 || b.Lease.Generation < 1 || b.Lease.Generation > 9007199254740991 || b.Lease.ExpiresAt.IsZero() {
		return false
	}
	for _, value := range []string{b.Lease.Ref, b.Lease.Fence} {
		if value == "" || len(value) > 512 || strings.ContainsAny(value, " \t\r\n\x00") {
			return false
		}
	}
	ref := b.InvocationRef
	if ref == nil {
		ref = b.ConnectionTestRef
	}
	return len(*ref) > 0 && len(*ref) <= 128 && !strings.ContainsAny(*ref, " \t\r\n\x00")
}

func ExecutionHeaderValue(b *ExecutionBinding) (string, error) {
	if !ValidExecutionBinding(b) {
		return "", errors.New("invalid email execution binding")
	}
	raw, err := json.Marshal(b)
	return string(raw), err
}

func ParseExecutionHeader(raw string) (*ExecutionBinding, error) {
	var b ExecutionBinding
	if len(raw) > 4096 || Decode([]byte(raw), &b) != nil || !ValidExecutionBinding(&b) || !b.Lease.ExpiresAt.After(time.Now()) {
		return nil, errors.New("invalid email execution binding")
	}
	return &b, nil
}

func WithExecutionBinding(ctx context.Context, b *ExecutionBinding) context.Context {
	return context.WithValue(ctx, executionContextKey{}, b)
}

func ExecutionFromContext(ctx context.Context) *ExecutionBinding {
	b, _ := ctx.Value(executionContextKey{}).(*ExecutionBinding)
	return b
}
