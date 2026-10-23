package grpc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Owner adapts the protected control-plane operation/projection APIs without variadic transport options.
// Implementations own grant/claim authority; returning a DTO does not make arbitrary caller metadata trusted.
// Owner адаптирует защищённые control-plane operation/projection API без вариативных transport options.
// Реализации владеют grant/claim authority; один DTO не делает произвольные metadata caller доверенными.
type Owner interface {
	// Check probes the owner's operation surface without consuming a one-use operation grant.
	// Check проверяет готовность owner operation surface без потребления одноразового operation grant.
	Check(context.Context) error
	// CheckCredentialProjection probes owner support for the protected credential-projection path.
	// CheckCredentialProjection проверяет поддержку защищённого credential-projection path у owner.
	CheckCredentialProjection(context.Context) error
	// Consume exchanges the exact opaque grant for a claimant-bound operation under owner authority.
	// Consume обменивает точный opaque grant на claimant-bound operation под authority владельца.
	Consume(context.Context, string) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error)
	// Complete reports an exact operation/generation and optional materialization to the authoritative owner;
	// the broker must verify the returned secret matches that effect before exposing its result.
	// Complete сообщает authoritative owner точные operation/generation и необязательную materialization;
	// broker обязан сверить возвращённый secret с эффектом перед выдачей результата.
	Complete(context.Context, string, int64, *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RuntimeSecret, error)
	// Fail reports a closed terminal failure for the exact operation and claim generation.
	// Fail сообщает закрытый terminal failure точных operation и claim generation.
	Fail(context.Context, string, int64, controlplanev1.RuntimeSecretFailureCode) error
	// ResolveRuntimeCredentialProjection obtains the owner's exact execution-bound projection manifest.
	// ResolveRuntimeCredentialProjection получает точный execution-bound projection manifest владельца.
	ResolveRuntimeCredentialProjection(context.Context, *controlplanev1.ResolveRuntimeCredentialProjectionRequest) (*controlplanev1.ResolveRuntimeCredentialProjectionResponse, error)
	// ValidateRuntimeCredentialProjection asks the owner whether the supplied pinned runtime binding is still eligible.
	// ValidateRuntimeCredentialProjection запрашивает у owner актуальную допустимость закреплённой runtime binding.
	ValidateRuntimeCredentialProjection(context.Context, *controlplanev1.ValidateRuntimeCredentialProjectionRequest) (bool, error)
	// ResolveTranscriptionCredentialProjection resolves the separate owner-authorized transcription credential binding.
	// ResolveTranscriptionCredentialProjection разрешает отдельную owner-authorized transcription credential binding.
	ResolveTranscriptionCredentialProjection(context.Context, *controlplanev1.ResolveTranscriptionCredentialProjectionRequest) (*controlplanev1.ResolveTranscriptionCredentialProjectionResponse, error)
}

// Recovery supplies the local reconciler's health observation, not a second authority for secret operations.
// Recovery предоставляет health observation локального reconciler, не вторую authority secret operations.
type Recovery interface {
	// Check reports recovery health without issuing grants or treating an incomplete scan as owner completion.
	// Check сообщает recovery health без выдачи grants и без подмены owner completion неполным scan.
	Check(context.Context) error
}

// Store exposes the exact Kubernetes materialization/projection operations required by this transport.
// Caller-side owner checks remain mandatory; possession of this port does not establish resource eligibility.
// Store предоставляет точные Kubernetes materialization/projection operations этого transport.
// Owner checks caller остаются обязательными; наличие port не доказывает resource eligibility.
type Store interface {
	// Namespace returns the stable configured storage namespace captured by server construction.
	// Namespace возвращает стабильный настроенный storage namespace для server construction.
	Namespace() string
	// Check probes local storage prerequisites used by broker readiness, not downstream owner authority.
	// Check проверяет локальные storage prerequisites broker readiness, не downstream owner authority.
	Check(context.Context) error
	// CreateImmutableForEffect materializes bytes for the exact authorized effect and returns identity pins.
	// CreateImmutableForEffect материализует байты точного разрешённого эффекта и возвращает identity pins.
	CreateImmutableForEffect(context.Context, kubernetesstore.MaterializationEffect, []byte) (kubernetesstore.Materialization, error)
	// ResolveExact verifies the descriptor's object identity and content metadata without returning plaintext.
	// ResolveExact сверяет object identity и content metadata descriptor без возврата plaintext.
	ResolveExact(context.Context, kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, error)
	// ReadExactValue returns bytes only for the exact descriptor; the caller owns clearing the returned buffer.
	// ReadExactValue возвращает байты лишь точного descriptor; caller отвечает за обнуление возвращённого буфера.
	ReadExactValue(context.Context, kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, []byte, error)
	// DeleteExact removes only the supplied verified materialization using its exact object preconditions.
	// DeleteExact удаляет лишь переданную проверенную materialization с её точными object preconditions.
	DeleteExact(context.Context, kubernetesstore.Materialization) error
	// ReadProviderCredentialExact reads one account-bound provider credential from the exact pinned object.
	// ReadProviderCredentialExact читает один account-bound provider credential из точного закреплённого объекта.
	ReadProviderCredentialExact(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error)
	// MaterializeRuntimeCredentialProjection builds the execution projection from a verified manifest and exact sources.
	// MaterializeRuntimeCredentialProjection строит execution projection по проверенному manifest и точным источникам.
	MaterializeRuntimeCredentialProjection(context.Context, kubernetesstore.CredentialProjectionManifest) (kubernetesstore.CredentialProjection, error)
	// ListRuntimeCredentialProjections inventories managed projections for subsequent owner-bound validation/cleanup.
	// ListRuntimeCredentialProjections составляет inventory управляемых projections для последующей owner-bound проверки/очистки.
	ListRuntimeCredentialProjections(context.Context) ([]kubernetesstore.CredentialProjection, error)
	// DeleteRuntimeCredentialProjection deletes one exact observed projection, not all objects sharing a label.
	// DeleteRuntimeCredentialProjection удаляет одну точную observed projection, не все объекты с общей меткой.
	DeleteRuntimeCredentialProjection(context.Context, kubernetesstore.CredentialProjection) error
}

// Server implements immediate secret and credential RPC surfaces over explicit owner/storage/recovery
// ports and optional provider/draft capabilities. It does not replace authentication interceptors,
// authoritative owner lifecycle or the private adapters' exact effect checks.
// Server реализует immediate secret и credential RPC через явные owner/storage/recovery ports
// и необязательные provider/draft capabilities. Он не заменяет authentication interceptors,
// authoritative owner lifecycle и точные effect checks приватных adapters.
type Server struct {
	catalogLogger *slog.Logger
	secretbrokerv1.UnimplementedSecretBrokerServiceServer
	secretbrokerv1.UnimplementedRuntimeCredentialProjectionServiceServer
	sttv1.UnimplementedTranscriptionCredentialProjectionServiceServer
	controlplanev1.UnimplementedProviderCredentialMaterializerServiceServer
	owner                Owner
	store                Store
	recovery             Recovery
	providerCredentials  ProviderCredentialMaterializer
	providerRenewalOwner ProviderRenewalOwner
	drafts               DraftCommands
	namespace            string
	maximumSize          int
}

// ProviderCredentialMaterializer isolates provider login, exact credentials and recovery/catalog work
// from transport. Its implementations can perform effects; method names do not grant caller authority.
// ProviderCredentialMaterializer изолирует provider login, точные credentials и recovery/catalog work
// от transport. Реализации могут выполнять эффекты; имена методов не выдают authority caller.
type ProviderCredentialMaterializer interface {
	// Check verifies materializer prerequisites without starting a device authorization or model turn.
	// Check проверяет materializer prerequisites без запуска device authorization или model turn.
	Check(context.Context) error
	// StartDeviceAuthorization starts or rejoins the exact attempt/account flow and may own a background producer.
	// StartDeviceAuthorization запускает или продолжает точный attempt/account flow и может владеть background producer.
	StartDeviceAuthorization(context.Context, string, string) (providercredential.DeviceAuthorization, error)
	// ObserveDeviceAuthorization reads attempt state and may persist expiry; it is not the read-only cleanup observer.
	// ObserveDeviceAuthorization читает attempt state и может фиксировать expiry; это не read-only cleanup observer.
	ObserveDeviceAuthorization(context.Context, string) (kubernetesstore.ProviderAuthorizationAttempt, error)
	// MaterializeAPIKey stores the validated account/attempt credential and returns its exact descriptor and display label;
	// plaintext remains sensitive input, never a diagnostic or ordinary response field.
	// MaterializeAPIKey сохраняет проверенный account/attempt credential и возвращает точные descriptor и display label;
	// plaintext остаётся чувствительным входом, не diagnostic или обычным response field.
	MaterializeAPIKey(context.Context, string, string, []byte) (kubernetesstore.ProviderCredentialDescriptor, string, error)
	// Discard stops local pending work where needed and removes only the supplied pinned attempt or credential.
	// Discard при необходимости останавливает локальный pending work и удаляет лишь переданные pinned attempt/credential.
	Discard(context.Context, providercredential.DiscardMaterialization) error
	// CleanupProviderCredentialWithRecovery reconciles the exact credential cleanup task under its recovery identity.
	// CleanupProviderCredentialWithRecovery согласует точную credential cleanup task с её recovery identity.
	CleanupProviderCredentialWithRecovery(context.Context, string, string, int64, kubernetesstore.ProviderCredentialDescriptor, kubernetesstore.ProviderCleanupRecoveryIdentity) (kubernetesstore.ProviderCredentialCleanupResult, error)
	// ObserveAuthorizationCleanup reads exact cleanup metadata without polling authorization or expiring its attempt.
	// ObserveAuthorizationCleanup читает точные cleanup metadata без authorization polling или expiry попытки.
	ObserveAuthorizationCleanup(context.Context, kubernetesstore.ProviderAuthorizationCleanupTarget) (kubernetesstore.ProviderAuthorizationCleanupObservation, error)
	// CleanupAuthorizationWithRecovery performs fenced authorization cleanup using the bound recovery descriptor.
	// CleanupAuthorizationWithRecovery выполняет fenced authorization cleanup по связанному recovery descriptor.
	CleanupAuthorizationWithRecovery(context.Context, kubernetesstore.ProviderAuthorizationCleanupTarget, kubernetesstore.ProviderCleanupRecoveryIdentity) (kubernetesstore.ProviderCredentialCleanupResult, error)
	// ObserveModelCatalog queries the configured catalog method using the exact account/credential descriptor,
	// not an arbitrary caller-selected provider URL or credential body.
	// ObserveModelCatalog запрашивает настроенный catalog method по точным account/credential descriptor,
	// не произвольному provider URL или credential body caller.
	ObserveModelCatalog(context.Context, string, kubernetesstore.ProviderCredentialDescriptor, string) (providercredential.ModelCatalog, error)
}

// Option configures a server during trusted composition; it is not a runtime mutation or authorization API.
// Option настраивает server при доверенной composition; это не runtime mutation или authorization API.
type Option func(*Server)

// WithProviderCredentialMaterializer installs the optional materializer, including nil, without probing it.
// WithProviderCredentialMaterializer устанавливает необязательный materializer, включая nil, без его проверки.
func WithProviderCredentialMaterializer(materializer ProviderCredentialMaterializer) Option {
	return func(server *Server) { server.providerCredentials = materializer }
}

// New requires nonnil ports, the canonical initial namespace and a 1-KiB..1-MiB value limit,
// then applies composition options. Ports must report stable configuration; typed-nil checks,
// readiness calls, listeners and background workers are not provided by this constructor.
// New требует ненулевые ports, канонический начальный namespace и value limit 1 КиБ..1 МиБ,
// затем применяет composition options. Конфигурация ports должна быть стабильной; typed-nil checks,
// readiness calls, listeners и background workers этот constructor не обеспечивает.
func New(owner Owner, store Store, recovery Recovery, maximumSize int, options ...Option) (*Server, error) {
	if owner == nil || store == nil || recovery == nil || store.Namespace() != "kodex-runtime" || maximumSize < 1<<10 || maximumSize > 1<<20 {
		return nil, errors.New("secret broker server configuration is invalid")
	}
	server := &Server{owner: owner, store: store, recovery: recovery, namespace: store.Namespace(), maximumSize: maximumSize}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	return server, nil
}

// CreateSecret selects the fixed CREATE kind and returns its dedicated metadata response after mutate;
// unlike draft SAVE, this immediate wrapper does not wipe the caller's request value.
// CreateSecret выбирает фиксированный CREATE kind и выдаёт отдельный metadata response после mutate;
// в отличие от draft SAVE этот immediate wrapper не обнуляет request value caller.
func (server *Server) CreateSecret(ctx context.Context, request *secretbrokerv1.CreateSecretRequest) (*secretbrokerv1.CreateSecretResponse, error) {
	secret, err := server.mutate(ctx, request.GetOperationGrant(), request.GetValue(), controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.CreateSecretResponse{Secret: secret}, nil
}

// RotateSecret selects the fixed ROTATE kind while retaining the common fenced mutation path and dedicated response.
// RotateSecret выбирает фиксированный ROTATE kind, сохраняя общий fenced mutation path и отдельный response.
func (server *Server) RotateSecret(ctx context.Context, request *secretbrokerv1.RotateSecretRequest) (*secretbrokerv1.RotateSecretResponse, error) {
	secret, err := server.mutate(ctx, request.GetOperationGrant(), request.GetValue(), controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.RotateSecretResponse{Secret: secret}, nil
}

// mutate rejects size before consumption, checks consumed intent, content digest and value shape,
// then materializes an immutable effect before owner completion. Conflicts are failed with the same fence;
// ambiguous infrastructure/completion failure leaves the effect for recovery rather than deleting it.
// mutate отвергает размер до consumption, проверяет consumed intent, content digest и value shape,
// затем материализует immutable effect перед owner completion. Конфликты закрываются тем же fence;
// неоднозначный infrastructure/completion отказ оставляет эффект для recovery, не удаляя его.
func (server *Server) mutate(ctx context.Context, grant string, value []byte, expected controlplanev1.RuntimeSecretOperationKind) (*secretbrokerv1.RuntimeSecretMetadata, error) {
	if len(value) == 0 || len(value) > server.maximumSize {
		return nil, status.Error(codes.InvalidArgument, "secret value size is invalid")
	}
	operation, err := server.consumeValidatedOperation(ctx, grant, expected)
	if err != nil {
		return nil, err
	}
	if !contentDigestMatches(operation.GetExpectedContentSha256(), value) {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID,
			status.Error(codes.InvalidArgument, "secret value does not match authorized content digest"))
	}
	if err := validateValue(operation.GetValueType(), value); err != nil {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, err)
	}
	materialized, err := server.store.CreateImmutableForEffect(ctx, kubernetesstore.MaterializationEffect{
		WorkKind:     kubernetesstore.WorkKindImmediate,
		OperationRef: operation.GetOperationRef(), ClaimGeneration: operation.GetClaimGeneration(),
		SecretRef: operation.GetSecretRef(), Key: operation.GetSecretKey(), Revision: operation.GetTargetRevision(),
		ContentSHA256: operation.GetExpectedContentSha256(),
	}, value)
	if err != nil {
		if errors.Is(err, kubernetesstore.ErrMaterializationConflict) {
			return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT,
				status.Error(codes.FailedPrecondition, "secret materialization conflicts with authorized effect"))
		}
		if errors.Is(err, kubernetesstore.ErrMaterializationInvalid) {
			return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID,
				status.Error(codes.FailedPrecondition, "secret materialization is invalid"))
		}
		return nil, status.Error(codes.Unavailable, "secret storage is unavailable")
	}
	materialization := castMaterialization(materialized)
	if hint := displayHint(operation.GetValueType(), value); hint != nil {
		materialization.DisplayHint = hint
	}
	secret, err := server.completeEffect(ctx, operation, materialization)
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	return castSecret(secret), nil
}

// RevealSecret consumes REVEAL, resolves the exact revision and completes the owner operation
// before returning a copied value; the adapter buffer is deferred-cleared even on completion failure.
// RevealSecret потребляет REVEAL, разрешает exact revision и завершает owner operation
// до возврата копии значения; adapter buffer обнуляется defer даже при completion failure.
func (server *Server) RevealSecret(ctx context.Context, request *secretbrokerv1.RevealSecretRequest) (*secretbrokerv1.RevealSecretResponse, error) {
	operation, err := server.consumeValidatedOperation(ctx, request.GetOperationGrant(), controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL)
	if err != nil {
		return nil, err
	}
	descriptor, err := exactDescriptor(operation, operation.GetRevisionDescriptors()[0])
	if err != nil {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, err)
	}
	_, value, err := server.store.ReadExactValue(ctx, descriptor)
	if err != nil {
		if errors.Is(err, kubernetesstore.ErrMaterializationConflict) || errors.Is(err, kubernetesstore.ErrMaterializationNotFound) ||
			errors.Is(err, kubernetesstore.ErrExactDeletePreconditionsRequired) {
			return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT,
				status.Error(codes.FailedPrecondition, "secret materialization does not match active revision"))
		}
		return nil, status.Error(codes.Unavailable, "secret storage is unavailable")
	}
	defer clear(value)
	if _, err := server.completeEffect(ctx, operation, nil); err != nil {
		return nil, preserveOwnerError(err)
	}
	return &secretbrokerv1.RevealSecretResponse{Value: append([]byte(nil), value...), ValueType: castValueType(operation.GetValueType())}, nil
}

// RevokeSecret validates all exact revision descriptors before durable owner completion, then cleans
// revoked materializations. Cleanup failure remains Unavailable after revocation and does not undo it.
// RevokeSecret проверяет все exact revision descriptors до устойчивой owner completion, затем очищает
// revoked materializations. Cleanup failure остаётся Unavailable после revocation и не откатывает её.
func (server *Server) RevokeSecret(ctx context.Context, request *secretbrokerv1.RevokeSecretRequest) (*secretbrokerv1.RevokeSecretResponse, error) {
	operation, err := server.consumeValidatedOperation(ctx, request.GetOperationGrant(), controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE)
	if err != nil {
		return nil, err
	}
	descriptors := make([]kubernetesstore.ExactDescriptor, 0, len(operation.GetRevisionDescriptors()))
	for _, revision := range operation.GetRevisionDescriptors() {
		descriptor, descriptorErr := exactDescriptor(operation, revision)
		if descriptorErr != nil {
			return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, descriptorErr)
		}
		descriptors = append(descriptors, descriptor)
	}
	secret, err := server.completeEffect(ctx, operation, nil)
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	if err := server.cleanupRevoked(ctx, descriptors); err != nil {
		return nil, status.Error(codes.Unavailable, "revoked secret cleanup is incomplete")
	}
	return &secretbrokerv1.RevokeSecretResponse{Secret: castSecret(secret)}, nil
}

// consumeValidatedOperation consumes the exact grant once, preserves owner failures, then checks
// the expected kind/namespace and existing descriptor shape. Invalid consumed metadata follows the
// same fenced failure path; lease freshness and authentication remain the owner's responsibility.
// consumeValidatedOperation единожды потребляет точный grant, сохраняет owner failures и проверяет
// ожидаемые kind/namespace и прежнюю форму descriptor. Неверные consumed metadata проходят тот же
// fenced failure path; актуальность lease и authentication остаются ответственностью owner.
func (server *Server) consumeValidatedOperation(ctx context.Context, grant string, expected controlplanev1.RuntimeSecretOperationKind) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error) {
	operation, err := server.owner.Consume(ctx, grant)
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	if err := validateOperation(operation, expected, server.namespace); err != nil {
		return nil, server.failPermanent(ctx, operation, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID, err)
	}
	return operation, nil
}

// completeEffect uses an independent uncancelled ten-second budget and three fixed attempts only for
// Unavailable/DeadlineExceeded/Aborted. A successful reply must match scope/resource/type/revision/state;
// it is not accepted merely because the RPC succeeded, and the owner port must honor its context.
// completeEffect использует независимый неотменённый десятисекундный бюджет и три попытки только для
// Unavailable/DeadlineExceeded/Aborted. Успешный ответ должен совпасть по scope/resource/type/revision/state;
// одного RPC success недостаточно, а owner port обязан соблюдать свой context.
func (server *Server) completeEffect(ctx context.Context, operation *controlplanev1.ConsumeRuntimeSecretOperationResponse, materialization *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RuntimeSecret, error) {
	completion, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	delays := [...]time.Duration{0, 100 * time.Millisecond, 300 * time.Millisecond}
	var err error
	for attempt, delay := range delays {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-completion.Done():
				timer.Stop()
				return nil, completion.Err()
			case <-timer.C:
			}
		}
		var secret *controlplanev1.RuntimeSecret
		secret, err = server.owner.Complete(completion, operation.GetOperationRef(), operation.GetClaimGeneration(), materialization)
		if err == nil {
			expectedState := "ACTIVE"
			if operation.GetKind() == controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE {
				expectedState = "REVOKED"
			}
			if secret == nil || secret.GetScopeKind() != operation.GetScopeKind() || secret.GetOrganizationRef() != operation.GetOrganizationRef() ||
				secret.GetProjectRef() != operation.GetProjectRef() || secret.GetRef() != operation.GetSecretRef() ||
				secret.GetValueType() != operation.GetValueType() || secret.GetCurrentRevision() != operation.GetTargetRevision() || secret.GetState() != expectedState {
				return nil, status.Error(codes.FailedPrecondition, "secret completion does not match authorized operation")
			}
			return secret, nil
		}
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
			continue
		default:
			return nil, err
		}
	}
	return nil, err
}

// failPermanent reports the original operation/generation with a separate bounded retry budget,
// returning the original cause only after owner acknowledgement. Incomplete identity skips reporting;
// receipt failure is preserved instead of fabricating terminal success.
// failPermanent сообщает исходные operation/generation с отдельным ограниченным retry budget,
// возвращая исходную cause лишь после owner acknowledgement. Неполная identity пропускает reporting;
// отказ receipt сохраняется вместо выдуманного terminal success.
func (server *Server) failPermanent(ctx context.Context, operation *controlplanev1.ConsumeRuntimeSecretOperationResponse, failureCode controlplanev1.RuntimeSecretFailureCode, cause error) error {
	if operation == nil || operation.GetOperationRef() == "" || operation.GetClaimGeneration() < 1 {
		return cause
	}
	failure, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	delays := [...]time.Duration{0, 100 * time.Millisecond, 300 * time.Millisecond}
	var err error
	for attempt, delay := range delays {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-failure.Done():
				timer.Stop()
				return status.Error(codes.Unavailable, "runtime secret failure receipt is unavailable")
			case <-timer.C:
			}
		}
		err = server.owner.Fail(failure, operation.GetOperationRef(), operation.GetClaimGeneration(), failureCode)
		if err == nil {
			return cause
		}
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
			continue
		default:
			return preserveOwnerError(err)
		}
	}
	return preserveOwnerError(err)
}

// cleanupRevoked independently bounds serial exact resolve/delete, tolerates an already absent object
// and stops on the first other error; earlier successful deletions are not rolled back.
// cleanupRevoked независимо ограничивает последовательные exact resolve/delete, допускает уже отсутствующий объект
// и останавливается при первой иной ошибке; прежние успешные удаления не откатываются.
func (server *Server) cleanupRevoked(ctx context.Context, descriptors []kubernetesstore.ExactDescriptor) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for _, descriptor := range descriptors {
		materialized, err := server.store.ResolveExact(cleanup, descriptor)
		if errors.Is(err, kubernetesstore.ErrMaterializationNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if err := server.store.DeleteExact(cleanup, materialized); err != nil {
			return err
		}
	}
	return nil
}

// CheckReadiness probes only local storage so an adjacent owner/recovery outage cannot create a startup cycle;
// operational downstream failures remain errors of their own RPC paths.
// CheckReadiness проверяет лишь local storage, исключая startup cycle от соседних owner/recovery outage;
// рабочие downstream failures остаются ошибками собственных RPC paths.
func (server *Server) CheckReadiness(ctx context.Context, _ *secretbrokerv1.CheckReadinessRequest) (*secretbrokerv1.CheckReadinessResponse, error) {
	if err := server.store.Check(ctx); err != nil {
		return &secretbrokerv1.CheckReadinessResponse{Ready: false}, status.Error(codes.Unavailable, "secret broker local infrastructure is unavailable")
	}
	return &secretbrokerv1.CheckReadinessResponse{Ready: true}, nil
}

// validateOperation checks the expected kind, scoped namespace/resource/claim coordinates and kind-specific
// descriptor shape. LeaseDeadline must be a valid protobuf timestamp, not necessarily in the future;
// authentication, actual expiry and authoritative ownership are enforced by the owner consume path.
// validateOperation проверяет ожидаемые kind, scoped namespace/resource/claim coordinates и форму
// descriptor по kind. LeaseDeadline обязан быть корректным protobuf timestamp, не обязательно будущим;
// authentication, фактический expiry и authoritative ownership обеспечивает owner consume path.
func validateOperation(operation *controlplanev1.ConsumeRuntimeSecretOperationResponse, expected controlplanev1.RuntimeSecretOperationKind, namespace string) error {
	if operation == nil || operation.GetOperationRef() == "" || operation.GetKind() != expected ||
		runtimesecret.ValidateScope(runtimesecret.ScopeKind(strings.TrimPrefix(operation.GetScopeKind().String(), "RUNTIME_RESOURCE_SCOPE_KIND_")), operation.GetOrganizationRef(), operation.GetProjectRef()) != nil ||
		operation.GetNamespace() != namespace || operation.GetSecretRef() == "" || operation.GetSecretKey() == "" ||
		operation.GetTargetRevision() < 1 || operation.GetClaimGeneration() < 1 || operation.GetLeaseDeadline() == nil {
		return status.Error(codes.FailedPrecondition, "secret operation does not match request")
	}
	if err := operation.GetLeaseDeadline().CheckValid(); err != nil {
		return status.Error(codes.FailedPrecondition, "secret operation lease is invalid")
	}
	switch expected {
	case controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE,
		controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE:
		if !validDigest(operation.GetExpectedContentSha256()) || len(operation.GetRevisionDescriptors()) != 1 {
			return status.Error(codes.FailedPrecondition, "secret mutation intent is invalid")
		}
		descriptor := operation.GetRevisionDescriptors()[0]
		if descriptor.GetRevision() != operation.GetTargetRevision() || descriptor.GetNamespace() != namespace ||
			descriptor.GetSecretName() == "" || descriptor.GetSecretKey() != operation.GetSecretKey() ||
			descriptor.GetContentSha256() != operation.GetExpectedContentSha256() || descriptor.GetSecretUid() != "" || descriptor.GetSecretResourceVersion() != "" {
			return status.Error(codes.FailedPrecondition, "secret mutation descriptor is invalid")
		}
	case controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL:
		if operation.GetExpectedContentSha256() != "" || len(operation.GetRevisionDescriptors()) != 1 ||
			operation.GetRevisionDescriptors()[0].GetRevision() != operation.GetTargetRevision() {
			return status.Error(codes.FailedPrecondition, "secret reveal descriptor is invalid")
		}
	case controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE:
		if operation.GetExpectedContentSha256() != "" || len(operation.GetRevisionDescriptors()) == 0 {
			return status.Error(codes.FailedPrecondition, "secret revoke descriptors are invalid")
		}
	default:
		return status.Error(codes.FailedPrecondition, "secret operation kind is unsupported")
	}
	return nil
}

// exactDescriptor requires a revision within the operation's namespace/key plus UID, resource version
// and canonical digest, then copies those pins without reading storage or issuing delete authority.
// exactDescriptor требует revision в namespace/key операции, UID, resource version и canonical digest,
// затем копирует pins без чтения storage и без выдачи delete authority.
func exactDescriptor(operation *controlplanev1.ConsumeRuntimeSecretOperationResponse, revision *controlplanev1.RuntimeSecretRevisionDescriptor) (kubernetesstore.ExactDescriptor, error) {
	if operation == nil || revision == nil || revision.GetNamespace() != operation.GetNamespace() ||
		revision.GetSecretName() == "" || revision.GetSecretKey() != operation.GetSecretKey() || revision.GetRevision() < 1 ||
		revision.GetSecretUid() == "" || revision.GetSecretResourceVersion() == "" || !validDigest(revision.GetContentSha256()) {
		return kubernetesstore.ExactDescriptor{}, status.Error(codes.FailedPrecondition, "secret revision descriptor is invalid")
	}
	return kubernetesstore.ExactDescriptor{
		Namespace: revision.GetNamespace(), Name: revision.GetSecretName(), SecretRef: operation.GetSecretRef(),
		Key: revision.GetSecretKey(), Revision: revision.GetRevision(), UID: revision.GetSecretUid(),
		ResourceVersion: revision.GetSecretResourceVersion(), ContentSHA256: revision.GetContentSha256(),
	}, nil
}

// castMaterialization copies the storage result's exact object pins into the owner completion DTO;
// it performs no further lookup, digest computation or authority validation.
// castMaterialization копирует точные object pins storage result в owner completion DTO;
// дополнительный lookup, вычисление digest и authority validation не выполняются.
func castMaterialization(value kubernetesstore.Materialization) *controlplanev1.RuntimeSecretMaterialization {
	return &controlplanev1.RuntimeSecretMaterialization{
		Namespace: value.Namespace, SecretName: value.Name, SecretKey: value.Key,
		SecretUid: value.UID, SecretResourceVersion: value.ResourceVersion, ContentSha256: value.ContentSHA256,
	}
}

// contentDigestMatches requires a canonical lowercase SHA-256 and compares it with the exact value bytes
// using ConstantTimeCompare; it does not replace value-type or owner checks.
// contentDigestMatches требует canonical lowercase SHA-256 и сравнивает его с точными value bytes
// через ConstantTimeCompare; value-type и owner checks этим не заменяются.
func contentDigestMatches(expected string, value []byte) bool {
	if !validDigest(expected) {
		return false
	}
	digest := sha256.Sum256(value)
	actual := hex.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

// validDigest accepts exactly 32 decoded bytes in lowercase hex, including the all-zero digest;
// it validates representation, not correspondence to an unseen value.
// validDigest принимает ровно 32 decoded bytes в lowercase hex, включая нулевой digest;
// проверяется представление, не соответствие неизвестному значению.
func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}

// validateValue requires UTF-8 for STRING, syntactically valid JSON for JSON and no content rule for BINARY.
// Size is checked by mutate; JSON duplicate keys and application schemas are not validated here.
// validateValue требует UTF-8 для STRING, синтаксически корректный JSON для JSON и не ограничивает BINARY content.
// Размер проверяет mutate; JSON duplicate keys и прикладные schemas здесь не проверяются.
func validateValue(kind controlplanev1.RuntimeSecretValueType, value []byte) error {
	switch kind {
	case controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING:
		if !utf8.Valid(value) {
			return status.Error(codes.InvalidArgument, "string secret must contain valid UTF-8")
		}
	case controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_BINARY:
		return nil
	case controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_JSON:
		if !json.Valid(value) {
			return status.Error(codes.InvalidArgument, "JSON secret value is invalid")
		}
	default:
		return status.Error(codes.FailedPrecondition, "secret value type is unsupported")
	}
	return nil
}

// displayHint exposes only a valid STRING prefix/suffix within floor(15% of runes), capped at twelve,
// and suppresses hints below two visible runes; it is intentional bounded disclosure, not redaction.
// displayHint выдаёт лишь prefix/suffix корректного STRING в пределах floor(15% рун), не более двенадцати,
// подавляя подсказку меньше двух видимых рун; это намеренное ограниченное раскрытие, не redaction.
func displayHint(kind controlplanev1.RuntimeSecretValueType, value []byte) *controlplanev1.RuntimeSecretDisplayHint {
	if kind != controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING || !utf8.Valid(value) {
		return nil
	}
	runes := []rune(string(value))
	budget := len(runes) * 15 / 100
	if budget > 12 {
		budget = 12
	}
	if budget < 2 {
		return nil
	}
	prefixCount := (budget + 1) / 2
	suffixCount := budget - prefixCount
	return &controlplanev1.RuntimeSecretDisplayHint{Prefix: string(runes[:prefixCount]), Suffix: string(runes[len(runes)-suffixCount:])}
}

// castSecret copies owner metadata, numeric enum alignment, timestamps and an optional display hint.
// It handles nil but does not independently validate the owner DTO, version signs or enum membership.
// castSecret копирует owner metadata, числовое enum alignment, timestamps и необязательный display hint.
// Nil допускается, но owner DTO, знаки версий и enum membership отдельно не проверяются.
func castSecret(value *controlplanev1.RuntimeSecret) *secretbrokerv1.RuntimeSecretMetadata {
	if value == nil {
		return nil
	}
	result := &secretbrokerv1.RuntimeSecretMetadata{
		ScopeKind: secretbrokerv1.RuntimeResourceScopeKind(value.GetScopeKind()), OrganizationRef: value.GetOrganizationRef(),
		SecretRef: value.GetRef(), ProjectRef: value.GetProjectRef(), Name: value.GetName(),
		ValueType: castValueType(value.GetValueType()), Status: castStatus(value.GetState()), Revision: uint64(value.GetCurrentRevision()),
		CreatedAt: cloneTimestamp(value.GetCreatedAt()), UpdatedAt: cloneTimestamp(value.GetUpdatedAt()),
		Description: value.GetDescription(), Version: value.GetVersion(),
	}
	if hint := value.GetDisplayHint(); hint != nil {
		result.DisplayHint = &secretbrokerv1.RuntimeSecretDisplayHint{Prefix: hint.GetPrefix(), Suffix: hint.GetSuffix()}
	}
	return result
}

// cloneTimestamp copies seconds/nanos or preserves nil without checking protobuf validity.
// cloneTimestamp копирует seconds/nanos либо сохраняет nil без проверки protobuf validity.
func cloneTimestamp(value *timestamppb.Timestamp) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return &timestamppb.Timestamp{Seconds: value.Seconds, Nanos: value.Nanos}
}

// castValueType preserves the contracts' numeric enum representation without rejecting unknown values.
// castValueType сохраняет числовое enum representation контрактов без отказа неизвестным значениям.
func castValueType(value controlplanev1.RuntimeSecretValueType) secretbrokerv1.RuntimeSecretValueType {
	return secretbrokerv1.RuntimeSecretValueType(value)
}

// castStatus maps only ACTIVE/REVOKED and returns UNSPECIFIED for every other owner state.
// castStatus отображает лишь ACTIVE/REVOKED, возвращая UNSPECIFIED для любого иного owner state.
func castStatus(value string) secretbrokerv1.RuntimeSecretStatus {
	if value == "ACTIVE" {
		return secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_ACTIVE
	}
	if value == "REVOKED" {
		return secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_REVOKED
	}
	return secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_UNSPECIFIED
}

// preserveOwnerError returns any existing gRPC status unchanged, including its text/details, and
// replaces ordinary errors with fixed Unavailable. It is not a sanitizer for owner status diagnostics.
// preserveOwnerError возвращает существующий gRPC status без изменений, включая text/details,
// заменяя обычные ошибки фиксированным Unavailable. Owner status diagnostics она не очищает.
func preserveOwnerError(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Error(codes.Unavailable, "runtime secret owner is unavailable")
}
