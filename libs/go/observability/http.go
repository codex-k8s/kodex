package observability

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type HTTPObserver func(route string, status int, started time.Time)

type httpStatusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *httpStatusWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *httpStatusWriter) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(value)
}

func (writer *httpStatusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (writer *httpStatusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("HTTP connection does not support hijacking")
	}
	writer.status = http.StatusSwitchingProtocols
	return hijacker.Hijack()
}

func (writer *httpStatusWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// HTTPMiddleware создаёт server span и передаёт только нормализованные route,
// method и status. URL, headers, cookies и bearer values не записываются.
func (runtime *Runtime) HTTPMiddleware(route func(string) string, observe HTTPObserver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		normalizedRoute := route(request.URL.Path)
		method := normalizeHTTPMethod(request.Method)
		ctx, span := runtime.tracer.Start(request.Context(), normalizedRoute, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", method), attribute.String("http.route", normalizedRoute)))
		captured := &httpStatusWriter{ResponseWriter: writer}
		defer func() {
			recovered := recover()
			status := captured.status
			if status == 0 {
				if recovered != nil {
					status = http.StatusInternalServerError
				} else {
					status = http.StatusOK
				}
			}
			span.SetAttributes(attribute.Int("http.response.status_code", status))
			if status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
			if observe != nil {
				observe(normalizedRoute, status, started)
			}
			if recovered != nil {
				err := errors.New("HTTP handler panic")
				span.RecordError(err)
				runtime.CaptureException(ctx, err)
				span.End()
				panic(recovered)
			}
			span.End()
		}()
		next.ServeHTTP(captured, request.WithContext(ctx))
	})
}

func normalizeHTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}
