// Package value содержит неизменяемые значения одного STT-запроса.
package value

import (
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"io"
	"time"
)

const (
	// PermissionTranscribe names the domain grant required before processing audio.
	// PermissionTranscribe обозначает доменный допуск, необходимый до обработки аудио.
	PermissionTranscribe = "stt.transcribe"
	// TransportPermissionTranscribe binds the protected RPC to its registered operation permission.
	// TransportPermissionTranscribe связывает защищённый RPC с зарегистрированным разрешением операции.
	TransportPermissionTranscribe = "platform.stt.transcribe"
	// PermissionManageConfiguration authorizes the separate system configuration catalog path.
	// PermissionManageConfiguration разрешает отдельный путь управления системным каталогом настроек.
	PermissionManageConfiguration = "system.configuration.manage"
	// ConfigurationCapability identifies the STT capability checked in effective configuration.
	// ConfigurationCapability обозначает возможность STT, проверяемую в действующей конфигурации.
	ConfigurationCapability = "platform.stt.use"
	// DefaultModel selects the model used by the local provider smoke operation.
	// DefaultModel выбирает модель для локальной проверки доступности провайдера.
	DefaultModel = "gpt-transcribe"
	// DefaultLanguage selects Russian for the local provider smoke operation.
	// DefaultLanguage выбирает русский язык для локальной проверки провайдера.
	DefaultLanguage = "ru"
	// MaximumAbsoluteBytes mirrors the shared model profile's per-audio byte limit.
	// MaximumAbsoluteBytes повторяет лимит байтов одного аудио из общего профиля модели.
	MaximumAbsoluteBytes int64 = modelprofile.MaximumAudioBytes
	// MaximumConcurrentStreams bounds simultaneous admitted transcription streams.
	// MaximumConcurrentStreams ограничивает число одновременно допущенных потоков распознавания.
	MaximumConcurrentStreams = 2
	// MaximumInflightBytes bounds all admitted audio bytes across concurrent streams.
	// MaximumInflightBytes ограничивает сумму байтов аудио всех одновременно допущенных потоков.
	MaximumInflightBytes int64 = MaximumAbsoluteBytes * MaximumConcurrentStreams
	// MaximumChunkBytes rejects an oversized gRPC chunk before it is accepted into the audio stream.
	// MaximumChunkBytes отклоняет чрезмерный фрагмент gRPC до добавления в поток аудио.
	MaximumChunkBytes = 64 << 10
)

// AuthorityProvenance pins the source, revision and digest of one verified identity claim.
// AuthorityProvenance закрепляет источник, ревизию и дайджест одного проверенного утверждения личности.
type AuthorityProvenance struct {
	Source       int32
	Reference    string
	Revision     uint64
	DigestSHA256 string
}

// Principal carries verified actor and scope identities with their authority provenance
// and expiry; caller-supplied IDs alone do not establish this principal.
// RU: Principal хранит проверенные личности субъекта и области с происхождением
// полномочий и сроком действия; одни ID из запроса не создают такой субъект.
type Principal struct {
	RPCProfile                   string
	ActorID, TenantID, ProjectID string
	Actor, Tenant, Project       AuthorityProvenance
	RequestID                    string
	Permission                   string
	AuthorityRevision            uint64
	AuthorityDigestSHA256        string
	ExpiresAt                    time.Time
}

// Audio describes one bounded seekable recording and the metadata used for validation.
// Audio описывает одну ограниченную запись с перемоткой и метаданные для проверки.
type Audio struct {
	Reader    io.ReadSeeker
	SizeBytes int64
	MediaType string
	FileName  string
	Duration  time.Duration
}

// Policy pins the effective model, limits and provider account to a versioned
// transcription configuration that expires independently of the request.
// RU: Policy закрепляет модель, лимиты и аккаунт провайдера за версионированной
// конфигурацией распознавания, срок которой не зависит от запроса.
type Policy struct {
	Revision                     uint64
	DigestSHA256                 string
	Model, Language              string
	Parameters                   modelprofile.Parameters
	MaximumAudioBytes            int64
	MaximumAudioDuration         time.Duration
	ProviderTimeout              time.Duration
	ProviderAccountRef           string
	ProviderCredentialGeneration uint64
	ExpiresAt                    time.Time
}

// Credential contains short-lived provider key bytes bound to an account generation
// and configuration digest; it is not part of a public response or audit payload.
// RU: Credential хранит краткоживущие байты ключа провайдера, связанные с поколением
// аккаунта и дайджестом конфигурации; они не входят в публичный ответ или аудит.
type Credential struct {
	APIKey                       []byte
	ProviderAccountRef           string
	ProviderCredentialGeneration uint64
	ConfigDigestSHA256           string
	ExpiresAt                    time.Time
}

// ProviderRequest combines admitted audio, effective model parameters and key bytes
// for the provider adapter without granting the provider any authority over policy.
// RU: ProviderRequest передаёт адаптеру допущенное аудио, действующие параметры модели
// и ключ, не передавая провайдеру право менять политику.
type ProviderRequest struct {
	Parameters modelprofile.Parameters
	Audio      Audio
	Model      string
	Language   string
	APIKey     []byte
}

// TranscriptionReceipt records the pinned authority and configuration provenance
// of a completed stage without copying audio or provider credential bytes.
// RU: TranscriptionReceipt фиксирует происхождение полномочий и конфигурации
// завершённого этапа без копирования аудио или байтов ключа провайдера.
type TranscriptionReceipt struct {
	RequestID, CorrelationID            string
	ActorID, TenantID, ProjectID        string
	AuthoritySourceRevision             uint64
	AuthoritySourceDigestSHA256         string
	ConfigRevision                      uint64
	ConfigDigestSHA256, Model, Language string
	ProviderAccountRef                  string
	ProviderCredentialGeneration        uint64
	CompletedStage                      Stage
}

// TranscriptionResult pairs recognized text with the receipt describing its verified inputs.
// TranscriptionResult связывает распознанный текст с квитанцией его проверенных входных данных.
type TranscriptionResult struct {
	Text    string
	Receipt TranscriptionReceipt
}

// Stage identifies the boundary that produced a transcription outcome;
// failed stages do not assert that the boundary completed successfully.
// RU: Stage обозначает границу, на которой возник исход распознавания;
// этап отказа не подтверждает успешное завершение этой границы.
type Stage string

const (
	// StageAuthority locates a caller or scope authority failure.
	// StageAuthority указывает на отказ проверки полномочий субъекта или области.
	StageAuthority Stage = "authority"
	// StagePolicy locates an effective-policy resolution or validation outcome.
	// StagePolicy указывает на исход получения или проверки действующей политики.
	StagePolicy Stage = "policy"
	// StageAudio locates audio format, size or duration validation failure.
	// StageAudio указывает на отказ проверки формата, размера или длительности аудио.
	StageAudio Stage = "audio"
	// StageCredential locates a managed credential projection or validation outcome.
	// StageCredential указывает на исход получения или проверки управляемого реквизита.
	StageCredential Stage = "credential"
	// StageEgress locates failure of the restricted provider network route.
	// StageEgress указывает на отказ ограниченного сетевого маршрута к провайдеру.
	StageEgress Stage = "egress"
	// StageProvider records the provider request phase without claiming a successful result.
	// StageProvider фиксирует фазу запроса к провайдеру, не утверждая успешный результат.
	StageProvider Stage = "provider"
	// StageSuccess marks a fully completed transcription with a result receipt.
	// StageSuccess обозначает полностью завершённое распознавание с квитанцией результата.
	StageSuccess Stage = "success"
	// StageUnknown is the closed fallback for an unrecognized or unavailable phase.
	// StageUnknown служит закрытым запасным значением для неизвестного или недоступного этапа.
	StageUnknown Stage = "unknown"
)

// ErrorClass groups safe failure outcomes without carrying provider response details.
// ErrorClass группирует безопасные исходы отказа без деталей ответа провайдера.
type ErrorClass string

const (
	// ErrorNone marks an operation for which no failure class is present.
	// ErrorNone отмечает операцию без класса отказа.
	ErrorNone ErrorClass = "none"
	// ErrorDenied represents a definitive authorization denial.
	// ErrorDenied обозначает окончательный отказ в полномочиях.
	ErrorDenied ErrorClass = "denied"
	// ErrorInvalid represents rejected local input or media validation.
	// ErrorInvalid обозначает отклонённый ввод или проверку аудио.
	ErrorInvalid ErrorClass = "invalid"
	// ErrorUnavailable represents a required dependency that cannot currently serve the request.
	// ErrorUnavailable обозначает временную недоступность обязательной зависимости.
	ErrorUnavailable ErrorClass = "unavailable"
	// ErrorTimeout represents exhaustion of a bounded operation deadline.
	// ErrorTimeout обозначает истечение ограниченного срока операции.
	ErrorTimeout ErrorClass = "timeout"
	// ErrorRejected represents a provider-side refusal distinct from local authorization.
	// ErrorRejected обозначает отказ провайдера, отличный от локального отказа в полномочиях.
	ErrorRejected ErrorClass = "rejected"
	// ErrorUnknown is the closed fallback when a failure cannot be safely classified.
	// ErrorUnknown служит закрытым запасным классом для отказа без безопасной классификации.
	ErrorUnknown ErrorClass = "unknown"
)
