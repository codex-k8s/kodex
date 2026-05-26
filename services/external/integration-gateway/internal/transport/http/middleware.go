package httptransport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"strings"
	"time"
)

// Middleware wraps an HTTP handler.
type Middleware func(stdhttp.Handler) stdhttp.Handler

// RequestIDMiddleware assigns a safe request id and returns it in the response.
func RequestIDMiddleware(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		requestID := requestIDFromRequest(req)
		w.Header().Set(headerRequestID, requestID)
		next.ServeHTTP(w, req.WithContext(contextWithRequestID(req.Context(), requestID)))
	})
}

// RouteDiagnosticsMiddleware derives non-sensitive route/source labels before body handling.
func RouteDiagnosticsMiddleware(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		setPathDiagnostics(req)
		next.ServeHTTP(w, req)
	})
}

// TimeoutMiddleware applies the edge request timeout to downstream handlers.
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
			if timeout <= 0 {
				next.ServeHTTP(w, req)
				return
			}
			ctx, cancel := context.WithTimeout(req.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// BodyCaptureMiddleware enforces the payload limit and keeps the raw body only in request memory.
func BodyCaptureMiddleware(limit int64) Middleware {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
			if req.Body == nil || req.Body == stdhttp.NoBody {
				if diagnostics := diagnosticsFromContext(req.Context()); diagnostics != nil {
					diagnostics.PayloadBytes = 0
				}
				next.ServeHTTP(w, req.WithContext(contextWithRequestBody(req.Context(), nil)))
				return
			}
			defer req.Body.Close()
			body, err := io.ReadAll(io.LimitReader(req.Body, limit+1))
			if err != nil {
				WriteSafeError(w, req, NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "invalid request body", false))
				return
			}
			if int64(len(body)) > limit {
				if diagnostics := diagnosticsFromContext(req.Context()); diagnostics != nil {
					diagnostics.PayloadBytes = len(body)
				}
				WriteSafeError(w, req, NewSafeError(stdhttp.StatusRequestEntityTooLarge, CodePayloadTooLarge, "payload is too large", false))
				return
			}
			if diagnostics := diagnosticsFromContext(req.Context()); diagnostics != nil {
				diagnostics.PayloadBytes = len(body)
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, req.WithContext(contextWithRequestBody(req.Context(), body)))
		})
	}
}

// LoggingMiddleware writes one redaction-safe structured log record per request.
func LoggingMiddleware(logger *slog.Logger) Middleware {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
			startedAt := time.Now()
			recorder := &responseRecorder{ResponseWriter: w, status: stdhttp.StatusOK}
			next.ServeHTTP(recorder, req)
			level := slog.LevelInfo
			if recorder.status >= 500 || errors.Is(req.Context().Err(), context.DeadlineExceeded) {
				level = slog.LevelWarn
			}
			duration := time.Since(startedAt)
			payloadSizeBucket := payloadSizeBucket(payloadBytesFromContext(req.Context()))
			routeID := diagnosticsString(req.Context(), func(d *requestDiagnostics) string { return d.RouteID })
			source := diagnosticsString(req.Context(), func(d *requestDiagnostics) string { return d.Source })
			rejectReason := diagnosticsString(req.Context(), func(d *requestDiagnostics) string { return d.RejectReason })
			logger.Log(req.Context(), level, "integration-gateway http request",
				"request_id", requestIDFromContext(req.Context()),
				"route_id", routeID,
				"source", source,
				"method", req.Method,
				"path", req.URL.Path,
				"status", recorder.status,
				"duration_ms", duration.Milliseconds(),
				"payload_size_bucket", payloadSizeBucket,
				"reject_reason", rejectReason,
			)
			recordEdgeSummary(edgeSummary{
				RouteID:           routeID,
				Source:            source,
				Status:            recorder.status,
				Latency:           duration,
				PayloadSizeBucket: payloadSizeBucket,
				RejectReason:      rejectReason,
			})
		})
	}
}

func setPathDiagnostics(req *stdhttp.Request) {
	path := strings.Trim(req.URL.Path, "/")
	segments := strings.Split(path, "/")
	if len(segments) != 3 || segments[0] != "v1" {
		return
	}
	switch segments[1] {
	case "provider-webhooks":
		setRouteDiagnostics(req, routeIDProviderWebhook, segments[2])
	case "external-callbacks":
		setRouteDiagnostics(req, routeIDExternalCallback, segments[2])
	}
}

func diagnosticsString(ctx context.Context, value func(*requestDiagnostics) string) string {
	diagnostics := diagnosticsFromContext(ctx)
	if diagnostics == nil {
		return ""
	}
	return value(diagnostics)
}

func payloadSizeBucket(size int) string {
	switch {
	case size <= 0:
		return "0"
	case size <= 1024:
		return "1-1KiB"
	case size <= 16*1024:
		return "1KiB-16KiB"
	case size <= 256*1024:
		return "16KiB-256KiB"
	case size <= 1024*1024:
		return "256KiB-1MiB"
	default:
		return ">1MiB"
	}
}

type responseRecorder struct {
	stdhttp.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
