package grpc

import (
	"context"
	"errors"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// providerMaterializerUnavailableMessage keeps absent-capability and unexpected materializer failures on the same fixed public diagnostic.
	// providerMaterializerUnavailableMessage сохраняет одну fixed public diagnostic для absent capability и unexpected materializer failures.
	providerMaterializerUnavailableMessage = "provider credential materializer is unavailable"
	// providerCleanupErrorDomain is the fixed ErrorInfo domain for the cleanup CAS-change signal, not
	// caller-provided diagnostic metadata.
	// providerCleanupErrorDomain — фиксированный ErrorInfo domain для cleanup CAS-change signal, не diagnostic
	// metadata по выбору caller.
	providerCleanupErrorDomain = "kodex.provider_credential_cleanup"
	// providerCleanupCASChangedReason identifies a rejected pre-fence snapshot change; generic conflicts,
	// arbitrary matching text and uncertain writes must not acquire this no-effect signal.
	// providerCleanupCASChangedReason обозначает отклонённое pre-fence snapshot change; generic conflicts,
	// совпавший произвольный текст и uncertain writes не должны получать этот no-effect signal.
	providerCleanupCASChangedReason = "CAS_SNAPSHOT_CHANGED"
)

// CheckProviderCredentialMaterializerReadiness requires a configured materializer and its Check success,
// returning Ready=false plus fixed Unavailable otherwise; it does not start authorization.
// CheckProviderCredentialMaterializerReadiness требует настроенный materializer и успешный Check, иначе
// возвращает Ready=false с fixed Unavailable; authorization не запускает.
func (server *Server) CheckProviderCredentialMaterializerReadiness(
	ctx context.Context,
	_ *controlplanev1.CheckProviderCredentialMaterializerReadinessRequest,
) (*controlplanev1.CheckProviderCredentialMaterializerReadinessResponse, error) {
	if server.providerCredentials == nil || server.providerCredentials.Check(ctx) != nil {
		return &controlplanev1.CheckProviderCredentialMaterializerReadinessResponse{Ready: false},
			status.Error(codes.Unavailable, providerMaterializerUnavailableMessage)
	}
	return &controlplanev1.CheckProviderCredentialMaterializerReadinessResponse{Ready: true}, nil
}

// StartDeviceAuthorization forwards the exact attempt/account to the configured materializer and returns
// its device-flow locator/code/expiry/object pins, mapping failures without exposing credential bytes.
// StartDeviceAuthorization передаёт exact attempt/account настроенному materializer и возвращает
// device-flow locator/code/expiry/object pins, отображая failures без выдачи credential bytes.
func (server *Server) StartDeviceAuthorization(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceStartDeviceAuthorizationRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceStartDeviceAuthorizationResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, providerMaterializerUnavailableMessage)
	}
	result, err := server.providerCredentials.StartDeviceAuthorization(ctx, request.GetAttemptRef(), request.GetAccountRef())
	if err != nil {
		return nil, providerCredentialError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceStartDeviceAuthorizationResponse{
		MaterializerAttemptRef:             result.MaterializerAttemptRef,
		VerificationUri:                    result.VerificationURI,
		UserCode:                           result.UserCode,
		ExpiresAt:                          timestamppb.New(result.ExpiresAt),
		MaterializerAttemptUid:             result.MaterializerAttemptUID,
		MaterializerAttemptResourceVersion: result.MaterializerAttemptVersion,
	}, nil
}

// ObserveDeviceAuthorization separates metadata-only cleanup observation from POLL; mixed/unknown poll
// fields are refused. Metadata mode returns presence/absence pins and optional produced credential
// descriptor, never polling state or account diagnostics.
// ObserveDeviceAuthorization отделяет metadata-only cleanup observation от POLL и отклоняет mixed/unknown
// poll fields. Metadata mode возвращает presence/absence pins и optional produced credential descriptor,
// не polling state и не account diagnostics.
func (server *Server) ObserveDeviceAuthorization(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, providerMaterializerUnavailableMessage)
	}
	if request.GetMode() == controlplanev1.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_METADATA_ONLY {
		target := kubernetesstore.ProviderAuthorizationCleanupTarget{TaskRef: request.GetTaskRef(), AccountRef: request.GetAccountRef(),
			AuthorizationAttemptRef: request.GetAuthorizationAttemptRef(), MaterializerAttemptRef: request.GetMaterializerAttemptRef(),
			Generation: request.GetLeaseGeneration()}
		result, err := server.providerCredentials.ObserveAuthorizationCleanup(ctx, target)
		if err != nil {
			return nil, providerCredentialCleanupError(err)
		}
		response := &controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse{
			Credential: providerCredentialDescriptor(result.ProducedCredential),
		}
		switch result.State {
		case kubernetesstore.ProviderAuthorizationPresent:
			response.ObjectState = controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_PRESENT
			response.PendingObject = &controlplanev1.ProviderAuthorizationObjectDescriptor{AccountRef: result.Target.AccountRef,
				AuthorizationAttemptRef: result.Target.AuthorizationAttemptRef, MaterializerAttemptRef: result.Target.MaterializerAttemptRef,
				Uid: result.Target.UID, ResourceVersion: result.Target.ResourceVersion}
		case kubernetesstore.ProviderAuthorizationAbsent, kubernetesstore.ProviderAuthorizationFenced:
			response.ObjectState = controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_ABSENT_UNFENCED
			if result.State == kubernetesstore.ProviderAuthorizationFenced {
				response.ObjectState = controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_CONFIRMED_ABSENT
			}
			response.AbsentObject = &controlplanev1.ProviderAuthorizationAbsenceDescriptor{AccountRef: result.Target.AccountRef,
				AuthorizationAttemptRef: result.Target.AuthorizationAttemptRef, MaterializerAttemptRef: result.Target.MaterializerAttemptRef}
		default:
			return nil, providerCredentialCleanupError(kubernetesstore.ErrProviderCredentialCleanupConflict)
		}
		return response, nil
	}
	if request.GetMode() != controlplanev1.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_POLL ||
		request.GetAccountRef() != "" || request.GetAuthorizationAttemptRef() != "" || request.GetTaskRef() != "" || request.GetLeaseGeneration() != 0 {
		return nil, providerCredentialCleanupError(kubernetesstore.ErrProviderCredentialCleanupInvalid)
	}
	result, err := server.providerCredentials.ObserveDeviceAuthorization(ctx, request.GetMaterializerAttemptRef())
	if err != nil {
		return nil, providerCredentialError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse{
		State:                 providerAuthorizationState(result.State),
		Credential:            providerCredentialDescriptor(result.Credential),
		ExternalAccountMasked: result.ExternalAccountMasked,
		SafeFailureCode:       result.SafeFailureCode,
	}, nil
}

// MaterializeAPIKey copies the request key for the materializer, clears that private copy on return and
// emits only descriptor/masked account; it does not clear the caller's original request slice itself.
// MaterializeAPIKey копирует request key для materializer, очищает private copy при return и выдаёт лишь
// descriptor/masked account; исходный request slice caller самостоятельно не очищает.
func (server *Server) MaterializeAPIKey(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceMaterializeAPIKeyRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceMaterializeAPIKeyResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, providerMaterializerUnavailableMessage)
	}
	apiKey := append([]byte(nil), request.GetApiKey()...)
	defer clear(apiKey)
	credential, masked, err := server.providerCredentials.MaterializeAPIKey(ctx, request.GetAttemptRef(), request.GetAccountRef(), apiKey)
	if err != nil {
		return nil, providerCredentialError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceMaterializeAPIKeyResponse{
		Credential: providerCredentialDescriptor(&credential), ExternalAccountMasked: masked,
	}, nil
}

// DiscardProviderCredentialMaterialization copies the supplied attempt and optional exact credential pins
// into a typed discard request, delegates once and returns Discarded only after port success.
// DiscardProviderCredentialMaterialization копирует attempt и optional exact credential pins в typed
// discard request, однократно делегирует и возвращает Discarded лишь после port success.
func (server *Server) DiscardProviderCredentialMaterialization(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceDiscardMaterializationRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceDiscardMaterializationResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, providerMaterializerUnavailableMessage)
	}
	target := providercredential.DiscardMaterialization{
		AttemptRef: request.GetAttemptRef(), AccountRef: request.GetAccountRef(),
		MaterializerAttemptRef:     request.GetMaterializerAttemptRef(),
		MaterializerAttemptUID:     request.GetMaterializerAttemptUid(),
		MaterializerAttemptVersion: request.GetMaterializerAttemptResourceVersion(),
	}
	if credential := request.GetCredential(); credential != nil {
		target.Credential = &kubernetesstore.ProviderCredentialDescriptor{
			SecretName: credential.GetSecretName(), SecretUID: credential.GetSecretUid(),
			SecretResourceVersion: credential.GetSecretResourceVersion(), ContentSHA256: credential.GetContentSha256(),
		}
	}
	if err := server.providerCredentials.Discard(ctx, target); err != nil {
		return nil, providerCredentialError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceDiscardMaterializationResponse{Discarded: true}, nil
}

// CleanupProviderCredential validates recovery identity and exactly one typed credential/pending/absence
// target, then forwards the original task/account/generation and exact pins. The materializer owns
// fencing/effects/receipt provenance; this wrapper only maps its result/error.
// CleanupProviderCredential проверяет recovery identity и ровно один typed credential/pending/absence
// target, затем передаёт original task/account/generation и exact pins. Fencing/effects/receipt provenance
// принадлежат materializer; wrapper лишь отображает result/error.
func (server *Server) CleanupProviderCredential(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, providerMaterializerUnavailableMessage)
	}
	var result kubernetesstore.ProviderCredentialCleanupResult
	var err error
	recovery := kubernetesstore.ProviderCleanupRecoveryIdentity{TaskRef: request.GetRecoveryIdentity().GetTaskRef(),
		Generation: request.GetRecoveryIdentity().GetLeaseGeneration(), LegacyLastGeneration: request.GetRecoveryIdentity().GetLegacyLastGeneration()}
	if !recovery.Valid(request.GetTaskRef(), request.GetLeaseGeneration()) {
		return nil, providerCredentialCleanupError(kubernetesstore.ErrProviderCredentialCleanupInvalid)
	}
	switch request.GetTargetKind() {
	case controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_CREDENTIAL:
		credential := request.GetCredential()
		if credential == nil || request.GetPendingObject() != nil || request.GetAbsentObject() != nil {
			return nil, providerCredentialCleanupError(kubernetesstore.ErrProviderCredentialCleanupInvalid)
		}
		descriptor := kubernetesstore.ProviderCredentialDescriptor{
			SecretName: credential.GetSecretName(), SecretUID: credential.GetSecretUid(),
			SecretResourceVersion: credential.GetSecretResourceVersion(), ContentSHA256: credential.GetContentSha256(),
		}
		result, err = server.providerCredentials.CleanupProviderCredentialWithRecovery(ctx, request.GetTaskRef(), request.GetAccountRef(), request.GetLeaseGeneration(), descriptor, recovery)
	case controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ATTEMPT:
		pending := request.GetPendingObject()
		if pending == nil || request.GetCredential() != nil || request.GetAbsentObject() != nil || pending.GetAccountRef() != request.GetAccountRef() {
			return nil, providerCredentialCleanupError(kubernetesstore.ErrProviderCredentialCleanupInvalid)
		}
		result, err = server.providerCredentials.CleanupAuthorizationWithRecovery(ctx, kubernetesstore.ProviderAuthorizationCleanupTarget{
			TaskRef: request.GetTaskRef(), AccountRef: request.GetAccountRef(), Generation: request.GetLeaseGeneration(),
			Kind: kubernetesstore.ProviderCleanupAuthorization, AuthorizationAttemptRef: pending.GetAuthorizationAttemptRef(),
			MaterializerAttemptRef: pending.GetMaterializerAttemptRef(), UID: pending.GetUid(), ResourceVersion: pending.GetResourceVersion(),
		}, recovery)
	case controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ABSENCE:
		absent := request.GetAbsentObject()
		if absent == nil || request.GetCredential() != nil || request.GetPendingObject() != nil || absent.GetAccountRef() != request.GetAccountRef() {
			return nil, providerCredentialCleanupError(kubernetesstore.ErrProviderCredentialCleanupInvalid)
		}
		result, err = server.providerCredentials.CleanupAuthorizationWithRecovery(ctx, kubernetesstore.ProviderAuthorizationCleanupTarget{
			TaskRef: request.GetTaskRef(), AccountRef: request.GetAccountRef(), Generation: request.GetLeaseGeneration(),
			Kind: kubernetesstore.ProviderCleanupAbsence, AuthorizationAttemptRef: absent.GetAuthorizationAttemptRef(),
			MaterializerAttemptRef: absent.GetMaterializerAttemptRef(),
		}, recovery)
	default:
		return nil, providerCredentialCleanupError(kubernetesstore.ErrProviderCredentialCleanupInvalid)
	}
	if err != nil {
		return nil, providerCredentialCleanupError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{
		TerminalReceipt: result.TerminalReceipt, ProducedCredential: providerCredentialDescriptor(result.ProducedCredential),
	}, nil
}

// providerAuthorizationState maps only PENDING/AUTHORIZED/EXPIRED/FAILED strings and returns UNSPECIFIED
// for every other materializer state.
// providerAuthorizationState отображает лишь PENDING/AUTHORIZED/EXPIRED/FAILED и возвращает UNSPECIFIED
// для остальных materializer states.
func providerAuthorizationState(state string) controlplanev1.ProviderAuthorizationState {
	switch state {
	case "PENDING":
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_PENDING
	case "AUTHORIZED":
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_AUTHORIZED
	case "EXPIRED":
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_EXPIRED
	case "FAILED":
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_FAILED
	default:
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_UNSPECIFIED
	}
}

// providerCredentialDescriptor preserves nil or copies exact name/UID/RV/digest metadata into protobuf
// without reading values or independently validating the descriptor.
// providerCredentialDescriptor сохраняет nil либо копирует exact name/UID/RV/digest metadata в protobuf
// без чтения values и самостоятельной проверки descriptor.
func providerCredentialDescriptor(value *kubernetesstore.ProviderCredentialDescriptor) *controlplanev1.ProviderCredentialDescriptor {
	if value == nil {
		return nil
	}
	return &controlplanev1.ProviderCredentialDescriptor{
		SecretName: value.SecretName, SecretUid: value.SecretUID,
		SecretResourceVersion: value.SecretResourceVersion, ContentSha256: value.ContentSHA256,
	}
}

// providerCredentialError maps known invalid/not-found/conflict sentinels to fixed gRPC statuses and every
// other failure to Unavailable, never interpolating upstream text; it is not a nil-success helper.
// providerCredentialError отображает known invalid/not-found/conflict sentinels в fixed gRPC statuses,
// остальные failures — в Unavailable без upstream text; nil-success helper не является.
func providerCredentialError(err error) error {
	switch {
	case errors.Is(err, providercredential.ErrInvalidInput),
		errors.Is(err, kubernetesstore.ErrProviderCredentialInputInvalid):
		return status.Error(codes.InvalidArgument, "provider credential materializer input is invalid")
	case errors.Is(err, providercredential.ErrNotFound), errors.Is(err, kubernetesstore.ErrProviderAttemptNotFound):
		return status.Error(codes.NotFound, "provider authorization attempt is not found")
	case errors.Is(err, providercredential.ErrConflict),
		errors.Is(err, kubernetesstore.ErrProviderAttemptConflict),
		errors.Is(err, kubernetesstore.ErrProviderCredentialConflict):
		return status.Error(codes.Aborted, "provider credential materialization conflicts with stored state")
	default:
		return status.Error(codes.Unavailable, providerMaterializerUnavailableMessage)
	}
}

// providerCredentialCleanupError attaches one fixed ErrorInfo only for the exact snapshot-changed
// sentinel, falling back to plain FailedPrecondition if details fail. Other invalid/conflict/unknown
// failures remain closed statuses without a fabricated no-effect receipt.
// providerCredentialCleanupError добавляет один fixed ErrorInfo лишь для exact snapshot-changed sentinel,
// при отказе details возвращая plain FailedPrecondition. Прочие invalid/conflict/unknown failures остаются
// closed statuses без выдуманного no-effect receipt.
func providerCredentialCleanupError(err error) error {
	switch {
	case errors.Is(err, kubernetesstore.ErrProviderAuthorizationCleanupSnapshotChanged):
		result := status.New(codes.FailedPrecondition, "provider authorization cleanup snapshot changed before fencing")
		withDetails, detailErr := result.WithDetails(&errdetails.ErrorInfo{
			Domain: providerCleanupErrorDomain, Reason: providerCleanupCASChangedReason,
		})
		if detailErr != nil {
			return result.Err()
		}
		return withDetails.Err()
	case errors.Is(err, providercredential.ErrInvalidInput),
		errors.Is(err, kubernetesstore.ErrProviderCredentialCleanupInvalid):
		return status.Error(codes.InvalidArgument, "provider credential cleanup input is invalid")
	case errors.Is(err, kubernetesstore.ErrProviderCredentialCleanupConflict):
		return status.Error(codes.FailedPrecondition, "provider credential cleanup conflicts with stored state")
	default:
		return status.Error(codes.Unavailable, "provider credential cleanup is unavailable")
	}
}
