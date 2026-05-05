package grpcserver

import (
	"context"

	"google.golang.org/grpc/metadata"
)

const (
	// MetadataTraceID links logs, audit records and future distributed tracing spans.
	MetadataTraceID = "x-kodex-trace-id"
	// MetadataRequestID links one caller command to downstream service logs.
	MetadataRequestID = "x-kodex-request-id"
	// MetadataSessionID links a user or agent session when the caller can safely provide it.
	MetadataSessionID = "x-kodex-session-id"
	// MetadataRequestSource identifies the caller surface for diagnostics.
	MetadataRequestSource = "x-kodex-request-source"
)

type requestCorrelationContextKey struct{}

// RequestCorrelation is safe request metadata used in logs and diagnostics.
type RequestCorrelation struct {
	TraceID   string
	RequestID string
	SessionID string
	Source    string
}

// UnaryCorrelationInterceptor stores safe correlation metadata in the request context.
func UnaryCorrelationInterceptor() UnaryInterceptor {
	return func(ctx context.Context, req any, info *UnaryServerInfo, handler UnaryHandler) (any, error) {
		incoming, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return handler(ctx, req)
		}
		correlation := RequestCorrelation{
			TraceID:   firstMetadataValue(incoming, MetadataTraceID),
			RequestID: firstMetadataValue(incoming, MetadataRequestID),
			SessionID: firstMetadataValue(incoming, MetadataSessionID),
			Source:    firstMetadataValue(incoming, MetadataRequestSource),
		}
		if correlation.empty() {
			return handler(ctx, req)
		}
		return handler(ContextWithRequestCorrelation(ctx, correlation), req)
	}
}

// ContextWithRequestCorrelation stores safe correlation metadata in context.
func ContextWithRequestCorrelation(ctx context.Context, correlation RequestCorrelation) context.Context {
	return context.WithValue(ctx, requestCorrelationContextKey{}, correlation)
}

// RequestCorrelationFromContext returns request correlation metadata when the caller provided it.
func RequestCorrelationFromContext(ctx context.Context) (RequestCorrelation, bool) {
	correlation, ok := ctx.Value(requestCorrelationContextKey{}).(RequestCorrelation)
	return correlation, ok
}

// LogAttrsFromContext returns slog attributes that are safe to include in service logs.
func LogAttrsFromContext(ctx context.Context) []any {
	correlation, ok := RequestCorrelationFromContext(ctx)
	if !ok || correlation.empty() {
		return nil
	}
	attrs := make([]any, 0, 8)
	if correlation.TraceID != "" {
		attrs = append(attrs, "trace_id", correlation.TraceID)
	}
	if correlation.RequestID != "" {
		attrs = append(attrs, "request_id", correlation.RequestID)
	}
	if correlation.SessionID != "" {
		attrs = append(attrs, "session_id", correlation.SessionID)
	}
	if correlation.Source != "" {
		attrs = append(attrs, "request_source", correlation.Source)
	}
	return attrs
}

func (correlation RequestCorrelation) empty() bool {
	return correlation.TraceID == "" && correlation.RequestID == "" && correlation.SessionID == "" && correlation.Source == ""
}
