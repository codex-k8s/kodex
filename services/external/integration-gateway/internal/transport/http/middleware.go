package httptransport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
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
			logger.Log(req.Context(), level, "integration-gateway http request",
				"request_id", requestIDFromContext(req.Context()),
				"method", req.Method,
				"path", req.URL.Path,
				"status", recorder.status,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"payload_bytes", payloadBytesFromContext(req.Context()),
			)
		})
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
