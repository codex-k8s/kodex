package httptransport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stdhttp "net/http"
	"strings"
)

const (
	headerRequestID = "X-Kodex-Request-Id"
)

type requestIDContextKey struct{}
type requestBodyContextKey struct{}
type requestDiagnosticsContextKey struct{}

type requestDiagnostics struct {
	RequestID    string
	PayloadBytes int
}

func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	diagnostics := &requestDiagnostics{RequestID: requestID}
	ctx = context.WithValue(ctx, requestDiagnosticsContextKey{}, diagnostics)
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func contextWithRequestBody(ctx context.Context, body []byte) context.Context {
	return context.WithValue(ctx, requestBodyContextKey{}, body)
}

func requestBodyFromContext(ctx context.Context) []byte {
	body, _ := ctx.Value(requestBodyContextKey{}).([]byte)
	return body
}

func diagnosticsFromContext(ctx context.Context) *requestDiagnostics {
	diagnostics, _ := ctx.Value(requestDiagnosticsContextKey{}).(*requestDiagnostics)
	return diagnostics
}

func payloadBytesFromContext(ctx context.Context) int {
	diagnostics := diagnosticsFromContext(ctx)
	if diagnostics == nil {
		return len(requestBodyFromContext(ctx))
	}
	return diagnostics.PayloadBytes
}

func requestIDFromRequest(req *stdhttp.Request) string {
	requestID := strings.TrimSpace(req.Header.Get(headerRequestID))
	if requestID == "" || len(requestID) > 128 {
		return newRequestID()
	}
	for _, char := range requestID {
		if char <= 32 || char == 127 {
			return newRequestID()
		}
	}
	return requestID
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "igw-request-id-unavailable"
	}
	return hex.EncodeToString(bytes[:])
}
