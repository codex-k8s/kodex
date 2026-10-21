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

// HTTPObserver receives the normalized route, captured response status and request start time after handler completion or panic unwinding.
// HTTPObserver получает нормализованный маршрут, записанный статус ответа и время начала после завершения обработчика или раскрутки panic.
type HTTPObserver func(route string, status int, started time.Time)

// httpStatusWriter wraps the response writer while tracking the first explicit or implicit status for telemetry.
// httpStatusWriter оборачивает запись ответа, сохраняя первый явный или неявный статус для телеметрии.
type httpStatusWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader records and forwards the first status only, ignoring subsequent header writes.
// WriteHeader сохраняет и передаёт только первый статус, игнорируя последующие записи заголовка.
func (writer *httpStatusWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

// Write records an implicit 200 when needed and delegates body bytes without retaining them for telemetry.
// Write при необходимости сохраняет неявный 200 и передаёт байты тела без сохранения для телеметрии.
func (writer *httpStatusWriter) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(value)
}

// Unwrap exposes the underlying response writer to standard response-controller capability discovery.
// Unwrap предоставляет исходный writer для поиска возможностей стандартным контроллером ответа.
func (writer *httpStatusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// Hijack requires underlying hijack support and records switching-protocols status before delegating connection ownership.
// Hijack требует поддержки перехвата исходным writer и записывает смену протокола перед передачей владения соединением.
func (writer *httpStatusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("HTTP connection does not support hijacking")
	}
	writer.status = http.StatusSwitchingProtocols
	return hijacker.Hijack()
}

// Flush forwards flushing only when supported by the underlying writer, without inspecting or buffering response data.
// Flush передаёт сброс только при поддержке исходным writer без просмотра и буферизации данных ответа.
func (writer *httpStatusWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// HTTPMiddleware uses the caller's route normalizer and a closed method set for server spans, reports status and a fixed panic error, then rethrows the original panic without recording request data.
// HTTPMiddleware использует нормализатор маршрута вызывающего и закрытый набор методов для server span, сообщает статус и фиксированную ошибку panic, затем повторяет исходную panic без записи данных запроса.
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

// normalizeHTTPMethod preserves only the explicitly registered HTTP verbs and maps every other token to one bounded OTHER value.
// normalizeHTTPMethod сохраняет лишь явно зарегистрированные HTTP-методы и сводит остальные токены к одному ограниченному OTHER.
func normalizeHTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}
