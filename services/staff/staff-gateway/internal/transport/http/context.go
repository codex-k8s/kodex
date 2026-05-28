package httptransport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const (
	headerRequestID = "X-Kodex-Request-Id"
	headerTraceID   = "X-Kodex-Trace-Id"
	headerSessionID = "X-Kodex-Session-Id"
	headerActorType = "X-Kodex-Actor-Type"
	headerActorID   = "X-Kodex-Actor-Id"
)

type requestIDContextKey struct{}

func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func requestIDFromRequest(req *http.Request) string {
	candidate := strings.TrimSpace(req.Header.Get(headerRequestID))
	if safeRequestID(candidate) {
		return candidate
	}
	return newRequestID()
}

func safeRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	return strings.IndexFunc(value, func(char rune) bool {
		return char <= 32 || char == 127
	}) == -1
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "sgw-request-id-unavailable"
	}
	return hex.EncodeToString(bytes[:])
}
