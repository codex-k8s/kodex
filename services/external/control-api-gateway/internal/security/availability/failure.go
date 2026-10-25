// Package availability carries request failure metadata without changing authentication or retry decisions.
// Package availability переносит метаданные отказа запроса, не меняя аутентификацию либо решения о повторе.
package availability

import (
	"context"
	"errors"
	"fmt"
	"io"

	brokeravailability "github.com/codex-k8s/kodex/libs/go/eventing/availability"
	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
)

// failure preserves the original public error and closed metadata; the native diagnostic cause is discarded.
// failure сохраняет исходную публичную ошибку и закрытые метаданные; исходная диагностическая причина отбрасывается.
type failure struct {
	public error
	stage  string
	class  string
}

// Error never formats the retained public error or arbitrary upstream details.
// Error никогда не форматирует сохранённую публичную ошибку либо произвольные сведения upstream.
func (value *failure) Error() string {
	return "request unavailable: " + value.stage + "/" + value.class
}

// Format keeps debug output inside the same closed vocabulary as Error.
// Format ограничивает debug-вывод тем же закрытым словарём, что и Error.
func (value *failure) Format(state fmt.State, _ rune) { _, _ = io.WriteString(state, value.Error()) }

// Unwrap retains the preexisting public mapping, including expected authentication refusals.
// Unwrap сохраняет прежнее публичное отображение, включая ожидаемые отказы аутентификации.
func (value *failure) Unwrap() error { return value.public }

// At adds a stage without replacing an earlier precise stage or changing errors.Is behavior.
// At добавляет этап без замены прежнего точного этапа и без изменения errors.Is.
func At(stage string, err error) error {
	if err == nil {
		return nil
	}
	return WithCause(stage, err, err)
}

// WithCause classifies a discarded native cause while preserving a separately selected public error.
// WithCause классифицирует отбрасываемую исходную причину, сохраняя отдельно выбранную публичную ошибку.
func WithCause(stage string, public, cause error) error {
	var previous *failure
	if errors.As(public, &previous) {
		return public
	}
	return Fixed(stage, classify(cause), public)
}

// Fixed normalizes caller-supplied metadata and leaves the public error's identity unchanged through Unwrap.
// Fixed нормализует метаданные вызывающего и сохраняет идентичность публичной ошибки через Unwrap.
func Fixed(stage, class string, public error) error {
	if public == nil {
		return nil
	}
	return &failure{public: public, stage: Stage(stage), class: Class(class)}
}

// Describe returns only normalized metadata, treating errors without an explicit stage as unknown.
// Describe возвращает только нормализованные метаданные, считая неизвестным этап ошибки без явной отметки.
func Describe(err error) (string, string) {
	var value *failure
	if errors.As(err, &value) {
		return value.stage, value.class
	}
	return "unknown", classify(err)
}

// Stage maps every unregistered value to unknown, preventing identifiers from becoming log fields.
// Stage сводит каждое незарегистрированное значение к unknown, исключая идентификаторы из полей логов.
func Stage(value string) string {
	switch value {
	case "auth_revocation_read", "auth_family_read", "auth_oidc_verify", "session_renewal", "ticket_family_read", "ticket_ledger_read", "ticket_ledger_cas", "ticket_capacity", "ticket_cas_exhausted", "upstream_rpc", "gateway_stopping":
		return value
	default:
		return "unknown"
	}
}

// Class closes the log vocabulary independently of the upstream error type or message.
// Class замыкает словарь логов независимо от типа либо текста исходной ошибки.
func Class(value string) string {
	switch value {
	case "canceled", "deadline", "connection_closed", "disconnected", "no_responders", "timeout", "authorization", "cas_conflict", "record_invalid", "signing_keys_unavailable", "pending", "unavailable":
		return value
	default:
		return "unknown"
	}
}

// classify uses typed causes only and keeps native broker diagnostics before the general availability sentinel.
// classify использует только типизированные причины и сохраняет диагностику брокера до общего sentinel недоступности.
func classify(err error) string {
	if value := brokeravailability.Class(err); value != "unknown" {
		return Class(value)
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, oidcauth.ErrSigningKeysUnavailable):
		return "signing_keys_unavailable"
	case errors.Is(err, browserstate.ErrConflict):
		return "cas_conflict"
	case errors.Is(err, browserstate.ErrUnavailable):
		return "unavailable"
	default:
		return "unknown"
	}
}
