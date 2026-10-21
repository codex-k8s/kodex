package transcription

import (
	"context"
	"io"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// AudioDecoder measures a complete bounded recording and reports only local decoder readiness.
// AudioDecoder измеряет всю ограниченную запись и сообщает только о локальной готовности декодера.
type AudioDecoder interface {
	// Duration derives duration from decoded samples rather than trusting container headers.
	// Duration вычисляет длительность по декодированным сэмплам, не доверяя заголовкам контейнера.
	Duration(context.Context, io.ReadSeeker, int64, string, time.Duration) (time.Duration, error)
	// CheckLocal checks the decoder dependency without contacting the transcription provider.
	// CheckLocal проверяет зависимость декодера без обращения к провайдеру распознавания.
	CheckLocal(context.Context) error
}

// PolicyResolver projects the effective policy for a verified principal and exact request IDs.
// PolicyResolver получает действующую политику для проверенного субъекта и точных ID запроса.
type PolicyResolver interface {
	// Resolve returns versioned limits and provider binding for the current authority scope.
	// Resolve возвращает версионированные лимиты и привязку провайдера для текущей области полномочий.
	Resolve(context.Context, value.Principal, string, string) (value.Policy, error)
}

// CredentialProjector obtains a short-lived provider key only after policy and audio validation.
// CredentialProjector получает краткоживущий ключ провайдера только после проверки политики и аудио.
type CredentialProjector interface {
	// Project binds the managed credential to the principal, request and effective policy.
	// Project связывает управляемый реквизит с субъектом, запросом и действующей политикой.
	Project(context.Context, value.Principal, string, string, value.Policy) (value.Credential, error)
}

// Provider is the narrow model adapter; it cannot resolve policy or issue credentials.
// Provider служит узким адаптером модели и не может назначать политику либо выдавать реквизиты.
type Provider interface {
	// Transcribe sends only the validated provider request and returns untrusted text.
	// Transcribe передаёт только проверенный запрос провайдеру и возвращает недоверенный текст.
	Transcribe(context.Context, value.ProviderRequest) (string, error)
	// CheckLocal tests local adapter prerequisites without a billable provider request.
	// CheckLocal проверяет локальные предпосылки адаптера без платного запроса к провайдеру.
	CheckLocal(context.Context) error
	// CheckEgress checks the configured restricted route to the provider.
	// CheckEgress проверяет настроенный ограниченный маршрут к провайдеру.
	CheckEgress(context.Context) error
}

// Observer receives only bounded stage and error-class labels, never payloads or IDs.
// Observer получает только ограниченные метки этапа и класса ошибки без тела или ID.
type Observer interface {
	// Observe records one outcome using closed, low-cardinality labels.
	// Observe фиксирует один исход с закрытыми низкокардинальными метками.
	Observe(value.Stage, value.ErrorClass)
}

// ObserverFunc adapts a bounded observation callback to the Observer port.
// ObserverFunc адаптирует функцию ограниченной регистрации исхода к порту Observer.
type ObserverFunc func(value.Stage, value.ErrorClass)

// Observe delegates the closed stage and class to the wrapped callback.
// Observe передаёт закрытые этап и класс обёрнутой функции.
func (observe ObserverFunc) Observe(stage value.Stage, class value.ErrorClass) {
	observe(stage, class)
}
