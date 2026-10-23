// Package errs содержит безопасные доменные ошибки STT.
package errs

import (
	"errors"
	"time"
)

var (
	// ErrInvalidRequest rejects an STT command whose validated input is incomplete or inconsistent.
	// ErrInvalidRequest отклоняет команду STT с неполным или противоречивым проверенным вводом.
	ErrInvalidRequest = errors.New("transcription request is invalid")
	// ErrAudioTooLarge marks an audio payload exceeding the configured byte budget.
	// ErrAudioTooLarge отмечает звуковой файл, превышающий настроенный лимит байтов.
	ErrAudioTooLarge = errors.New("audio payload exceeds the configured limit")
	// ErrAudioTooLong marks an audio recording exceeding the configured duration budget.
	// ErrAudioTooLong отмечает запись, превышающую настроенный предел длительности.
	ErrAudioTooLong = errors.New("audio duration exceeds the configured limit")
	// ErrUnsupportedAudio rejects a media format the transcription path cannot process safely.
	// ErrUnsupportedAudio отклоняет формат записи, который путь распознавания не может безопасно обработать.
	ErrUnsupportedAudio = errors.New("audio format is unsupported")
	// ErrPermissionDenied denotes a definitive denial by the transcription authority.
	// ErrPermissionDenied обозначает окончательный отказ полномочия на распознавание.
	ErrPermissionDenied = errors.New("transcription permission is denied")
	// ErrPolicyUnavailable denotes failure to obtain an authoritative transcription policy decision.
	// ErrPolicyUnavailable обозначает невозможность получить авторитетное решение политики распознавания.
	ErrPolicyUnavailable = errors.New("transcription policy is unavailable")
	// ErrCredentialUnavailable means the managed provider credential cannot be used for this turn.
	// ErrCredentialUnavailable означает недоступность управляемого реквизита провайдера для текущего хода.
	ErrCredentialUnavailable = errors.New("provider credential is unavailable")
	// ErrGrantRevoked rejects a revoked or outdated grant without treating it as retryable provider failure.
	// ErrGrantRevoked отклоняет отозванный или устаревший допуск без классификации как сбоя провайдера.
	ErrGrantRevoked = errors.New("transcription grant is revoked or stale")
	// ErrProviderUnavailable reports a temporary provider transport or service failure.
	// ErrProviderUnavailable сообщает о временной недоступности транспорта или сервиса провайдера.
	ErrProviderUnavailable = errors.New("transcription provider is unavailable")
	// ErrProviderRateLimited identifies provider throttling while hiding provider response details.
	// ErrProviderRateLimited обозначает ограничение частоты провайдером без раскрытия деталей ответа.
	ErrProviderRateLimited = errors.New("transcription provider rate limit exceeded")
	// ErrProviderRejected marks a provider-side rejection that is not a local permission grant.
	// ErrProviderRejected отмечает отказ провайдера, не изменяющий локальный допуск.
	ErrProviderRejected = errors.New("transcription provider rejected the request")
	// ErrDelegatedProofPending keeps the request closed until delegated authority proof is materialized.
	// ErrDelegatedProofPending закрывает запрос до материализации доказательства делегированного полномочия.
	ErrDelegatedProofPending = errors.New("delegated authorization proof is not materialized")
	// ErrEgressUnavailable distinguishes the required network route failure from a provider response.
	// ErrEgressUnavailable отличает отказ обязательного сетевого маршрута от ответа провайдера.
	ErrEgressUnavailable = errors.New("provider egress is unavailable")
)

// ProviderRateLimit carries only a validated retry delay, never the provider body;
// a zero RetryAfter means that no usable bounded hint was available.
// RU: ProviderRateLimit хранит только проверенную задержку повтора, никогда тело
// ответа провайдера; нулевой RetryAfter означает отсутствие пригодной подсказки.
type ProviderRateLimit struct{ RetryAfter time.Duration }

// Error returns the stable rate-limit sentinel text without exposing provider details.
// Error возвращает устойчивый текст ограничения частоты без деталей провайдера.
func (*ProviderRateLimit) Error() string { return ErrProviderRateLimited.Error() }

// Unwrap makes ProviderRateLimit detectable through errors.Is while retaining RetryAfter.
// Unwrap позволяет errors.Is распознать ограничение частоты, сохраняя RetryAfter.
func (*ProviderRateLimit) Unwrap() error { return ErrProviderRateLimited }
