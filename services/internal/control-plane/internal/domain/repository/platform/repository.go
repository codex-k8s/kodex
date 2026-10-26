// Package platform defines the control-plane domain's transport-neutral storage
// and work ports. Expected references, versions and descriptors do not establish
// caller authority without the domain/owner checks of the concrete operation.
// Пакет platform задаёт transport-neutral порты хранения и работы control-plane.
// Ожидаемые ссылки, версии и дескрипторы не доказывают полномочия вызывающего
// без доменных проверок и проверок владельца конкретной операции.
package platform

import (
	"context"
	"io"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// BootstrapState provides installation/onboarding state, organization and actor/role views, assistant
// readiness, project count and transcription availability; it is not a principal or evidence of a new
// bootstrap transaction.
// BootstrapState задаёт состояние установки и onboarding, организацию, исполнителя, роль, готовность
// помощника, количество проектов и доступность транскрипции; это не principal и не доказательство
// новой bootstrap-транзакции.
type BootstrapState struct {
	SpeechTranscription               entity.SpeechTranscriptionAvailability
	Bootstrapped, OnboardingCompleted bool
	OrganizationRef                   string
	Assistant                         entity.SystemAssistant
	ProjectCount                      int32
	Actor                             entity.User
	PlatformRole                      string
	NextActions                       []string
}

// Overview contains dashboard counts and selected active runs, pending gates and recent artifacts. The
// data shape does not enforce that every nested read came from one consistent storage snapshot.
// Overview содержит счётчики панели и выбранные активные запуски, ожидающие gate и недавние артефакты.
// Форма не обеспечивает чтение всех вложенных данных из одного согласованного снимка хранилища.
type Overview struct {
	ProjectCount, AgentCount, ActiveRunCount, PendingGateCount int32
	ActiveRuns                                                 []entity.Run
	PendingGates                                               []entity.OwnerGate
	RecentArtifacts                                            []entity.Artifact
}

// Administration groups an observed installation/core profile with assistant, optional adapter and
// incident views at ObservedAt; CoreReady is reported state, not an independent live readiness check.
// Administration объединяет наблюдаемый профиль установки и ядра с помощником, необязательными
// адаптерами и инцидентами на ObservedAt; CoreReady является сообщённым состоянием, не независимой
// live-проверкой готовности.
type Administration struct {
	Profile, CoreSummary string
	CoreReady            bool
	Assistant            entity.SystemAssistant
	OptionalAdapters     []entity.IntegrationDefinition
	Incidents            []entity.Incident
	ObservedAt           time.Time
}

// MaximumArtifactBytes is the 512 MiB artifact-size ceiling available to upload/policy adapters;
// declaring it does not bound a Reader automatically.
// MaximumArtifactBytes — предел размера артефакта 512 МиБ для адаптеров загрузки и политики;
// объявление само Reader не ограничивает.
const MaximumArtifactBytes int64 = 512 << 20

// ArtifactReader requires sequential, offset and seek reads so policy/storage can revisit one staged
// stream without copying all bytes. The interface alone guarantees neither file backing, a byte limit
// nor automatic Close.
// ArtifactReader требует последовательное, адресное и seek-чтение для повторного доступа политики и
// storage к подготовленному потоку без копирования всех байтов. Интерфейс сам не гарантирует file
// backing, предел размера или автоматический Close.
type ArtifactReader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

// ArtifactUpload supplies principal-independent file/run/project metadata, digest/size/scan-preview
// state and a rereadable stream to the upload boundary; ownership, limits and admission still require
// checks.
// ArtifactUpload передаёт независимые от principal метаданные файла, запуска и проекта, хеш, размер,
// scan-preview состояние и перечитываемый поток границе загрузки; владение, пределы и допуск всё ещё
// требуют проверки.
type ArtifactUpload struct {
	ProjectRef, RunRef, FileName, MediaType, Digest string
	ScanState, PreviewState                         string
	SizeBytes                                       int64
	Reader                                          ArtifactReader
}

// AgentAvatarUpload reuses artifact upload data while pinning the target agent and expected agent
// version, so avatar replacement is not an unversioned generic file write.
// AgentAvatarUpload переиспользует данные загрузки артефакта с точными целевым агентом и ожидаемой
// версией, чтобы замена аватара не становилась безверсионной общей записью файла.
type AgentAvatarUpload struct {
	ArtifactUpload
	AgentRef        string
	ExpectedVersion int64
}

// ArtifactDownload returns artifact metadata, a caller-owned closable byte stream and its grant
// reference; copying the record does not duplicate the stream or authorize another read.
// ArtifactDownload возвращает метаданные артефакта, закрываемый вызывающим поток байтов и ссылку
// grant; копия записи поток не дублирует и другое чтение не разрешает.
type ArtifactDownload struct {
	Artifact entity.Artifact
	Reader   io.ReadCloser
	GrantRef string
}

// ProofPrincipalInput carries an already authenticated external subject/session context or stable
// service identity with exact workload/operation/request/project selectors. External IDs and
// OwnerClaim still require owner resolution; browser payload cannot mint internal authority.
// ProofPrincipalInput несёт уже аутентифицированный внешний subject и контекст сессии либо устойчивую
// сервисную личность с точными workload, операцией, запросом и проектом. External IDs и OwnerClaim ещё
// требуют разрешения владельцем; browser payload внутренние полномочия не создаёт.
type ProofPrincipalInput struct {
	RPCProfile              string
	ExternalActorID         string
	ExternalTenantID        string
	ExternalDisplayName     string
	ExternalEmailHint       string
	ExternalIssuer          string
	ExternalGroups          []string
	ExternalSessionRevision uint64
	ExternalAuthenticatedAt time.Time
	ExternalACR             string
	ExternalAMR             []string
	OwnerClaim              bool
	CallerWorkload          string
	Operation               string
	RequestDigestSHA256     string
	ProjectRef              string
}

// ProofAuthority contains owner-resolved internal actor/organization/optional project UUIDs and
// versions plus optional runtime-execution provenance; opaque resource references remain locators
// rather than wire identity claims.
// ProofAuthority содержит разрешённые владельцем внутренние UUID и версии исполнителя, организации и
// необязательного проекта и optional runtime-execution происхождение; непрозрачные ссылки ресурсов
// остаются указателями, не wire identity claims.
type ProofAuthority struct {
	RuntimeExecution                   *RuntimeExecutionProof
	ActorID, OrganizationID, ProjectID string
	ActorVersion, OrganizationVersion  uint64
	ProjectVersion                     uint64
}

// WorkerGrantInput binds workload/optional instance and exact envelope digest to credential
// generation, grant revision and issued/expiry times for durable acceptance; the shape does not verify
// a signature or update a watermark.
// WorkerGrantInput связывает нагрузку, необязательный экземпляр и точный хеш конверта с поколением
// credentials, ревизией grant и сроками для устойчивого принятия; форма подпись не проверяет и
// watermark не меняет.
type WorkerGrantInput struct {
	WorkloadID           string
	InstanceID           string
	EnvelopeSHA256       string
	CredentialGeneration uint64
	Revision             uint64
	IssuedAt             time.Time
	ExpiresAt            time.Time
}

// RuntimeSecretPrepareInput describes the secret operation kind/scope/project, metadata/value type,
// expected content digest and mutation pins without embedding the secret's value bytes.
// RuntimeSecretPrepareInput описывает вид, область и проект операции секрета, метаданные, тип
// значения, ожидаемый хеш и привязки изменения без байтов значения секрета.
type RuntimeSecretPrepareInput struct {
	Purpose                                                                          string
	ScopeKind                                                                        string
	Kind, ProjectRef, SecretRef, Name, Description, ValueType, ExpectedContentSHA256 string
	Mutation                                                                         value.Mutation
}

// RuntimeSecretPrepareResult returns operation identity/state/expiry and either an opaque operation
// grant or optional terminal secret view. OperationGrant is sensitive bearer material, not safe
// diagnostic text.
// RuntimeSecretPrepareResult возвращает личность, состояние и срок операции и непрозрачный operation
// grant либо необязательное terminal view секрета. OperationGrant — чувствительный bearer-материал, не
// безопасная диагностика.
type RuntimeSecretPrepareResult struct {
	OperationRef, OperationGrant, State, ValueType, FailureCode string
	ExpiresAt                                                   time.Time
	TerminalSecret                                              *entity.RuntimeSecret
}

// RuntimeSecretCompleteInput pins completion to exact operation/claimant/generation and
// materialization evidence; it is not authority to substitute another secret object.
// RuntimeSecretCompleteInput закрепляет завершение с точными операцией, claimant, поколением и
// свидетельством материализации; права подставить другой объект секрета не даёт.
type RuntimeSecretCompleteInput struct {
	OperationRef, ClaimantID string
	ClaimGeneration          int64
	Materialization          *entity.RuntimeSecretMaterialization
}

// RuntimeSecretConsumeInput presents one opaque operation grant and claimant identity for owner-side
// consumption; the grant must not be logged or treated as a reusable unrestricted secret-read token.
// RuntimeSecretConsumeInput передаёт непрозрачный operation grant и claimant для потребления
// владельцем; grant нельзя журналировать или считать многоразовым неограниченным токеном чтения
// секретов.
type RuntimeSecretConsumeInput struct {
	OperationGrant, ClaimantID string
}

// RuntimeSecretFailInput binds a safe failure code to exact operation/claimant/generation, keeping
// terminal handling separate from completion materialization input.
// RuntimeSecretFailInput связывает безопасный код ошибки с точными операцией, claimant и поколением,
// отделяя terminal-обработку от ввода материализации завершения.
type RuntimeSecretFailInput struct {
	OperationRef, ClaimantID, FailureCode string
	ClaimGeneration                       int64
}

// ProviderCredentialCleanupTask carries pinned credential/authorization cleanup targets with exact
// task/account, attempt/generation/deadline and optional recovery identity; cleanup must target this
// snapshot, not a newer account credential.
// ProviderCredentialCleanupTask несёт закреплённые цели очистки credentials и авторизации с точными
// задачей, аккаунтом, попыткой, поколением, сроком и optional recovery identity; удаляется этот
// снимок, не более новый credential аккаунта.
type ProviderCredentialCleanupTask struct {
	Recovery        *entity.ProviderCleanupRecoveryIdentity
	Ref, AccountRef string
	Attempt         int32
	Generation      int64
	LeaseExpiresAt  time.Time
	Credential      entity.ProviderCredentialDescriptor
	TargetKind      string
	Authorization   entity.ProviderAuthorizationCleanupTarget
}

// ProviderCredentialCleanupResult reports cleanup task state, safe error, terminal receipt and whether
// retry was scheduled; constructing it does not commit deletion or prove an external effect.
// ProviderCredentialCleanupResult сообщает состояние задачи очистки, безопасную ошибку, terminal
// receipt и назначение повтора; создание значения удаление не фиксирует и внешний эффект не
// доказывает.
type ProviderCredentialCleanupResult struct {
	Ref, State, SafeErrorCode, TerminalReceipt string
	RetryScheduled                             bool
}

// RuntimeSecretFailureResult is the owner's operation/state/failure-code result after failure
// handling, distinct from raw provider or storage errors.
// RuntimeSecretFailureResult — результат владельца с операцией, состоянием и кодом после обработки
// отказа, отдельный от сырых ошибок провайдера или storage.
type RuntimeSecretFailureResult struct {
	OperationRef, State, FailureCode string
}

// RuntimeSecretRecoveryInput pins recovery to an operation and exact observed secret materialization
// instead of asking the owner to trust a current secret name alone.
// RuntimeSecretRecoveryInput закрепляет восстановление с операцией и точной наблюдаемой
// материализацией секрета, не предлагая владельцу доверять лишь текущему имени.
type RuntimeSecretRecoveryInput struct {
	OperationRef    string
	Materialization entity.RuntimeSecretMaterialization
}

// RuntimeSecretRecoveryResult carries the chosen recovery action, authoritative operation state and
// optional resulting secret view; it does not itself perform recovery.
// RuntimeSecretRecoveryResult несёт выбранное действие восстановления, авторитетное состояние операции
// и optional итоговое view секрета; сам восстановление не выполняет.
type RuntimeSecretRecoveryResult struct {
	Action, OperationState string
	Secret                 *entity.RuntimeSecret
}

// RuntimeSecretRecoveryPage bounds a recovery-work page with size/cursor, not permission to enumerate
// another tenant's operations.
// RuntimeSecretRecoveryPage ограничивает страницу восстановления размером и курсором, не выдавая права
// перечислять операции другого tenant.
type RuntimeSecretRecoveryPage struct {
	Size  int32
	Token string
}

// CredentialProjectionAuthority records the checked RPC profile, internal actor/tenant/project,
// proof/source/revision/credential pins, exact caller method and expiry needed by projection;
// populating these fields is not verification.
// CredentialProjectionAuthority фиксирует проверенный профиль RPC, внутренние actor, tenant и project,
// привязки proof, источника, ревизии и credential, точный caller method и срок для проекции;
// заполнение полей проверкой не является.
type CredentialProjectionAuthority struct {
	RPCProfile                               string
	ActorID, TenantID, ProjectID             string
	SourceDigestSHA256, ProofJTI             string
	CallerWorkloadID, CallerFullMethod       string
	SourceRevision, CallerCredentialRevision uint64
	ExpiresAt                                time.Time
}

// ProviderCredentialBinding pins account/logical credential revision to exact secret name/UID/resource
// version/content digest; it carries descriptors, not a raw provider token.
// ProviderCredentialBinding закрепляет аккаунт и логическую ревизию credential с точными именем, UID,
// resource version и хешем секрета; несёт дескрипторы, не сырой токен провайдера.
type ProviderCredentialBinding struct {
	AccountRef, CredentialRevisionRef            string
	SecretName, SecretUID, SecretResourceVersion string
	ContentSHA256                                string
	CredentialRevision                           int64
}

// RuntimeSecretProjectionBinding joins an environment-facing name/logical secret reference with its
// exact revision descriptor, preserving the distinction between logical selection and materialized
// object identity.
// RuntimeSecretProjectionBinding соединяет имя для окружения и логическую ссылку секрета с точным
// дескриптором ревизии, сохраняя различие выбора и личности материализованного объекта.
type RuntimeSecretProjectionBinding struct {
	Name, SecretRef string
	Descriptor      entity.RuntimeSecretRevisionDescriptor
}

// RuntimeCredentialProjectionInput binds checked authority to workload/lease/fence, exact
// runtime/session/turn/attempt/generation/input digest and optional expected provider/secret bindings.
// Resolve and Validate use different fence modes; this struct does not choose or authorize either.
// RuntimeCredentialProjectionInput связывает проверенные полномочия с workload, lease, fence, точными
// runtime, session, turn, attempt, generation, хешем ввода и optional ожидаемыми связями провайдера и
// секретов. Resolve и Validate используют разные режимы fence; структура их не выбирает и не
// разрешает.
type RuntimeCredentialProjectionInput struct {
	Authority                                 CredentialProjectionAuthority
	WorkloadInstance, LeaseRef, Fence         string
	RuntimeRevisionRef, RuntimeRevisionDigest string
	SessionRef, TurnRef, InputDigest          string
	Generation                                int64
	Attempt                                   int32
	ProviderCredential                        ProviderCredentialBinding
	RuntimeSecrets                            []RuntimeSecretProjectionBinding
}

// RuntimeCredentialProjection returns bounded-lifetime authority and exact provider/runtime-secret
// descriptors after owner resolution; it contains no decrypted credential or secret value bytes.
// RuntimeCredentialProjection возвращает ограниченные по сроку полномочия и точные дескрипторы
// провайдера и runtime-секретов после разрешения владельцем; расшифрованных credentials или байтов
// значений не содержит.
type RuntimeCredentialProjection struct {
	Authority          CredentialProjectionAuthority
	ProviderCredential ProviderCredentialBinding
	RuntimeSecrets     []RuntimeSecretProjectionBinding
	ExpiresAt          time.Time
}

// TranscriptionCredentialProjectionInput binds checked authority to exact provider account/credential
// generation and STT configuration revision/digest, independent of a generic runtime lease projection.
// TranscriptionCredentialProjectionInput связывает проверенные полномочия с точными аккаунтом,
// поколением credentials и ревизией и хешем STT-конфигурации отдельно от общей runtime lease
// projection.
type TranscriptionCredentialProjectionInput struct {
	Authority                    CredentialProjectionAuthority
	ProviderAccountRef           string
	ProviderCredentialGeneration uint64
	ConfigRevision               uint64
	ConfigDigestSHA256           string
}

// TranscriptionCredentialProjection returns only the exact provider credential descriptor and its
// projection expiry, not audio, a provider token or an authorization to send arbitrary requests.
// TranscriptionCredentialProjection возвращает лишь точный дескриптор credentials провайдера и срок
// проекции, не аудио, токен провайдера или право произвольных запросов.
type TranscriptionCredentialProjection struct {
	ProviderCredential ProviderCredentialBinding
	ExpiresAt          time.Time
}

// Repository is the domain-owned persistence/work port for scoped reads, specialized
// lifecycle/projection commands and the existing typed command dispatcher. Signatures define
// inputs/results, not authentication, atomicity or security guarantees by themselves; implementations
// and domain callers retain those checks.
// Repository — доменный порт хранения и работы для scoped-чтения, специализированных lifecycle и
// projection команд и существующего типизированного диспетчера. Сигнатуры задают ввод и результат, не
// аутентификацию, атомарность или security-гарантии сами по себе; проверки остаются у реализаций и
// доменных вызывающих.
type Repository interface {
	// GetExecutionWorkflowCatalog requests the execution-visible workflow catalog for an exact principal
	// and typed execution context, not a tenant-wide unfiltered definition dump.
	// GetExecutionWorkflowCatalog запрашивает доступный исполнению каталог процессов для точных principal
	// и контекста, не неограниченную выдачу определений tenant.
	GetExecutionWorkflowCatalog(context.Context, value.Principal, query.ExecutionWorkflowCatalog) (entity.ExecutionWorkflowCatalog, error)
	// GetEmailMailboxConfiguration resolves the addressed connection/configuration/revision into an
	// owner-scoped mailbox view without returning raw credential values.
	// GetEmailMailboxConfiguration разрешает заданные подключение, конфигурацию и ревизию в mailbox view
	// области владельца без сырых значений credentials.
	GetEmailMailboxConfiguration(context.Context, value.Principal, string, string, string) (entity.EmailMailboxConfigurationView, error)
	// ListEmailMailboxConfigurations pages mailbox configuration views under the supplied principal and
	// connection selectors; page parameters do not widen ownership.
	// ListEmailMailboxConfigurations возвращает страницы mailbox-конфигураций по principal и выбранному
	// подключению; параметры страницы владение не расширяют.
	ListEmailMailboxConfigurations(context.Context, value.Principal, string, string, query.Page) (entity.EmailMailboxPage, error)
	// ListEmailMailboxCredentials pages named credential candidates for one connection/kind with
	// total/cursor metadata, not credential bytes or unrestricted secret enumeration.
	// ListEmailMailboxCredentials возвращает страницы именованных кандидатов credentials подключения и
	// вида с количеством и курсором, не байты credentials или неограниченный список секретов.
	ListEmailMailboxCredentials(context.Context, value.Principal, string, string, query.Page) ([]entity.EmailMailboxCredential, int64, string, error)
	// GetEmailMailboxCredentialReceipt resolves one named credential receipt inside a connection boundary
	// so exact secret provenance can be used instead of a free-form key.
	// GetEmailMailboxCredentialReceipt разрешает receipt именованного credential внутри границы
	// подключения для точного происхождения секрета вместо произвольного ключа.
	GetEmailMailboxCredentialReceipt(context.Context, value.Principal, string, string) (entity.EmailMailboxCredential, error)
	// PreviewEmailMailboxConfiguration checks proposed format/content against the scoped connection and
	// returns canonical preview/diagnostics; preview is not publication or a mail send.
	// PreviewEmailMailboxConfiguration проверяет предлагаемые формат и текст относительно подключения
	// области и возвращает канонический preview и диагностику; preview не является публикацией или
	// отправкой почты.
	PreviewEmailMailboxConfiguration(context.Context, value.Principal, string, string, string) (entity.EmailMailboxPreview, error)
	// ReportEmailConfigurationReadback records an exact revision/digest callback from the email-bridge
	// reporting principal; it is separate from preparing or publishing mailbox configuration.
	// ReportEmailConfigurationReadback фиксирует callback точных ревизии и хеша от principal отчёта
	// email-bridge отдельно от подготовки и публикации конфигурации ящика.
	ReportEmailConfigurationReadback(context.Context, value.Principal, int64, string) error
	// GetRuntimeSecretDraft reads a scoped secret draft descriptor without exposing its value bytes
	// through the ordinary metadata port.
	// GetRuntimeSecretDraft читает scoped-дескриптор черновика секрета без выдачи байтов значения через
	// обычный metadata port.
	GetRuntimeSecretDraft(context.Context, value.Principal, string) (entity.RuntimeSecretDraft, error)
	// PrepareRuntimeSecretDraftImpact creates the versioned impact proposal for an exact draft under
	// mutation pins, without implying that its consumers have already been changed.
	// PrepareRuntimeSecretDraftImpact создаёт версионное предложение влияния точного черновика под
	// привязками изменения, не означая уже выполненного изменения потребителей.
	PrepareRuntimeSecretDraftImpact(context.Context, value.Principal, string, value.Mutation) (entity.RuntimeSecretDraftImpactPlan, error)
	// GetRuntimeSecretDraftImpact pages the addressed secret-draft impact plan and consumer search in the
	// principal scope; reading a plan does not apply it.
	// GetRuntimeSecretDraftImpact возвращает страницу заданного плана влияния черновика секрета и поиска
	// потребителей в области principal; чтение план не применяет.
	GetRuntimeSecretDraftImpact(context.Context, value.Principal, string, string, query.Page) (entity.RuntimeSecretDraftImpactPage, error)
	// PrepareRuntimeSecretDraft prepares the specialized draft operation and its receipt using typed
	// version/digest inputs; it does not accept arbitrary secret content through this metadata call.
	// PrepareRuntimeSecretDraft подготавливает специализированную операцию черновика и receipt по
	// типизированным версиям и хешам; произвольное содержимое секрета через metadata-вызов не принимает.
	PrepareRuntimeSecretDraft(context.Context, value.Principal, RuntimeSecretDraftPrepareInput) (entity.RuntimeSecretDraftOperationReceipt, error)
	// ConsumeRuntimeSecretDraft resolves/claims the exact prepared draft work through its typed work
	// binding rather than treating a draft reference as materialization authority.
	// ConsumeRuntimeSecretDraft разрешает или захватывает точную подготовленную работу черновика по
	// типизированной привязке, не считая ссылку полномочием материализации.
	ConsumeRuntimeSecretDraft(context.Context, value.Principal, RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftWork, error)
	// FinishRuntimeSecretDraft reports exact draft-work completion to the owner and returns its
	// authoritative result; a worker's local success alone is not a committed draft transition.
	// FinishRuntimeSecretDraft сообщает владельцу завершение точной работы черновика и возвращает
	// авторитетный результат; локальный успех worker сам не является зафиксированным переходом.
	FinishRuntimeSecretDraft(context.Context, value.Principal, RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftResult, error)
	// ListRuntimeSecretDraftRecovery pages owner-visible draft recovery work for reconciliation,
	// preserving exact work identities instead of discovering secrets by name.
	// ListRuntimeSecretDraftRecovery возвращает страницы доступной владельцу работы восстановления
	// черновиков для согласования, сохраняя точные личности задач вместо поиска секретов по имени.
	ListRuntimeSecretDraftRecovery(context.Context, value.Principal, query.Page) ([]entity.RuntimeSecretDraftWork, string, error)
	// CheckRuntimeSecretDraftWork checks the scoped draft-work boundary for its worker principal; it is
	// not a claim, completion or generic secret-read operation.
	// CheckRuntimeSecretDraftWork проверяет scoped-границу работы черновиков для worker principal; это не
	// захват, завершение или общее чтение секрета.
	CheckRuntimeSecretDraftWork(context.Context, value.Principal) error
	// GetRuntimeRevisionPublicPair reads the public revision pair selected by exact run/revision
	// references under principal eligibility, keeping the optional comparison view separate from private
	// runtime materialization.
	// GetRuntimeRevisionPublicPair читает пару публичных ревизий по точным ссылкам запуска и ревизии в
	// допустимости principal, отделяя optional comparison view от приватной runtime-материализации.
	GetRuntimeRevisionPublicPair(context.Context, value.Principal, string, string) (entity.RuntimeRevisionPublicProjection, *entity.RuntimeRevisionPublicProjection, error)
	// GetEmailEffectReceipt reads the addressed invocation's owner receipt/decision view, rather than
	// inferring whether a mail effect occurred from chat text.
	// GetEmailEffectReceipt читает view receipt и решения владельца заданного вызова, не выводя совершение
	// почтового эффекта из текста чата.
	GetEmailEffectReceipt(context.Context, value.Principal, string) (entity.EmailEffectReceiptView, error)
	// ResolveEmailAuthorization resolves the typed exact mail-operation request against owner policy,
	// resource scope and decision state before a transport may execute it.
	// ResolveEmailAuthorization разрешает точный типизированный запрос почтовой операции по политике,
	// области ресурса и состоянию решения владельца до выполнения транспортом.
	ResolveEmailAuthorization(context.Context, value.Principal, query.EmailAuthorization) (entity.EmailAuthorization, error)
	// ResolveEmailReconciliation binds receipt, decision, external reference and digest to the principal's
	// reconciliation read; it is not authorization to retry an uncertain send.
	// ResolveEmailReconciliation связывает receipt, решение, внешнюю ссылку и хеш с reconciliation-чтением
	// principal; разрешением повторить неопределённую отправку не является.
	ResolveEmailReconciliation(context.Context, value.Principal, string, string, string, string) (entity.EmailEffectReceiptView, error)
	// GetMemoryRecord reads one memory record inside principal eligibility, keeping resource lookup
	// distinct from an execution grant or prompt injection.
	// GetMemoryRecord читает запись памяти в области допустимости principal, отделяя поиск ресурса от
	// grant выполнения или внедрения в промпт.
	GetMemoryRecord(context.Context, value.Principal, string) (entity.KodexMemoryRecord, error)
	// GetSkillBundle resolves a scoped skill bundle view; the bundle reference alone is not permission to
	// load or execute its files.
	// GetSkillBundle разрешает scoped-view пакета навыка; ссылка сама не разрешает загрузку или исполнение
	// его файлов.
	GetSkillBundle(context.Context, value.Principal, string) (entity.SkillBundle, error)
	// ListSkillBundles returns a filtered, counted and cursor-paged bundle catalog eligible for the
	// principal, not arbitrary repository or host files.
	// ListSkillBundles возвращает фильтрованный каталог пакетов с количеством и курсором, допустимый
	// principal, не произвольные файлы репозитория или хоста.
	ListSkillBundles(context.Context, value.Principal, query.Filter) ([]entity.SkillBundle, int64, string, error)
	// ListSkillBundleRevisions pages versioned history of one scoped bundle with total/cursor, preserving
	// revision selection instead of mutable latest-only lookup.
	// ListSkillBundleRevisions возвращает страницы версионной истории scoped-пакета с количеством и
	// курсором, сохраняя выбор ревизии вместо только изменяемого latest.
	ListSkillBundleRevisions(context.Context, value.Principal, string, query.Page) ([]entity.SkillBundleRevision, int64, string, error)
	// ListMemoryRecords returns the principal-filtered memory catalog and total/cursor metadata without
	// granting access to every stored record.
	// ListMemoryRecords возвращает каталог памяти с фильтром principal, количеством и курсором без доступа
	// ко всем хранимым записям.
	ListMemoryRecords(context.Context, value.Principal, query.Filter) ([]entity.KodexMemoryRecord, int64, string, error)
	// ListMemoryRecordRevisions reads the scoped record's revision history with bounded pagination, not a
	// new write or memory activation.
	// ListMemoryRecordRevisions читает историю ревизий scoped-записи с ограниченной пагинацией, не
	// выполняя запись или активацию памяти.
	ListMemoryRecordRevisions(context.Context, value.Principal, string, query.Page) ([]entity.MemoryRecordRevision, int64, string, error)
	// GetRuntimeEnvironmentDraft resolves one owner-visible environment draft while keeping draft
	// selection separate from its published runtime version.
	// GetRuntimeEnvironmentDraft разрешает доступный владельцу черновик окружения, отделяя его выбор от
	// опубликованной runtime-версии.
	GetRuntimeEnvironmentDraft(context.Context, value.Principal, string) (entity.RuntimeEnvironmentDraft, error)
	// GetRuntimeSecretImpact reads consumers/blockers for an exact secret reference/revision with
	// search/page controls; impact visibility does not permit rotation or deletion.
	// GetRuntimeSecretImpact читает потребителей и препятствия точных секрета и ревизии с поиском и
	// страницей; видимость влияния не разрешает ротацию или удаление.
	GetRuntimeSecretImpact(context.Context, value.Principal, string, int64, string, query.Page) (entity.RuntimeSecretImpact, error)
	// GetRuntimeEnvironmentImpact reads the selected environment/version's consumer impact under
	// principal/search/page constraints, not an automatic rebind decision.
	// GetRuntimeEnvironmentImpact читает влияние выбранных окружения и версии на потребителей под
	// ограничениями principal, поиска и страницы, не принимая автоматическое решение о перепривязке.
	GetRuntimeEnvironmentImpact(context.Context, value.Principal, string, string, string, query.Page) (entity.RuntimeEnvironmentImpact, error)
	// ListInteractionIdentities pages identities associated with the addressed interaction source in the
	// principal scope; display identities do not authenticate a new message.
	// ListInteractionIdentities возвращает страницы личностей заданного источника взаимодействия в области
	// principal; отображаемые личности новое сообщение не аутентифицируют.
	ListInteractionIdentities(context.Context, value.Principal, string, query.Page) ([]entity.InteractionIdentity, string, error)
	// Bootstrap initializes or reconciles installation-owned state before normal serving; it has no
	// browser principal and is not a generic user command or implicit migration API.
	// Bootstrap инициализирует или согласует состояние установки до обычной работы; browser principal не
	// принимает и общей пользовательской командой или неявным migration API не является.
	Bootstrap(context.Context) error
	// ResolveProofAuthority maps authenticated subject/workload/operation input to owner-resolved internal
	// identity versions and optional execution provenance; request IDs or owner claims are not copied as
	// authority by contract.
	// ResolveProofAuthority переводит аутентифицированные subject, workload и операцию в разрешённые
	// владельцем внутренние личности, версии и optional execution provenance; ID запроса и owner claims по
	// контракту не копируются как полномочия.
	ResolveProofAuthority(context.Context, ProofPrincipalInput) (ProofAuthority, error)
	// ResolveServiceCredentialGeneration reads the currently registered generation for an exact workload,
	// rejecting missing/invalid registration rather than trusting a caller's claimed generation.
	// ResolveServiceCredentialGeneration читает текущее зарегистрированное поколение точной нагрузки,
	// отклоняя отсутствующую или неверную регистрацию вместо доверия заявленному поколению.
	ResolveServiceCredentialGeneration(context.Context, string) (uint64, error)
	// AcceptWorkerGrant submits the exact verified worker envelope/instance/generation/revision/times to
	// durable acceptance; cryptographic verification remains outside the data input and replay state
	// belongs to the owner.
	// AcceptWorkerGrant передаёт точные проверенные envelope, экземпляр, поколение, ревизию и сроки
	// нагрузки устойчивому принятию; криптографическая проверка вне структуры, а состояние повторов
	// принадлежит владельцу.
	AcceptWorkerGrant(context.Context, WorkerGrantInput) error
	// NextAuthorityProofRevision requests the next persisted, bounded proof revision from the owner, not a
	// caller-selected or process-local counter.
	// NextAuthorityProofRevision запрашивает следующую устойчивую ограниченную ревизию proof у владельца,
	// не выбранный вызывающим или локальный счётчик.
	NextAuthorityProofRevision(context.Context) (uint64, error)
	// ResolvePrincipal resolves the supplied verified identity inside authoritative organization/actor
	// state before domain use; it is not permission to construct identity from browser payload.
	// ResolvePrincipal разрешает переданную проверенную личность внутри авторитетного состояния
	// организации и исполнителя до доменного использования; права строить личность из browser payload не
	// даёт.
	ResolvePrincipal(context.Context, value.Principal) (value.Principal, error)
	// GetBootstrapState reads installation/onboarding and actor/role/assistant views for the resolved
	// principal, without running bootstrap itself.
	// GetBootstrapState читает установку, onboarding, исполнителя, роль и помощника для разрешённого
	// principal, не выполняя bootstrap.
	GetBootstrapState(context.Context, value.Principal) (BootstrapState, error)
	// GetPlatformEventCursor returns the resolved organization reference and current platform sequence for
	// realtime initialization, not an event publication or a tenant selector from payload.
	// GetPlatformEventCursor возвращает разрешённую организацию и текущую последовательность платформы для
	// realtime-инициализации, не публикацию события или tenant из payload.
	GetPlatformEventCursor(context.Context, value.Principal) (string, int64, error)
	// GetOverview assembles scoped dashboard counters and selected activity queues for an optional
	// project; the port does not by itself promise one transaction for all constituent reads.
	// GetOverview собирает scoped-счётчики панели и выбранные очереди активности необязательного проекта;
	// порт сам единую транзакцию всех составляющих чтений не обещает.
	GetOverview(context.Context, value.Principal, string) (Overview, error)
	// ListCapabilities reads the principal-visible platform capability catalog; advertised capability
	// metadata does not create a runnable integration grant.
	// ListCapabilities читает доступный principal каталог возможностей платформы; метаданные возможности
	// исполняемый grant интеграции не создают.
	ListCapabilities(context.Context, value.Principal) ([]entity.IntegrationCapability, error)
	// GetAgentEffectiveCapabilities resolves capability eligibility for the exact agent/workflow/stage
	// context and filter, rather than treating configured capability names as current grants.
	// GetAgentEffectiveCapabilities разрешает допустимость возможностей точных агента, процесса и этапа с
	// фильтром, не считая настроенные имена текущими grants.
	GetAgentEffectiveCapabilities(context.Context, value.Principal, string, string, string, query.Filter) (entity.AgentEffectiveCapabilities, error)
	// ListArtifactBindingTargets queries eligible binding targets for an artifact under the
	// principal/filter boundary, without creating a binding or broadening artifact access.
	// ListArtifactBindingTargets запрашивает допустимые цели связи артефакта под principal и фильтром, не
	// создавая связь и не расширяя доступ к артефакту.
	ListArtifactBindingTargets(context.Context, value.Principal, string, query.Filter) (entity.ArtifactBindingTargets, error)
	// GetRunAttachmentEligibility evaluates the addressed project/target/run attachment context for the
	// principal, not eligibility inferred from an attachment's display name.
	// GetRunAttachmentEligibility оценивает контекст вложений заданных проекта, цели и запуска для
	// principal, не допустимость по отображаемому имени вложения.
	GetRunAttachmentEligibility(context.Context, value.Principal, string, entity.RunTarget, string) (entity.RunAttachmentEligibility, error)
	// ListRuntimes reads selectable runtime summaries in principal scope; Ready flags are views, not
	// reserved capacity or newly authorized provider sessions.
	// ListRuntimes читает краткие выбираемые runtime в области principal; Ready-флаги являются view, не
	// резервом ёмкости или новой разрешённой сессией провайдера.
	ListRuntimes(context.Context, value.Principal) ([]entity.RuntimeSelection, error)
	// Search returns scoped cross-resource results with total/cursor under the typed filter, preserving
	// resource eligibility rather than providing a raw global index scan.
	// Search возвращает scoped-результаты разных ресурсов с количеством и курсором по типизированному
	// фильтру, сохраняя допустимость ресурсов вместо сырого глобального индекса.
	Search(context.Context, value.Principal, query.Filter) ([]entity.SearchResult, int64, string, error)
	// ListVFSNodes pages/counts logical filesystem entries under principal and typed filters, without
	// exposing unrestricted host-directory traversal.
	// ListVFSNodes возвращает страницы и количества записей логической файловой системы под principal и
	// фильтром без неограниченного обхода каталогов хоста.
	ListVFSNodes(context.Context, value.Principal, query.Filter) ([]entity.VFSNode, int64, string, error)
	// SearchVFS searches the eligible logical file projection with total/cursor metadata; matching a path
	// does not bypass file lifecycle or content-read checks.
	// SearchVFS ищет в допустимой логической проекции файлов с количеством и курсором; совпадение пути
	// жизненный цикл и проверки чтения содержимого не обходит.
	SearchVFS(context.Context, value.Principal, query.Filter) ([]entity.VFSNode, int64, string, error)
	// ListProjects returns the principal's filtered project page, continuation token and available catalog
	// actions, not all tenant rows regardless of membership.
	// ListProjects возвращает страницу проектов principal с фильтром, курсором и доступными действиями
	// каталога, не все строки tenant независимо от членства.
	ListProjects(context.Context, value.Principal, query.Filter) ([]entity.Project, string, []string, error)
	// ListTrashedProjects pages projects visible in the principal's trash boundary, distinct from
	// active-project listing or irreversible purge.
	// ListTrashedProjects возвращает страницы проектов в доступной principal корзине отдельно от активного
	// каталога и безвозвратной очистки.
	ListTrashedProjects(context.Context, value.Principal, query.Page) ([]entity.Project, string, error)
	// GetProjectPurgeRecovery resolves the addressed project's purge/recovery view inside owner
	// eligibility; a reference alone does not authorize destructive repair.
	// GetProjectPurgeRecovery разрешает view очистки и восстановления заданного проекта в допустимости
	// владельца; ссылка сама разрушительное исправление не разрешает.
	GetProjectPurgeRecovery(context.Context, value.Principal, string) (entity.ProjectPurgeRecovery, error)
	// GetProject reads one eligible project by reference under the principal, not an unscoped lookup that
	// grants ownership through the identifier.
	// GetProject читает допустимый проект по ссылке под principal, не unscoped-поиск с выдачей владения
	// через идентификатор.
	GetProject(context.Context, value.Principal, string) (entity.Project, error)
	// ListPlatformMemberships pages organization/platform membership views for the principal and typed
	// filter, distinct from project-specific membership.
	// ListPlatformMemberships возвращает страницы организационного или платформенного членства для
	// principal и фильтра отдельно от членства конкретного проекта.
	ListPlatformMemberships(context.Context, value.Principal, query.Filter) ([]entity.Membership, string, error)
	// ListPlatformMembershipCandidates queries eligible users for platform membership selection; appearing
	// in the candidate list does not assign a role.
	// ListPlatformMembershipCandidates запрашивает допустимых пользователей для выбора платформенного
	// членства; наличие в списке роль не выдаёт.
	ListPlatformMembershipCandidates(context.Context, value.Principal, query.Filter) ([]entity.User, string, error)
	// ListMemberships pages project-scoped membership views using the principal and typed filter, not a
	// write to membership or permissions.
	// ListMemberships возвращает страницы проектного членства по principal и фильтру, не изменяя членство
	// или права.
	ListMemberships(context.Context, value.Principal, query.Filter) ([]entity.Membership, string, error)
	// ListMembershipCandidates pages users eligible for the selected project's membership workflow,
	// without creating a binding by search alone.
	// ListMembershipCandidates возвращает страницы допустимых пользователей для членства выбранного
	// проекта без создания связи одним поиском.
	ListMembershipCandidates(context.Context, value.Principal, query.Filter) ([]entity.User, string, error)
	// ListAgents pages eligible agent views under project/search/state filters and principal scope,
	// preserving the difference between catalog visibility and execution authority.
	// ListAgents возвращает страницы допустимых агентов под фильтрами проекта, поиска, состояния и
	// principal, сохраняя различие видимости каталога и полномочий исполнения.
	ListAgents(context.Context, value.Principal, query.Filter) ([]entity.Agent, string, error)
	// GetAgent resolves one agent's owner-visible view by exact reference, including its versioned
	// configuration/instruction metadata rather than issuing a runtime grant.
	// GetAgent разрешает доступное владельцу view точного агента с версионными метаданными настроек и
	// инструкций, не выдавая runtime grant.
	GetAgent(context.Context, value.Principal, string) (entity.Agent, error)
	// GetEffectivePromptTemplate reads the exact agent's effective instruction/template version inside
	// principal scope, not an arbitrary template path or user-selected system prompt.
	// GetEffectivePromptTemplate читает действующую версию инструкции или шаблона точного агента внутри
	// области principal, не произвольный путь шаблона или выбранный пользователем system prompt.
	GetEffectivePromptTemplate(context.Context, value.Principal, string) (entity.InstructionVersion, error)
	// GetPromptMaterializationSnapshot resolves the selected target kind/reference and its prompt
	// dependencies into an owner snapshot; rendering and execution remain separate from this read.
	// GetPromptMaterializationSnapshot разрешает выбранные вид и ссылку цели и зависимости промпта в
	// снимок владельца; rendering и исполнение остаются отдельными от чтения.
	GetPromptMaterializationSnapshot(context.Context, value.Principal, string, string) (entity.PromptMaterializationSnapshot, error)
	// GetPromptPreviewContextSnapshot resolves preview context for the selected target kind/reference and
	// typed agent/workflow context, without executing that target.
	// GetPromptPreviewContextSnapshot разрешает preview-контекст выбранных вида, ссылки цели и
	// типизированного agent/workflow контекста без исполнения цели.
	GetPromptPreviewContextSnapshot(context.Context, value.Principal, string, string, query.PromptPreviewContext) (entity.PromptMaterializationSnapshot, error)
	// GetSchedulePromptPreviewSnapshot evaluates exact draft/current schedule input, expected version,
	// occurrence time and preview mode, returning context pin and variable metadata without creating an
	// occurrence.
	// GetSchedulePromptPreviewSnapshot оценивает точные draft или current ввод расписания, ожидаемую
	// версию, время события и режим preview, возвращая контекст и метаданные переменных без создания
	// события.
	GetSchedulePromptPreviewSnapshot(context.Context, value.Principal, command.ScheduleInput, string, int64, time.Time, string) (entity.PromptMaterializationSnapshot, entity.SchedulePromptPreviewPin, []entity.TemplateVariable, error)
	// ListPromptContextVariables pages the variable catalog for an exact eligible prompt context; catalog
	// availability is not permission to substitute unverified variable values.
	// ListPromptContextVariables возвращает страницы каталога переменных точного допустимого контекста
	// промпта; доступность каталога не разрешает подставить непроверенные значения.
	ListPromptContextVariables(context.Context, value.Principal, query.Filter) (entity.PromptVariableCatalog, error)
	// GetAgentRuntimeConfiguration reads the agent's configuration/overlay/environment/context binding
	// view under owner eligibility, not decrypted secrets or a mutable runtime process.
	// GetAgentRuntimeConfiguration читает view конфигурации, overlay, окружения и контекстных связей
	// агента в допустимости владельца, не расшифрованные секреты или изменяемый runtime-процесс.
	GetAgentRuntimeConfiguration(context.Context, value.Principal, string) (entity.AgentRuntimeConfigurationView, error)
	// ListConfigOverlayRevisions pages counted overlay history under the typed principal/resource filter,
	// preserving exact revision selection for inspection.
	// ListConfigOverlayRevisions возвращает страницы истории overlay с количеством под principal и
	// фильтром ресурса, сохраняя точный выбор ревизии для просмотра.
	ListConfigOverlayRevisions(context.Context, value.Principal, query.Filter) ([]entity.ConfigOverlayVersion, int64, string, error)
	// GetConfigOverlayRevision reads one addressed overlay revision for the scoped configuration/agent,
	// without changing publication or effective runtime state.
	// GetConfigOverlayRevision читает заданную ревизию overlay scoped-конфигурации или агента без
	// изменения публикации и действующего runtime.
	GetConfigOverlayRevision(context.Context, value.Principal, string, string) (entity.ConfigOverlayVersion, error)
	// ListAgentRuntimeConfigurations pages immutable-version views for the selected agent/filter, not an
	// unversioned overwrite of its active model/account policy.
	// ListAgentRuntimeConfigurations возвращает страницы версионных views выбранного агента или фильтра,
	// не безверсионную перезапись активной модели и политики аккаунтов.
	ListAgentRuntimeConfigurations(context.Context, value.Principal, query.Filter) ([]entity.AgentRuntimeConfiguration, string, error)
	// ListRuntimeEnvironments returns the eligible environment catalog and cursor for principal/filter,
	// keeping published versions and readiness in their owner views.
	// ListRuntimeEnvironments возвращает допустимый каталог окружений и курсор для principal и фильтра,
	// сохраняя опубликованные версии и готовность в views владельца.
	ListRuntimeEnvironments(context.Context, value.Principal, query.Filter) ([]entity.RuntimeEnvironmentSet, string, error)
	// GetRuntimeEnvironment resolves the exact environment view inside principal scope, not a direct
	// Kubernetes or secret-object read.
	// GetRuntimeEnvironment разрешает точное view окружения внутри области principal, не выполняя прямое
	// чтение Kubernetes или объекта секрета.
	GetRuntimeEnvironment(context.Context, value.Principal, string) (entity.RuntimeEnvironmentSet, error)
	// ListRuntimeEnvironmentVersions pages exact environment version snapshots for the selected filter
	// without switching the published/current version.
	// ListRuntimeEnvironmentVersions возвращает страницы точных версионных снимков окружения по фильтру
	// без переключения опубликованной или текущей версии.
	ListRuntimeEnvironmentVersions(context.Context, value.Principal, query.Filter) ([]entity.RuntimeEnvironmentVersion, string, error)
	// GetRuntimeEnvironmentReadiness returns observed readiness/blockers bound to the environment's exact
	// published version/digest, not automatic admission on catalog presence.
	// GetRuntimeEnvironmentReadiness возвращает наблюдаемые готовность и препятствия, связанные с точными
	// опубликованными версией и хешем окружения, не автоматический допуск по наличию в каталоге.
	GetRuntimeEnvironmentReadiness(context.Context, value.Principal, string) (entity.RuntimeEnvironmentReadiness, error)
	// ListRuntimeEnvironmentAgents pages agents bound to the selected environment within principal scope
	// so consumers can inspect impact without reassigning bindings.
	// ListRuntimeEnvironmentAgents возвращает страницы агентов выбранного окружения в области principal
	// для просмотра влияния без изменения связей.
	ListRuntimeEnvironmentAgents(context.Context, value.Principal, query.Filter) ([]entity.Agent, string, error)
	// ListRuntimeSecrets pages logical secret metadata under principal/filter eligibility, not a catalog
	// of decrypted values or arbitrary Kubernetes Secrets.
	// ListRuntimeSecrets возвращает страницы логических метаданных секретов под principal и фильтром, не
	// каталог расшифрованных значений или произвольных Kubernetes Secrets.
	ListRuntimeSecrets(context.Context, value.Principal, query.Filter) ([]entity.RuntimeSecret, string, error)
	// GetRuntimeSecret reads the addressed logical secret descriptor/version in owner scope without
	// exposing value bytes through this metadata method.
	// GetRuntimeSecret читает дескриптор и версию заданного логического секрета в области владельца без
	// выдачи байтов значения через metadata-метод.
	GetRuntimeSecret(context.Context, value.Principal, string) (entity.RuntimeSecret, error)
	// PrepareRuntimeSecretOperation prepares a scoped, versioned secret operation and expiring grant or
	// terminal result; it is separate from worker consumption and materialization completion.
	// PrepareRuntimeSecretOperation подготавливает scoped-версионную операцию секрета и expiring grant
	// либо terminal-результат отдельно от потребления worker и завершения материализации.
	PrepareRuntimeSecretOperation(context.Context, value.Principal, RuntimeSecretPrepareInput) (RuntimeSecretPrepareResult, error)
	// ListRuntimeSecretRecoveryWork pages exact owner recovery tasks with bounded size/cursor rather than
	// permitting workers to discover or delete secrets by loose names.
	// ListRuntimeSecretRecoveryWork возвращает страницы точных задач восстановления владельца с
	// ограниченными размером и курсором вместо поиска или удаления секретов worker по неточным именам.
	ListRuntimeSecretRecoveryWork(context.Context, value.Principal, RuntimeSecretRecoveryPage) ([]entity.RuntimeSecretRecoveryWork, string, error)
	// ConsumeRuntimeSecretOperation presents the operation grant/claimant to the owner for controlled work
	// consumption and returns the exact operation binding, not unrestricted secret access.
	// ConsumeRuntimeSecretOperation передаёт владельцу operation grant и claimant для управляемого
	// потребления работы и возвращает точную привязку операции, не неограниченный доступ к секретам.
	ConsumeRuntimeSecretOperation(context.Context, value.Principal, RuntimeSecretConsumeInput) (entity.RuntimeSecretOperation, error)
	// CompleteRuntimeSecretOperation finishes the exact claimed operation using generation and
	// materialization evidence, returning the resulting logical secret metadata after owner acceptance.
	// CompleteRuntimeSecretOperation завершает точную захваченную операцию по поколению и свидетельству
	// материализации, возвращая логические метаданные секрета после принятия владельцем.
	CompleteRuntimeSecretOperation(context.Context, value.Principal, RuntimeSecretCompleteInput) (entity.RuntimeSecret, error)
	// FailRuntimeSecretOperation reports a safe failure against exact operation/claimant/generation and
	// returns the owner's resulting state, not a generic cancellation of unrelated work.
	// FailRuntimeSecretOperation сообщает безопасную ошибку точной операции, claimant и поколения и
	// возвращает итоговое состояние владельца, не отменяя произвольную чужую работу.
	FailRuntimeSecretOperation(context.Context, value.Principal, RuntimeSecretFailInput) (RuntimeSecretFailureResult, error)
	// RecoverRuntimeSecretMaterialization reconciles the exact operation and observed materialization into
	// an owner action/state/optional secret result; recovery input alone is not proof of success.
	// RecoverRuntimeSecretMaterialization согласует точные операцию и наблюдаемую материализацию в
	// действие, состояние и optional secret result владельца; ввод восстановления сам успех не доказывает.
	RecoverRuntimeSecretMaterialization(context.Context, value.Principal, RuntimeSecretRecoveryInput) (RuntimeSecretRecoveryResult, error)
	// ResolveRuntimeCredentialProjection resolves current provider/secret descriptors for the exact
	// authorized execution tuple with nonempty fence, returning bounded expiry rather than credential
	// bytes.
	// ResolveRuntimeCredentialProjection разрешает текущие дескрипторы провайдера и секретов точного
	// разрешённого исполнения с непустым fence, возвращая ограниченный срок, не байты credentials.
	ResolveRuntimeCredentialProjection(context.Context, value.Principal, RuntimeCredentialProjectionInput) (RuntimeCredentialProjection, error)
	// ValidateRuntimeCredentialProjection uses the distinct validation authority and empty-fence mode to
	// compare submitted descriptors with current owner state; expected denial/conflict becomes false, not
	// a new materialization grant.
	// ValidateRuntimeCredentialProjection использует отдельные полномочия проверки и режим пустого fence
	// для сравнения дескрипторов с текущим состоянием владельца; ожидаемый отказ или конфликт даёт false,
	// не новый grant материализации.
	ValidateRuntimeCredentialProjection(context.Context, value.Principal, RuntimeCredentialProjectionInput) (bool, error)
	// ResolveTranscriptionCredentialProjection resolves provider descriptors for exact STT
	// configuration/account/generation and checked authority, with an expiry rather than provider secret
	// bytes.
	// ResolveTranscriptionCredentialProjection разрешает дескрипторы провайдера точных STT-конфигурации,
	// аккаунта, поколения и проверенных полномочий со сроком вместо байтов секрета.
	ResolveTranscriptionCredentialProjection(context.Context, value.Principal, TranscriptionCredentialProjectionInput) (TranscriptionCredentialProjection, error)
	// ListTemplateVariables pages/counts template-variable metadata under the typed principal/context
	// filter; it does not materialize arbitrary secret-bearing values.
	// ListTemplateVariables возвращает страницы и количество метаданных переменных шаблона под principal и
	// фильтром контекста; произвольные значения с секретами не материализует.
	ListTemplateVariables(context.Context, value.Principal, query.Filter) ([]entity.TemplateVariable, int64, string, error)
	// ListProviderDefinitions pages the principal-visible provider catalog and readiness metadata, not
	// account credentials or a successful external-provider check.
	// ListProviderDefinitions возвращает страницы доступного principal каталога провайдеров и готовности,
	// не credentials аккаунтов или результат успешной внешней проверки.
	ListProviderDefinitions(context.Context, value.Principal, query.Filter) ([]entity.ProviderDefinition, string, error)
	// ListModelCapabilities resolves the model catalog for exact provider-definition/account selectors and
	// filter, preserving catalog revision/digest/status rather than inventing supported models.
	// ListModelCapabilities разрешает каталог моделей точных провайдера, аккаунта и фильтра, сохраняя
	// ревизию, хеш и состояние вместо выдуманных поддерживаемых моделей.
	ListModelCapabilities(context.Context, value.Principal, string, string, query.Filter) (entity.ModelCatalog, error)
	// ListManagedConfigurationHistory reads a scoped configuration header and paged revision history with
	// total/cursor, preserving the distinction between current metadata and historical source.
	// ListManagedConfigurationHistory читает scoped-заголовок конфигурации и страницы истории ревизий с
	// количеством и курсором, отделяя текущие метаданные от исторического исходника.
	ListManagedConfigurationHistory(context.Context, value.Principal, string, query.Page) (entity.ManagedConfigurationSet, []entity.ManagedConfigurationRevision, int64, string, error)
	// ListManagedConfigurations pages/counts eligible managed configuration sets using typed filters;
	// catalog visibility does not authorize edits to Git-owned source.
	// ListManagedConfigurations возвращает страницы и количества допустимых управляемых конфигураций по
	// фильтрам; видимость каталога редактирование Git-owned исходника не разрешает.
	ListManagedConfigurations(context.Context, value.Principal, query.Filter) ([]entity.ManagedConfigurationSet, int64, string, error)
	// GetManagedConfigurationImpact resolves consumers of an exact configuration/target revision under
	// principal/filter scope, without applying the proposed change.
	// GetManagedConfigurationImpact разрешает потребителей точных конфигурации и целевой ревизии под
	// principal и фильтром без применения изменения.
	GetManagedConfigurationImpact(context.Context, value.Principal, string, string, query.Filter) (entity.ManagedConfigurationImpact, error)
	// GetRoleImageImpactPlan reads the addressed image impact plan and searched/paged consumer outcomes in
	// owner scope, not execution of a rebind batch.
	// GetRoleImageImpactPlan читает заданный план влияния образа и найденные на странице результаты
	// потребителей в области владельца, не исполняя batch перепривязки.
	GetRoleImageImpactPlan(context.Context, value.Principal, string, string, query.Page) (entity.RoleImageImpactPage, error)
	// GetRevisionImpactPlan reads an addressed revision-impact plan page with search and cursor context,
	// keeping inspection separate from applying its outcomes.
	// GetRevisionImpactPlan читает страницу заданного плана влияния ревизии с поиском и курсором, отделяя
	// просмотр от применения результатов.
	GetRevisionImpactPlan(context.Context, value.Principal, string, string, query.Page) (entity.RevisionImpactPage, error)
	// GetEffectiveManagedConfiguration resolves the exact configuration kind and consumer kind/reference
	// to a version-pinned binding snapshot, not a caller-asserted absence or a loose latest revision.
	// GetEffectiveManagedConfiguration разрешает точные вид конфигурации и вид и ссылку потребителя в
	// версионный снимок связи, не заявленное отсутствие или неточную latest revision.
	GetEffectiveManagedConfiguration(context.Context, value.Principal, string, string, string) (entity.ManagedConfigurationBindingSnapshot, error)
	// GetSystemSTTConfiguration reads the principal-scoped effective STT revision, model/account/policy
	// limits and readiness; it neither records audio nor performs transcription.
	// GetSystemSTTConfiguration читает действующие STT-ревизию, модель, аккаунт, пределы политики и
	// готовность области principal; аудио не записывает и транскрипцию не выполняет.
	GetSystemSTTConfiguration(context.Context, value.Principal) (entity.SystemSTTConfiguration, error)
	// GetProviderFinancialOverview reads owner-visible provider usage/credit/eligibility summaries;
	// reported balances are not a reservation or an authorization to execute a model call.
	// GetProviderFinancialOverview читает доступные владельцу сводки использования, кредитов и
	// допустимости провайдеров; сообщённый баланс не является резервом или разрешением вызова модели.
	GetProviderFinancialOverview(context.Context, value.Principal) (entity.ProviderFinancialOverview, error)
	// ListProviderAccounts pages eligible account views with cursor/catalog actions and masked
	// authorization state, not raw provider credentials.
	// ListProviderAccounts возвращает страницы допустимых аккаунтов с курсором, действиями каталога и
	// скрытым состоянием авторизации, не сырые credentials провайдера.
	ListProviderAccounts(context.Context, value.Principal, query.Filter) ([]entity.ProviderAccount, string, []string, error)
	// ListProviderAccountBlockers queries typed deletion/usage blockers for the selected account in
	// principal scope, without overriding active references or forcing deletion.
	// ListProviderAccountBlockers запрашивает типизированные препятствия удаления или использования
	// выбранного аккаунта в области principal без обхода активных ссылок или принудительного удаления.
	ListProviderAccountBlockers(context.Context, value.Principal, query.ProviderAccountBlockers) (entity.ProviderAccountBlockerPage, error)
	// GetProviderAccount resolves one account's owner-visible versioned state and lifecycle metadata, not
	// a login, refresh or credential disclosure.
	// GetProviderAccount разрешает доступные владельцу версионное состояние и жизненный цикл аккаунта, не
	// выполняя вход, refresh или раскрытие credentials.
	GetProviderAccount(context.Context, value.Principal, string) (entity.ProviderAccount, error)
	// GetProviderAccountWithUsage augments the exact account view with optional execution-context usage
	// metadata; the optional context does not widen account eligibility.
	// GetProviderAccountWithUsage дополняет точное view аккаунта optional использованием контекста
	// выполнения; контекст допустимость аккаунта не расширяет.
	GetProviderAccountWithUsage(context.Context, value.Principal, string, *entity.ProviderAccountUsageContext) (entity.ProviderAccount, error)
	// ListRoleImageRecipeRevisions pages recipe revision/provenance metadata under the typed owner filter,
	// not mutable tags or a new admission result.
	// ListRoleImageRecipeRevisions возвращает страницы ревизий и происхождения рецепта под фильтром
	// владельца, не изменяемые теги или новый результат допуска.
	ListRoleImageRecipeRevisions(context.Context, value.Principal, query.Filter) ([]entity.RoleImageRecipeRevision, string, error)
	// ListWorkflows pages eligible workflow views under the principal/filter boundary, keeping
	// catalog/readiness metadata distinct from a submitted run.
	// ListWorkflows возвращает страницы допустимых процессов под principal и фильтром, отделяя каталог и
	// готовность от отправленного запуска.
	ListWorkflows(context.Context, value.Principal, query.Filter) ([]entity.Workflow, string, error)
	// GetWorkflow reads one scoped workflow's draft/published/version view by reference, without
	// publishing it or selecting an unpinned execution revision.
	// GetWorkflow читает draft, published и version view scoped-процесса по ссылке, не публикуя его и не
	// выбирая незакреплённую ревизию исполнения.
	GetWorkflow(context.Context, value.Principal, string) (entity.Workflow, error)
	// ListRuns returns filtered owner-visible runs with total/cursor and their permitted action views, not
	// arbitrary execution histories outside the principal scope.
	// ListRuns возвращает найденные доступные владельцу запуски с количеством, курсором и действиями, не
	// произвольную историю вне области principal.
	ListRuns(context.Context, value.Principal, query.Filter) ([]entity.Run, int64, string, error)
	// GetRun resolves one eligible run with its current owner state and exact session/lineage/version
	// metadata; a reference is not execution authority.
	// GetRun разрешает допустимый запуск с текущим состоянием владельца и точными сессией, происхождением
	// и версией; ссылка полномочиями выполнения не является.
	GetRun(context.Context, value.Principal, string) (entity.Run, error)
	// GetRunGraph reads the eligible root graph and addressed run together without the auxiliary run
	// collection requested by GetRunGraphSnapshot.
	// GetRunGraph читает допустимые корневой граф и заданный запуск вместе без вспомогательной коллекции
	// запусков, запрашиваемой GetRunGraphSnapshot.
	GetRunGraph(context.Context, value.Principal, string) (entity.Run, entity.RunGraph, error)
	// GetRunGraphSnapshot requests the consistent graph/run projection including auxiliary runs for
	// realtime bootstrap/rejoin; graph revision and event sequence remain owner-derived.
	// GetRunGraphSnapshot запрашивает согласованную проекцию графа и запусков с вспомогательными runs для
	// realtime bootstrap/rejoin; ревизия графа и последовательность событий назначаются владельцем.
	GetRunGraphSnapshot(context.Context, value.Principal, string) (entity.Run, entity.RunGraph, error)
	// ListRunEvents reads events after the requested sequence inside eligible root-run scope, returning
	// the authoritative graph sequence and completion flag rather than a guessed count.
	// ListRunEvents читает события после запрошенной последовательности в допустимой области корневого
	// запуска, возвращая авторитетную последовательность графа и признак завершённости страницы, не
	// выдуманное количество.
	ListRunEvents(context.Context, value.Principal, query.Filter) ([]entity.RunEvent, int64, bool, error)
	// ListOwnerGates pages/counts gates by scoped project/state/search filters, preserving owner-derived
	// allowed decisions instead of treating visibility as approval authority.
	// ListOwnerGates возвращает страницы и количества gate по scoped-фильтрам проекта, состояния и поиска,
	// сохраняя разрешённые владельцем решения вместо права подтверждения по видимости.
	ListOwnerGates(context.Context, value.Principal, query.Filter) ([]entity.OwnerGate, int64, string, error)
	// GetOwnerGate resolves the addressed human gate and its consequence/intent context for the principal;
	// reading it does not record a decision.
	// GetOwnerGate разрешает заданный human gate с последствиями и контекстом намерения для principal;
	// чтение решения не фиксирует.
	GetOwnerGate(context.Context, value.Principal, string) (entity.OwnerGate, error)
	// ListArtifacts pages/counts eligible artifact metadata using lifecycle/project/search filters, not
	// binary content or storage locations.
	// ListArtifacts возвращает страницы и количества допустимых метаданных артефактов с lifecycle, project
	// и search фильтрами, не бинарное содержимое или storage locations.
	ListArtifacts(context.Context, value.Principal, query.Filter) ([]entity.Artifact, int64, string, error)
	// GetArtifact reads scoped artifact metadata/current revision and action state; content admission and
	// grant-backed byte access are separate operations.
	// GetArtifact читает scoped-метаданные артефакта, текущую ревизию и действия; допуск содержимого и
	// чтение байтов по grant являются отдельными операциями.
	GetArtifact(context.Context, value.Principal, string) (entity.Artifact, error)
	// ListArtifactRevisions pages counted exact file-revision metadata under owner eligibility rather than
	// exposing mutable object-store keys.
	// ListArtifactRevisions возвращает страницы точных метаданных ревизий файла с количеством в
	// допустимости владельца вместо изменяемых object-store keys.
	ListArtifactRevisions(context.Context, value.Principal, string, query.Page) ([]entity.ArtifactRevision, int64, string, error)
	// GetArtifactRevision resolves exact artifact/revision metadata for the principal, without inferring
	// safety or byte-read permission from the revision identifier.
	// GetArtifactRevision разрешает точные метаданные артефакта и ревизии для principal, не выводя
	// безопасность или право чтения байтов из ID ревизии.
	GetArtifactRevision(context.Context, value.Principal, string, string) (entity.ArtifactRevision, error)
	// GetArtifactImpact evaluates the addressed artifact/action's owner-visible blockers and
	// binding/runtime impact; a Permitted view is not execution of purge or mutation.
	// GetArtifactImpact оценивает препятствия и влияние на связи и runtime заданных артефакта и действия;
	// Permitted view не исполняет очистку или изменение.
	GetArtifactImpact(context.Context, value.Principal, string, string) (entity.ArtifactImpact, error)
	// GetAttachmentSet reads an eligible attachment manifest and a bounded item page/cursor, preserving
	// artifact revision/digest pins instead of resolving display names as paths.
	// GetAttachmentSet читает допустимый manifest вложений и ограниченную страницу элементов с курсором,
	// сохраняя ревизии и хеши вместо разрешения отображаемых имён как путей.
	GetAttachmentSet(context.Context, value.Principal, string, query.Page) (entity.AttachmentSet, string, error)
	// UploadArtifact accepts a rereadable upload under exact principal/mutation and returns owner artifact
	// metadata; implementations own size/scan/storage/receipt checks rather than trusting supplied scan
	// state.
	// UploadArtifact принимает перечитываемую загрузку под точными principal и изменением и возвращает
	// метаданные владельца; пределы, scan, storage и receipts проверяет реализация, не доверие переданному
	// scan state.
	UploadArtifact(context.Context, value.Principal, value.Mutation, ArtifactUpload) (entity.Artifact, error)
	// UploadAgentAvatar binds an artifact upload to the exact agent/expected version and mutation,
	// returning its updated view instead of an unscoped asset write.
	// UploadAgentAvatar связывает загрузку артефакта с точными агентом, ожидаемой версией и изменением,
	// возвращая обновлённое view вместо unscoped-записи файла.
	UploadAgentAvatar(context.Context, value.Principal, value.Mutation, AgentAvatarUpload) (entity.Agent, error)
	// CleanupExpiredAgentAvatarUploads performs a bounded local cleanup batch of expired avatar uploads;
	// the numeric limit is not a selector for deleting arbitrary active assets.
	// CleanupExpiredAgentAvatarUploads выполняет ограниченный локальный batch очистки истёкших загрузок
	// аватаров; числовой предел не выбирает произвольные активные файлы для удаления.
	CleanupExpiredAgentAvatarUploads(context.Context, int32) error
	// DownloadArtifact authorizes current-revision bytes for the addressed artifact/read purpose and
	// returns metadata, grant reference and a closable stream; callers retain stream cleanup ownership.
	// DownloadArtifact разрешает байты текущей ревизии заданного артефакта и назначения чтения, возвращая
	// метаданные, grant reference и закрываемый поток; закрытие остаётся обязанностью вызывающего.
	DownloadArtifact(context.Context, value.Principal, string, string) (ArtifactDownload, error)
	// DownloadArtifactRevision authorizes the exact artifact/revision/read-purpose tuple before returning
	// the closable stream, rather than silently substituting latest content.
	// DownloadArtifactRevision разрешает точный набор артефакта, ревизии и назначения чтения до возврата
	// закрываемого потока вместо незаметной подстановки latest content.
	DownloadArtifactRevision(context.Context, value.Principal, string, string, string) (ArtifactDownload, error)
	// PurgeArtifact executes the scoped versioned purge against the exact impact digest and returns
	// lifecycle state; object deletion/readback and durable finalization can fail, so a proposal is not
	// completion.
	// PurgeArtifact выполняет scoped-версионную очистку по точному хешу влияния и возвращает lifecycle
	// state; удаление объектов, readback и устойчивая фиксация могут отказать, поэтому предложение не
	// равно завершению.
	PurgeArtifact(context.Context, value.Principal, value.Mutation, string, string) (string, error)
	// ReadExecutionArtifact resolves artifact bytes only in the exact principal/lease/fence/generation
	// execution context, returning a closable download rather than arbitrary artifact access.
	// ReadExecutionArtifact разрешает байты артефакта лишь в точном контексте principal, lease, fence и
	// generation исполнения, возвращая закрываемую загрузку, не произвольный доступ.
	ReadExecutionArtifact(context.Context, value.Principal, string, string, int64, string) (ArtifactDownload, error)
	// SearchAssistantResources searches inside the exact assistant lease/fence/generation and principal
	// boundary, returning scoped hits plus a truncation flag instead of unbounded global search.
	// SearchAssistantResources ищет внутри точных assistant lease, fence, generation и principal,
	// возвращая scoped-находки и признак усечения вместо неограниченного глобального поиска.
	SearchAssistantResources(context.Context, value.Principal, string, string, int64, string) ([]entity.SearchResult, bool, error)
	// ReadAssistantTaskSession reads a typed, paged task-session history under exact assistant
	// lease/fence/generation, preserving origin/session eligibility rather than trusting references in
	// prompt text.
	// ReadAssistantTaskSession читает типизированную страницу истории task session под точными assistant
	// lease, fence и generation, сохраняя допустимость происхождения и сессии вместо доверия ссылкам
	// промпта.
	ReadAssistantTaskSession(context.Context, value.Principal, string, string, int64, query.AssistantTaskSessionRead) (query.AssistantTaskSessionPage, error)
	// ListAssistantIntegrationDefinitions returns reduced public definition candidates under exact
	// assistant lease/fence/generation and search/offset bounds, not connection credential instances.
	// ListAssistantIntegrationDefinitions возвращает сокращённых публичных кандидатов определений под
	// точными assistant lease, fence, generation, поиском и offset, не экземпляры credentials подключений.
	ListAssistantIntegrationDefinitions(context.Context, value.Principal, string, string, int64, string, int32) ([]entity.AssistantIntegrationDefinition, int32, error)
	// ListAssistantConfigurationCatalog resolves the typed configuration/image/environment catalog in the
	// assistant's exact lease scope, keeping model-visible candidates separate from execution admission.
	// ListAssistantConfigurationCatalog разрешает типизированный каталог конфигураций, образов и окружений
	// в точной lease-области помощника, отделяя видимых модели кандидатов от допуска к исполнению.
	ListAssistantConfigurationCatalog(context.Context, value.Principal, string, string, int64, entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error)
	// SearchExecutionFiles searches only the owner-resolved execution-file context with bounded
	// page/query, not arbitrary host or repository paths supplied by a worker.
	// SearchExecutionFiles ищет лишь в разрешённом владельцем контексте файлов исполнения с ограниченными
	// страницей и запросом, не произвольных путях хоста или репозитория от worker.
	SearchExecutionFiles(context.Context, value.Principal, query.ExecutionFileContext, string, query.Page) (entity.ExecutionFilePage, error)
	// GetExecutionFileMetadata resolves one typed file reference under its exact execution context and
	// principal, without exposing a storage locator or bypassing eligibility.
	// GetExecutionFileMetadata разрешает типизированную ссылку файла под точными контекстом выполнения и
	// principal без выдачи storage locator или обхода допустимости.
	GetExecutionFileMetadata(context.Context, value.Principal, query.ExecutionFileContext, query.ExecutionFileRef) (entity.ExecutionFileMetadata, error)
	// PreviewExecutionFile requests a bounded preview of one typed file within exact execution/principal
	// eligibility; preview limits do not authorize raw arbitrary-path reads.
	// PreviewExecutionFile запрашивает ограниченный preview типизированного файла внутри точных исполнения
	// и principal; предел preview сырое чтение произвольных путей не разрешает.
	PreviewExecutionFile(context.Context, value.Principal, query.ExecutionFileContext, query.ExecutionFileRef, int32) (entity.ExecutionFilePreview, error)
	// GetExecutionFileManifest pages the owner-selected execution file manifest so consumers retain exact
	// file/revision context rather than walking host directories.
	// GetExecutionFileManifest возвращает страницы выбранного владельцем manifest файлов исполнения для
	// сохранения точного контекста файлов и ревизий вместо обхода каталогов хоста.
	GetExecutionFileManifest(context.Context, value.Principal, query.ExecutionFileContext, query.Page) (entity.ExecutionFilePage, error)
	// ListSchedules pages eligible automation definitions/current state under principal/filter scope
	// without claiming due occurrences or starting runs.
	// ListSchedules возвращает страницы допустимых определений и состояния автоматизаций под principal и
	// фильтром без захвата наступивших событий или запуска.
	ListSchedules(context.Context, value.Principal, query.Filter) ([]entity.Schedule, string, error)
	// GetSchedule resolves one scoped schedule and its current revision/target/timing views; reading it
	// does not modify session continuation or next-run state.
	// GetSchedule разрешает scoped-расписание и его текущую ревизию, цель и время; чтение продолжение
	// сессии и next-run state не меняет.
	GetSchedule(context.Context, value.Principal, string) (entity.Schedule, error)
	// ListScheduleRevisions pages exact automation definition history for the selected owner filter,
	// preserving revision and target provenance.
	// ListScheduleRevisions возвращает страницы точной истории определений автоматизации по фильтру
	// владельца, сохраняя происхождение ревизии и цели.
	ListScheduleRevisions(context.Context, value.Principal, query.Filter) ([]entity.ScheduleRevision, string, error)
	// ListScheduleRuns pages runs associated with their exact schedule revisions/occurrences, not a
	// reconstruction from the schedule's mutable current text.
	// ListScheduleRuns возвращает страницы запусков с точными ревизиями расписания и событиями, не
	// восстановление по изменяемому текущему тексту.
	ListScheduleRuns(context.Context, value.Principal, query.Filter) ([]entity.ScheduleRunOccurrence, string, error)
	// ListIntegrationDefinitions pages eligible integration catalog definitions and available actions, not
	// activated connections or permission to execute catalog operations.
	// ListIntegrationDefinitions возвращает страницы допустимых определений интеграций и доступные
	// действия, не активированные подключения или право выполнять операции каталога.
	ListIntegrationDefinitions(context.Context, value.Principal, query.Filter) ([]entity.IntegrationDefinition, string, []string, error)
	// ListIntegrationConnections pages owner-visible connection/configuration/grant metadata with cursor,
	// keeping credentials in managed descriptors rather than raw public results.
	// ListIntegrationConnections возвращает страницы доступных владельцу подключений, настроек и grants с
	// курсором, сохраняя credentials в управляемых дескрипторах вместо сырых публичных результатов.
	ListIntegrationConnections(context.Context, value.Principal, query.Filter) ([]entity.IntegrationConnection, string, error)
	// ListIntegrationGrantConnectionCandidates resolves typed eligible connection candidates for grant
	// configuration; selecting a candidate still requires the specialized grant mutation.
	// ListIntegrationGrantConnectionCandidates разрешает типизированных допустимых кандидатов подключения
	// для настройки grant; выбор по-прежнему требует специализированного изменения.
	ListIntegrationGrantConnectionCandidates(context.Context, value.Principal, query.IntegrationCandidates) (entity.IntegrationConnectionCandidates, error)
	// ListIntegrationGrantProjectCandidates resolves eligible project choices for an integration grant
	// without accepting project references as ownership authority.
	// ListIntegrationGrantProjectCandidates разрешает допустимые проекты для grant интеграции без принятия
	// ссылок как полномочий владения.
	ListIntegrationGrantProjectCandidates(context.Context, value.Principal, query.IntegrationCandidates) (entity.IntegrationProjectCandidates, error)
	// ListIntegrationGrantRecipientCandidates resolves allowed recipient choices under the typed grant
	// context, not arbitrary external addresses derived from message content.
	// ListIntegrationGrantRecipientCandidates разрешает допустимых получателей в типизированном контексте
	// grant, не произвольные внешние адреса из текста сообщения.
	ListIntegrationGrantRecipientCandidates(context.Context, value.Principal, query.IntegrationCandidates) (entity.IntegrationRecipientCandidates, error)
	// ListIntegrationGrantCapabilityCandidates resolves allowed capability choices and policy metadata for
	// the exact candidate context rather than granting everything advertised by an adapter.
	// ListIntegrationGrantCapabilityCandidates разрешает допустимые возможности и политики точного
	// контекста кандидатов, не выдавая всё предлагаемое адаптером.
	ListIntegrationGrantCapabilityCandidates(context.Context, value.Principal, query.IntegrationCandidates) (entity.IntegrationCapabilityCandidates, error)
	// GetSystemAssistantIntegrationGrantCandidates queries organization-assistant grant candidates for an
	// exact connection/search/page under principal scope, without copying a project's grants.
	// GetSystemAssistantIntegrationGrantCandidates запрашивает кандидатов grant общесистемного помощника
	// для точных подключения, поиска и страницы под principal без копирования grants проекта.
	GetSystemAssistantIntegrationGrantCandidates(context.Context, value.Principal, string, string, query.Page) (entity.SystemAssistantIntegrationGrantCandidates, error)
	// GetProjectAssistantIntegrationGrantCandidates adds exact project and connection scope to the
	// assistant candidate query, preserving separation from organization-assistant grants.
	// GetProjectAssistantIntegrationGrantCandidates добавляет точные проект и подключение к запросу
	// кандидатов помощника, сохраняя отделение от grants общесистемного помощника.
	GetProjectAssistantIntegrationGrantCandidates(context.Context, value.Principal, string, string, string, query.Page) (entity.ProjectAssistantIntegrationGrantCandidates, error)
	// GetIntegrationConnection reads the exact eligible connection view and bound definition/credential
	// metadata, not a request to connect externally or reveal a password.
	// GetIntegrationConnection читает точное допустимое view подключения и привязанные метаданные
	// определения и credentials, не запрашивая внешний вход или раскрытие пароля.
	GetIntegrationConnection(context.Context, value.Principal, string) (entity.IntegrationConnection, error)
	// GetSystemAssistant reads the organization assistant's identity/configuration/runtime state for the
	// principal without starting a session or issuing an execution lease.
	// GetSystemAssistant читает личность, конфигурацию и runtime-состояние общесистемного помощника для
	// principal без запуска сессии или выдачи lease исполнения.
	GetSystemAssistant(context.Context, value.Principal) (entity.SystemAssistant, error)
	// GetProjectAssistant resolves the exact project's independent assistant profile under owner
	// eligibility rather than silently reusing another project's or system profile.
	// GetProjectAssistant разрешает независимый профиль помощника точного проекта в допустимости владельца
	// вместо незаметного использования чужого или системного профиля.
	GetProjectAssistant(context.Context, value.Principal, string) (entity.ProjectAssistantProfile, error)
	// ListAssistantConversations pages dialog summaries under the typed assistant/project/state filter and
	// principal boundary; a cursor does not grant access to another session's history.
	// ListAssistantConversations возвращает страницы диалогов под типизированным фильтром помощника,
	// проекта, состояния и principal; курсор историю другой сессии не разрешает.
	ListAssistantConversations(context.Context, value.Principal, query.AssistantConversationFilter) ([]entity.AssistantConversation, string, error)
	// GetAdministration returns the scoped observed installation/core/assistant/adapter/incident view; it
	// is not a command to repair infrastructure or a complete live readiness proof.
	// GetAdministration возвращает scoped-наблюдение установки, ядра, помощника, адаптеров и инцидентов;
	// это не команда ремонта инфраструктуры или полное live-доказательство готовности.
	GetAdministration(context.Context, value.Principal) (Administration, error)
	// ListAuditEvents pages owner-visible audit records under typed resource/project filters, without
	// treating caller-provided event text as evidence of an effect.
	// ListAuditEvents возвращает страницы доступных владельцу audit-записей под типизированными фильтрами
	// ресурса и проекта, не считая текст события от вызывающего доказательством эффекта.
	ListAuditEvents(context.Context, value.Principal, query.Filter) ([]entity.AuditEvent, string, error)
	// ListPermissionRegistry reads the closed permission-definition catalog for the principal, not a grant
	// of the enumerated permissions.
	// ListPermissionRegistry читает закрытый каталог определений прав для principal, не выдавая
	// перечисленные права.
	ListPermissionRegistry(context.Context, value.Principal) ([]entity.PermissionDefinition, error)
	// ListAccessSubjects pages eligible typed access subjects using the requested kind/filter; subject
	// lookup and permission evaluation remain distinct.
	// ListAccessSubjects возвращает страницы допустимых субъектов доступа по виду и фильтру; поиск
	// субъекта и оценка прав остаются отдельными.
	ListAccessSubjects(context.Context, value.Principal, query.Filter, string) ([]entity.AccessSubject, string, error)
	// ListOIDCGroups pages the owner-visible OIDC group registry for role/binding selection, not live
	// authentication or automatic membership assignment.
	// ListOIDCGroups возвращает страницы доступного владельцу реестра OIDC-групп для выбора роли и связи,
	// не выполняя live-аутентификацию или автоматическое назначение членства.
	ListOIDCGroups(context.Context, value.Principal, query.Filter) ([]entity.OIDCGroup, string, error)
	// ListAccessRoles pages eligible roles with an explicit include-archived selector, keeping catalog
	// visibility separate from role activation or binding.
	// ListAccessRoles возвращает страницы допустимых ролей с явным выбором include-archived, отделяя
	// каталог от активации или привязки роли.
	ListAccessRoles(context.Context, value.Principal, query.Filter, bool) ([]entity.AccessRole, string, error)
	// ListAccessRoleVersions reads a scoped role header with its paged version history and cursor;
	// historical policy content does not make that version active.
	// ListAccessRoleVersions читает scoped-заголовок роли с историей версий, страницей и курсором;
	// историческое содержимое политики активной версией не становится.
	ListAccessRoleVersions(context.Context, value.Principal, string, query.Page) (entity.AccessRole, []entity.AccessRoleVersion, string, error)
	// ListAccessBindings queries principal-visible bindings through the typed subject/scope/filter
	// boundary, without creating or renewing an access binding.
	// ListAccessBindings запрашивает доступные principal связи через типизированные subject, scope и
	// фильтр без создания или продления связи.
	ListAccessBindings(context.Context, value.Principal, query.AccessBindingFilter) ([]entity.AccessBinding, string, error)
	// QueryEffectiveAccess evaluates the selected subject/scope/permission keys at the exact supplied time
	// under caller authority; the returned explanation is not an execution grant.
	// QueryEffectiveAccess оценивает выбранные субъект, область и ключи прав на точное переданное время
	// под полномочиями вызывающего; возвращаемое объяснение не является grant исполнения.
	QueryEffectiveAccess(context.Context, value.Principal, string, entity.AccessScope, []string, time.Time) (entity.EffectiveAccess, error)
	// SimulateAccess requests a typed what-if policy/binding evaluation for the principal without applying
	// those proposed access changes as actual grants.
	// SimulateAccess запрашивает типизированную what-if оценку политики и связей для principal без
	// применения предлагаемых изменений как действительных grants.
	SimulateAccess(context.Context, value.Principal, command.AccessSimulationInput) (entity.AccessSimulation, error)
	// Execute runs the existing typed owner command with principal and semantic mutation pins, including
	// owner authorization/idempotency/result handling; it is not an arbitrary kind/payload privilege
	// bypass or a promise that external object work is one database transaction.
	// Execute выполняет существующую типизированную команду владельца с principal и смысловыми привязками
	// изменения, включая авторизацию, идемпотентность и результат; это не обход прав через произвольные
	// kind и payload и не обещание единой DB-транзакции внешней object-работы.
	Execute(context.Context, command.Command) (command.Result, error)
	// ReconcileWarmRuntime reconciles the exact workload request against assistant desired/observed state
	// and returns its runtime revision/materialization-needed view; the map does not make the caller an
	// unrestricted runtime owner.
	// ReconcileWarmRuntime согласует точный запрос нагрузки с желаемым и наблюдаемым состоянием помощника
	// и возвращает runtime revision и необходимость материализации; map не делает вызывающего
	// неограниченным владельцем runtime.
	ReconcileWarmRuntime(context.Context, value.Principal, string) (entity.SystemAssistant, map[string]any, bool, error)
	// ReportWarmRuntime reports the typed workload/runtime revision/state under the worker principal and
	// returns the owner's assistant view, not a user-selected overwrite of another runtime.
	// ReportWarmRuntime сообщает типизированные workload, runtime revision и state под worker principal и
	// возвращает view помощника владельца, не пользовательскую перезапись чужого runtime.
	ReportWarmRuntime(context.Context, value.Principal, command.WarmRuntimeInput) (entity.SystemAssistant, error)
	// ClaimSessionArchiveTasks claims a bounded batch for the exact worker principal/instance, returning
	// owner-created task bindings rather than selecting arbitrary sessions from payload.
	// ClaimSessionArchiveTasks захватывает ограниченный batch точных worker principal и экземпляра,
	// возвращая созданные владельцем привязки задач вместо произвольных сессий из payload.
	ClaimSessionArchiveTasks(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	// RenewSessionArchiveTask renews the exact typed archive task/lease tuple for the principal; a session
	// reference alone cannot extend another worker's task.
	// RenewSessionArchiveTask продлевает точный типизированный набор archive task и lease для principal;
	// ссылка сессии сама чужую задачу не продлевает.
	RenewSessionArchiveTask(context.Context, value.Principal, command.SessionArchiveTaskInput) (map[string]any, error)
	// ClaimProviderCredentialCleanupTasks is the local cleanup-worker contract for a bounded claimant
	// batch of exact descriptor/attempt/generation snapshots; absence of a Principal parameter is not
	// browser-facing delete authority.
	// ClaimProviderCredentialCleanupTasks — контракт локального cleanup worker для ограниченного batch
	// claimant с точными дескрипторами, попыткой и поколением; отсутствие Principal не означает
	// browser-полномочия удаления.
	ClaimProviderCredentialCleanupTasks(context.Context, string, int32) ([]ProviderCredentialCleanupTask, error)
	// CompleteProviderCredentialCleanupTask reports exact task/claimant/generation cleanup readback and
	// returns owner terminal/retry state, without allowing a stale result to target current credentials by
	// account name alone.
	// CompleteProviderCredentialCleanupTask сообщает readback очистки точных task, claimant и generation и
	// возвращает terminal или retry state владельца без выбора текущих credentials устаревшим результатом
	// лишь по имени аккаунта.
	CompleteProviderCredentialCleanupTask(context.Context, string, string, int64, entity.ProviderAuthorizationCleanupResult) (ProviderCredentialCleanupResult, error)
	// FailProviderCredentialCleanupTask reports a safe failure code against the exact local cleanup task
	// lease/generation, keeping retry or terminal decisions with the owner.
	// FailProviderCredentialCleanupTask сообщает безопасный код отказа точных локальной cleanup task,
	// lease и generation, оставляя решение о повторе или terminal владельцу.
	FailProviderCredentialCleanupTask(context.Context, string, string, int64, string) (ProviderCredentialCleanupResult, error)
	// ClaimDueSchedules claims a bounded batch of due occurrences for the exact worker principal/instance,
	// rather than launching arbitrary schedule references supplied by a caller.
	// ClaimDueSchedules захватывает ограниченный batch наступивших событий для точных worker principal и
	// экземпляра, не запуская произвольные ссылки расписаний от вызывающего.
	ClaimDueSchedules(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	// RenewScheduleOccurrence renews the exact typed schedule occurrence lease under its worker principal,
	// preserving occurrence/revision identity across retries.
	// RenewScheduleOccurrence продлевает точный типизированный lease события расписания под worker
	// principal, сохраняя личность события и ревизии при повторах.
	RenewScheduleOccurrence(context.Context, value.Principal, command.OccurrenceInput) (map[string]any, error)
	// ClaimIntegrationConnectionTests claims a bounded worker batch of prepared connection-test work;
	// claiming is not an automatic Human Gate approval for dangerous test effects.
	// ClaimIntegrationConnectionTests захватывает ограниченный worker batch подготовленных проверок
	// подключений; захват автоматическим Human Gate подтверждением опасных эффектов не является.
	ClaimIntegrationConnectionTests(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	// ResolveIntegrationInvocation resolves owner invocation context from exact selectors and bounded
	// typed argument data; input maps are not themselves grant, actor or approval authority.
	// ResolveIntegrationInvocation разрешает контекст вызова владельца по точным selectors и ограниченным
	// аргументам; ввод maps сам не является grant, исполнителем или полномочием подтверждения.
	ResolveIntegrationInvocation(context.Context, value.Principal, map[string]string, map[string]any) (map[string]any, error)
	// ClaimIntegrationInvocations assigns bounded prepared invocation work to the exact worker
	// principal/instance, preserving owner operation/grant/gate state rather than creating an arbitrary
	// adapter call.
	// ClaimIntegrationInvocations назначает ограниченную подготовленную работу вызовов точным worker
	// principal и экземпляру, сохраняя operation, grant и gate state владельца вместо произвольного вызова
	// адаптера.
	ClaimIntegrationInvocations(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	// GetIntegrationInvocation reads the exact owner-scoped invocation work/result view by reference, not
	// an external operation retry or unrestricted credential read.
	// GetIntegrationInvocation читает точное scoped-view работы или результата вызова по ссылке, не
	// повторяя внешнюю операцию и не читая credentials без ограничений.
	GetIntegrationInvocation(context.Context, value.Principal, string) (map[string]any, error)
	// ListInteractionSources reads interaction-source work metadata for the eligible principal; returned
	// maps do not authorize new channels, actors or arbitrary remote endpoints.
	// ListInteractionSources читает метаданные источников взаимодействия допустимого principal;
	// возвращаемые maps не разрешают новые каналы, исполнителей или произвольные remote endpoints.
	ListInteractionSources(context.Context, value.Principal) ([]map[string]any, error)
	// ClaimInteractionDeliveries claims a bounded delivery batch for the exact principal/worker instance,
	// leaving message origin, routing and lease ownership with the server.
	// ClaimInteractionDeliveries захватывает ограниченный batch доставок точных principal и экземпляра
	// worker, оставляя происхождение сообщений, маршрутизацию и владение lease серверу.
	ClaimInteractionDeliveries(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	// Ready checks required repository/schema readiness for serving this owner boundary; it does not
	// assert that every downstream adapter, provider or live user flow is healthy.
	// Ready проверяет необходимую готовность repository и схемы для обслуживания границы владельца;
	// здоровье всех downstream-адаптеров, провайдеров и live-сценариев не утверждает.
	Ready(context.Context) error
}
