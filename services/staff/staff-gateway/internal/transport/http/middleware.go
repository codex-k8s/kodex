package httptransport

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type Middleware func(http.Handler) http.Handler

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx, requestID := requestContextWithID(req.Context(), req)
		w.Header()[headerRequestID] = []string{requestID}
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func TimeoutMiddleware(timeout time.Duration) Middleware {
	if timeout <= 0 {
		return identityMiddleware
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx, cancel := context.WithTimeout(req.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

func ActorContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if skipOpenAPIValidation(req.URL.Path) {
			next.ServeHTTP(w, req)
			return
		}
		if _, _, safeErr := actorPartsFromHeaders(req); safeErr != nil {
			WriteSafeError(w, req, safeErr)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func BodyLimitMiddleware(limit int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Body == nil || req.Body == http.NoBody {
				next.ServeHTTP(w, req)
				return
			}
			defer req.Body.Close()
			body, err := io.ReadAll(io.LimitReader(req.Body, limit+1))
			if err != nil {
				WriteSafeError(w, req, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "invalid request body", false))
				return
			}
			if int64(len(body)) > limit {
				WriteSafeError(w, req, NewSafeError(http.StatusRequestEntityTooLarge, CodeInvalidRequest, "payload is too large", false))
				return
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, req)
		})
	}
}

func LoggingMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			startedAt := time.Now()
			recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, req)
			level := slog.LevelInfo
			if recorder.status >= 500 || req.Context().Err() == context.DeadlineExceeded {
				level = slog.LevelWarn
			}
			logger.Log(req.Context(), level, "staff-gateway http request",
				"request_id", requestIDFromContext(req.Context()),
				"method", req.Method,
				"path", req.URL.Path,
				"status", recorder.status,
				"duration_ms", time.Since(startedAt).Milliseconds(),
			)
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestContextWithID(ctx context.Context, req *http.Request) (context.Context, string) {
	requestID := requestIDFromRequest(req)
	return contextWithRequestID(ctx, requestID), requestID
}

func identityMiddleware(next http.Handler) http.Handler {
	return next
}
