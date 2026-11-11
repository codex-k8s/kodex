package providercredential

import (
	"context"
	"errors"
)

// errCredentialMaterialization distinguishes failures after device login when account metadata or
// private credential bytes could not be materialized; wrapped private causes are not copied into public
// service failure codes.
// errCredentialMaterialization отличает отказы после device login при невозможности materialize account
// metadata или приватные credential байты; обёрнутые private причины не копируются в публичные service
// failure codes.
var errCredentialMaterialization = errors.New("Codex credential materialization failed")

// DeviceAuthorizationSession owns one local device login, exposes bounded verification metadata and
// yields private credential bytes only after completion; its caller must always close the session and
// clear returned bytes.
// DeviceAuthorizationSession владеет одним local device login, выдаёт ограниченные verification metadata
// и приватные credential байты только после завершения; вызывающий обязан закрыть сессию и обнулить
// возвращённые байты.
type DeviceAuthorizationSession interface {
	// MaterializerAttemptRef returns the stable server-derived reference used to correlate this local login
	// with durable attempt metadata.
	// MaterializerAttemptRef возвращает стабильную server-derived ссылку для сопоставления local login с
	// durable attempt metadata.
	MaterializerAttemptRef() string
	// VerificationURI returns the provider's validated verification metadata for the user to authorize this
	// session, not a general network-fetch capability.
	// VerificationURI возвращает проверенные verification metadata provider для авторизации этой сессии
	// пользователем, а не право произвольного сетевого чтения.
	VerificationURI() string
	// UserCode returns the short verification code for this pending login; it is not an OAuth access token
	// or stored credential JSON.
	// UserCode возвращает короткий verification код pending login; это не OAuth access token и не
	// сохранённый credential JSON.
	UserCode() string
	// Wait observes completion under the supplied context and returns private auth bytes plus a masked
	// account label; callers publish/clear those bytes and classify errors without exposing provider
	// payload.
	// Wait наблюдает завершение под переданным context и возвращает приватные auth байты с masked account
	// меткой; вызывающий публикует/обнуляет байты и классифицирует ошибки без раскрытия provider payload.
	Wait(context.Context) ([]byte, string, error)
	// Close joins session-owned process/readers and removes private state; implementations preserve the
	// first cleanup outcome for idempotent replay.
	// Close завершает принадлежащие сессии process/readers и удаляет приватное состояние; реализации
	// сохраняют первый cleanup исход для идемпотентного повтора.
	Close() error
}

// CredentialBackend separates runtime readiness and authorization lifecycle; owner
// authorization and durable effect reservation are supplied by higher-level services.
// CredentialBackend разделяет runtime readiness и lifecycle авторизации; owner
// авторизацию и durable резервирование эффекта обеспечивают вышележащие сервисы.
type CredentialBackend interface {
	// Check verifies the configured executable/state root and pinned CLI version using the caller's budget
	// without starting an authorization session.
	// Check проверяет настроенные executable/state root и закреплённую CLI версию с бюджетом caller без
	// запуска authorization сессии.
	Check(context.Context) error
	// StartDeviceAuthorization creates one isolated local login for server-derived attempt/home identifiers
	// and returns a session whose lifecycle must be owned and closed by the caller.
	// StartDeviceAuthorization создаёт один изолированный local login по server-derived attempt/home
	// идентификаторам и возвращает сессию, чей lifecycle и закрытие принадлежат вызывающему.
	StartDeviceAuthorization(context.Context, string, string) (DeviceAuthorizationSession, error)
}

// CredentialClient has no executable runtime or hidden authentication fallback; API keys use the configured provider descriptor.
// CredentialClient не содержит executable runtime или скрытый auth fallback; API keys используют configured provider descriptor.
type CredentialClient struct{}

// NewCredentialClient creates a credential-only dependency without starting a process or reading a home.
// NewCredentialClient создаёт credential-only dependency без запуска процесса или чтения home.
func NewCredentialClient() *CredentialClient { return &CredentialClient{} }

// Check validates the caller lifetime; storage and configured API observations have their own readiness checks.
// Check проверяет lifetime caller; storage и configured API observations имеют собственные readiness checks.
func (*CredentialClient) Check(ctx context.Context) error { return ctx.Err() }

// StartDeviceAuthorization rejects the removed CLI device flow before any provider request or persisted materialization.
// StartDeviceAuthorization отклоняет удалённый CLI device flow до provider request или сохранения materialization.
func (*CredentialClient) StartDeviceAuthorization(context.Context, string, string) (DeviceAuthorizationSession, error) {
	return nil, ErrInvalidInput
}
