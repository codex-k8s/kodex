package oidcverifier

import (
	"context"
	"errors"
	"net"

	"github.com/codex-k8s/kodex/libs/go/observability/errorclass"
)

// keyRefreshFailure retains a fixed diagnostic and closed class while discarding transport messages and endpoint metadata.
// RU: keyRefreshFailure сохраняет фиксированную диагностику и закрытый класс, отбрасывая сообщения транспорта и метаданные endpoint.
type keyRefreshFailure struct {
	message string
	class   string
}

// Error preserves the historical public error text without exposing a native cause chain.
// RU: Error сохраняет прежний публичный текст ошибки без раскрытия исходной цепочки причин.
func (failure keyRefreshFailure) Error() string { return failure.message }

// refreshTransportClass classifies typed transport errors before redaction, without parsing messages or changing failure policy.
// RU: refreshTransportClass классифицирует типизированные ошибки транспорта до очистки без разбора сообщений и изменения политики отказа.
func refreshTransportClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	if class := errorclass.TLS(err); class != "unknown" {
		return class
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	var operation *net.OpError
	if errors.As(err, &operation) {
		return "network"
	}
	return "transport_unknown"
}

// SigningKeyFailureClass returns only the retained closed refresh class; it grants no token authority and does not change readiness.
// RU: SigningKeyFailureClass возвращает только сохранённый закрытый класс обновления; он не разрешает токены и не меняет readiness.
func (verifier *Verifier) SigningKeyFailureClass() string {
	if verifier == nil || verifier.keys == nil {
		return "unavailable"
	}
	verifier.keys.mutex.RLock()
	defer verifier.keys.mutex.RUnlock()
	return verifier.keys.lastFailureClass
}
