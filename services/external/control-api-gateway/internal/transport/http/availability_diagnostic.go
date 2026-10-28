package httptransport

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/availability"
)

// unavailableRequestMessage is the only message for a final HTTP503 diagnostic; error text is never logged.
// unavailableRequestMessage — единственный текст диагностики итогового HTTP503; текст ошибки никогда не логируется.
const unavailableRequestMessage = "control API request unavailable"

// requestDiagnostic owns one request's closed failure metadata and the existing traced logger context.
// requestDiagnostic владеет закрытыми метаданными одного запроса и существующим контекстом логгера с трассировкой.
type requestDiagnostic struct {
	mu             sync.Mutex
	once           sync.Once
	ctx            context.Context
	logger         *slog.Logger
	route          string
	method         string
	stage          string
	class          string
	status         int
	detached       bool
	flushUncertain bool
}

// FlushError preserves controller capability discovery and tracks an implicit response only after the underlying flush.
// FlushError сохраняет определение возможностей контроллера и учитывает неявный ответ только после исходного flush.
func (writer *localizedResponseWriter) FlushError() error {
	err := http.NewResponseController(writer.ResponseWriter).Flush()
	writer.diagnostic.mu.Lock()
	defer writer.diagnostic.mu.Unlock()
	if err == nil && writer.diagnostic.status == 0 {
		writer.diagnostic.status = http.StatusOK
	} else if err != nil && !errors.Is(err, http.ErrNotSupported) {
		// A failed flush may already have committed headers; do not infer a later wire status.
		// Неудачный flush мог уже отправить заголовки; последующий сетевой статус нельзя предполагать.
		writer.diagnostic.flushUncertain = true
	}
	return err
}

// Hijack transfers connection ownership unchanged and suppresses later HTTP diagnostics only after successful transfer.
// Hijack передаёт владение соединением без изменений и подавляет дальнейшую HTTP-диагностику только после успешной передачи.
func (writer *localizedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(writer.ResponseWriter).Hijack()
	if err == nil {
		writer.diagnostic.mu.Lock()
		writer.diagnostic.detached = true
		writer.diagnostic.mu.Unlock()
	}
	return conn, buffer, err
}

// ObserveFailure keeps the first precise failure and normalizes even direct untrusted interface inputs.
// ObserveFailure сохраняет первый точный отказ и нормализует даже прямые недоверенные входы интерфейса.
func (writer *localizedResponseWriter) ObserveFailure(stage, class string) {
	writer.diagnostic.mu.Lock()
	defer writer.diagnostic.mu.Unlock()
	if availability.Stage(writer.diagnostic.stage) != "unknown" {
		return
	}
	writer.diagnostic.stage, writer.diagnostic.class = availability.Stage(stage), availability.Class(class)
}

// WriteHeader records the first actual response status while preserving the underlying writer's header semantics.
// WriteHeader записывает первый фактический статус ответа, сохраняя семантику заголовков исходного writer.
func (writer *localizedResponseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		writer.ResponseWriter.WriteHeader(status)
		return
	}
	writer.diagnostic.mu.Lock()
	if writer.diagnostic.status != 0 {
		writer.diagnostic.mu.Unlock()
		return
	}
	writer.diagnostic.status = status
	writer.diagnostic.mu.Unlock()
	writer.ResponseWriter.WriteHeader(status)
}

// Write accounts for implicit HTTP200 without retaining any response bytes or changing streaming ownership.
// Write учитывает неявный HTTP200 без сохранения байтов ответа и изменения владения потоком.
func (writer *localizedResponseWriter) Write(value []byte) (int, error) {
	writer.diagnostic.mu.Lock()
	if writer.diagnostic.status == 0 {
		writer.diagnostic.status = http.StatusOK
	}
	writer.diagnostic.mu.Unlock()
	return writer.ResponseWriter.Write(value)
}

// ObserveProblem reports one real HTTP503 with its public correlation and existing trace context; no observer exception or metric is added.
// ObserveProblem сообщает один фактический HTTP503 с публичным correlation и прежним trace-контекстом; exception observer и метрики не добавляются.
func (writer *localizedResponseWriter) ObserveProblem(status int, code, correlation string) {
	diagnostic := &writer.diagnostic
	diagnostic.mu.Lock()
	actual, stage, class := diagnostic.status, availability.Stage(diagnostic.stage), availability.Class(diagnostic.class)
	unobservable := diagnostic.detached || diagnostic.flushUncertain
	diagnostic.mu.Unlock()
	if unobservable || status != http.StatusServiceUnavailable || actual != status || diagnostic.logger == nil {
		return
	}
	if code != "UNAVAILABLE" && code != "STOPPING" {
		code = "OTHER"
	}
	diagnostic.once.Do(func() {
		diagnostic.logger.ErrorContext(diagnostic.ctx, unavailableRequestMessage,
			"route", diagnostic.route, "method", diagnostic.method, "code", code,
			"error_stage", stage, "error_class", class, "correlation_id", correlation)
	})
}

// diagnosticMethod limits log methods to standard verbs without retaining arbitrary caller strings.
// diagnosticMethod ограничивает методы логов стандартными глаголами без сохранения произвольных строк клиента.
func diagnosticMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}
