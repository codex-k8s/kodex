package grpc

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// BeginManagedKubernetesIdentitySetup returns the sole owner-issued permit without accepting material.
// Replays preserve metadata only; no response projection can manufacture another upload authority.
// BeginManagedKubernetesIdentitySetup возвращает единственный owner permit без приёма material.
// Replay сохраняет только metadata; response projection не создаёт другую upload authority.
func (server *Server) BeginManagedKubernetesIdentitySetup(ctx context.Context, request *cp.BeginManagedKubernetesIdentitySetupRequest) (*cp.BeginManagedKubernetesIdentitySetupResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesIdentitySetupService_BeginManagedKubernetesIdentitySetup_FullMethodName, func(p value.Principal) (*cp.BeginManagedKubernetesIdentitySetupResponse, error) {
		result, err := server.service.BeginKubernetesIdentitySetup(ctx, p, mutation(request.GetMutation()), command.KubernetesIdentitySetupBegin{
			ConnectionRef: request.GetConnectionRef(), ExpectedConnectionVersion: request.GetExpectedConnectionVersion(), ContentSHA256: request.GetContentSha256(), ContentBytes: request.GetContentBytes()})
		if err != nil {
			return nil, err
		}
		defer clear(result.Permit)
		view, err := kubernetesIdentitySetupToProto(result.View)
		if err != nil {
			return nil, err
		}
		response := &cp.BeginManagedKubernetesIdentitySetupResponse{Setup: view, ReconcileOnly: result.ReconcileOnly}
		if result.ReconcileOnly {
			if len(result.Permit) != 0 || result.Endpoint.ServerOrigin != "" {
				return nil, errs.ErrUnavailable
			}
			return response, nil
		}
		if len(result.Permit) != 32 || result.Endpoint.Validate(time.Now().UTC()) != nil || result.View.State != "PREPARED" {
			return nil, errs.ErrUnavailable
		}
		response.UploadPermit = append([]byte(nil), result.Permit...)
		response.Endpoint = &cp.ManagedKubernetesIdentityUploadEndpoint{ServerOrigin: result.Endpoint.ServerOrigin, TlsServerName: result.Endpoint.TLSServerName,
			CertificateAuthorityPem: append([]byte(nil), result.Endpoint.CertificateAuthorityPEM...), CertificateAuthoritySha256: result.Endpoint.CertificateAuthoritySHA256,
			AddressIps: append([]string(nil), result.Endpoint.AddressIPs...)}
		return response, nil
	})
}

// GetManagedKubernetesIdentitySetup reads one self-owned setup or its original Begin receipt.
// The owner resolves visibility afresh and never recovers a permit through this method.
// GetManagedKubernetesIdentitySetup читает один собственный setup либо исходный Begin receipt.
// Owner заново проверяет visibility и никогда не восстанавливает permit через этот метод.
func (server *Server) GetManagedKubernetesIdentitySetup(ctx context.Context, request *cp.GetManagedKubernetesIdentitySetupRequest) (*cp.GetManagedKubernetesIdentitySetupResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesIdentitySetupService_GetManagedKubernetesIdentitySetup_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesIdentitySetupResponse, error) {
		result, err := server.service.GetKubernetesIdentitySetup(ctx, p, command.KubernetesIdentitySetupRead{ConnectionRef: request.GetConnectionRef(), SetupRef: request.GetSetupRef(), BeginIdempotencyKey: request.GetBeginIdempotencyKey()})
		if err != nil {
			return nil, err
		}
		view, err := kubernetesIdentitySetupToProto(result)
		if err != nil {
			return nil, err
		}
		return &cp.GetManagedKubernetesIdentitySetupResponse{Setup: view}, nil
	})
}

// CancelManagedKubernetesIdentitySetup closes the exact owner setup before late facts can bind.
// Cancellation records no claim that an unknown external material write has disappeared.
// CancelManagedKubernetesIdentitySetup закрывает точный owner setup до привязки late facts.
// Cancellation не утверждает исчезновение неизвестной внешней material write.
func (server *Server) CancelManagedKubernetesIdentitySetup(ctx context.Context, request *cp.CancelManagedKubernetesIdentitySetupRequest) (*cp.CancelManagedKubernetesIdentitySetupResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesIdentitySetupService_CancelManagedKubernetesIdentitySetup_FullMethodName, func(p value.Principal) (*cp.CancelManagedKubernetesIdentitySetupResponse, error) {
		result, err := server.service.CancelKubernetesIdentitySetup(ctx, p, mutation(request.GetMutation()), command.KubernetesIdentitySetupCancel{ConnectionRef: request.GetConnectionRef(), SetupRef: request.GetSetupRef()})
		if err != nil {
			return nil, err
		}
		view, err := kubernetesIdentitySetupToProto(result)
		if err != nil {
			return nil, err
		}
		return &cp.CancelManagedKubernetesIdentitySetupResponse{Setup: view}, nil
	})
}

// ResolveManagedKubernetesIdentityUpload exposes the immutable private setup only to its broker path.
// Resolving metadata does not admit a storage write; Claim remains a separate owner transaction.
// ResolveManagedKubernetesIdentityUpload раскрывает immutable private setup только broker path.
// Чтение metadata не допускает storage write; Claim остаётся отдельной owner transaction.
func (server *Server) ResolveManagedKubernetesIdentityUpload(ctx context.Context, request *cp.ResolveManagedKubernetesIdentityUploadRequest) (*cp.ResolveManagedKubernetesIdentityUploadResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesIdentitySetupService_ResolveManagedKubernetesIdentityUpload_FullMethodName, func(p value.Principal) (*cp.ResolveManagedKubernetesIdentityUploadResponse, error) {
		result, err := server.service.ResolveKubernetesIdentityUpload(ctx, p, command.KubernetesIdentityUploadResolve{SetupRef: request.GetSetupRef(), PermitSHA256: request.GetPermitSha256()})
		if err != nil {
			return nil, err
		}
		view, err := kubernetesIdentitySnapshotToProto(result)
		if err != nil {
			return nil, err
		}
		return &cp.ResolveManagedKubernetesIdentityUploadResponse{Snapshot: view}, nil
	})
}

// ClaimManagedKubernetesIdentityUpload returns a single WRITE or separately qualified OBSERVE grant.
// No request flag can select WRITE or refresh an earlier admission fence.
// ClaimManagedKubernetesIdentityUpload возвращает один WRITE либо отдельно допущенный OBSERVE grant.
// Request flag не выбирает WRITE и не обновляет прежний admission fence.
func (server *Server) ClaimManagedKubernetesIdentityUpload(ctx context.Context, request *cp.ClaimManagedKubernetesIdentityUploadRequest) (*cp.ClaimManagedKubernetesIdentityUploadResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesIdentitySetupService_ClaimManagedKubernetesIdentityUpload_FullMethodName, func(p value.Principal) (*cp.ClaimManagedKubernetesIdentityUploadResponse, error) {
		result, err := server.service.ClaimKubernetesIdentityUpload(ctx, p, command.KubernetesIdentityUploadClaim{SetupRef: request.GetSetupRef(), PermitSHA256: request.GetPermitSha256(), SnapshotDigest: request.GetSnapshotDigest(), ContentSHA256: request.GetContentSha256(), WorkloadInstance: request.GetWorkloadInstance()})
		if err != nil {
			return nil, err
		}
		provision := kubernetesIdentityProvisionToProto(result.Provision)
		expires := managedKubernetesTimestamp(result.ExpiresAt)
		if provision == nil || expires == nil || !managedkubernetes.ValidLocator(result.SetupRef) || !managedkubernetes.ValidLocator(result.AttemptRef) ||
			!managedkubernetes.ValidLocator(result.Fence) || result.Generation < 1 || result.Generation > 8 {
			return nil, errs.ErrUnavailable
		}
		response := &cp.ManagedKubernetesIdentityUploadAdmission{SetupRef: result.SetupRef, AttemptRef: result.AttemptRef, Generation: result.Generation, Fence: result.Fence,
			ExpiresAt: expires, ReconcileOnly: result.ReconcileOnly, Provision: provision}
		if result.KnownIdentity != nil {
			if !result.ReconcileOnly {
				return nil, errs.ErrUnavailable
			}
			response.KnownIdentity = managedKubernetesIdentityToProto(*result.KnownIdentity, result.Provision.IdentityRef, result.Provision.TargetRef)
			if response.KnownIdentity == nil || result.KnownIdentity.Revision != result.Provision.Revision || result.KnownIdentity.ContentSHA256 != result.Provision.ContentSHA256 || result.KnownIdentity.Namespace != result.Provision.Namespace {
				return nil, errs.ErrUnavailable
			}
		}
		return &cp.ClaimManagedKubernetesIdentityUploadResponse{Admission: response}, nil
	})
}

// CompleteManagedKubernetesIdentityUpload retains exactly one typed external fact under its fence.
// Empty, future or ambiguous outcomes cannot be downgraded to observation-eligible UNKNOWN.
// CompleteManagedKubernetesIdentityUpload сохраняет ровно один typed external fact под его fence.
// Пустой, будущий либо неоднозначный outcome не превращается в допускающий observation UNKNOWN.
func (server *Server) CompleteManagedKubernetesIdentityUpload(ctx context.Context, request *cp.CompleteManagedKubernetesIdentityUploadRequest) (*cp.CompleteManagedKubernetesIdentityUploadResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	input := command.KubernetesIdentityUploadComplete{SetupRef: request.GetSetupRef(), AttemptRef: request.GetAttemptRef(), Generation: request.GetGeneration(), Fence: request.GetFence(), WorkloadInstance: request.GetWorkloadInstance()}
	switch outcome := request.GetOutcome().(type) {
	case *cp.CompleteManagedKubernetesIdentityUploadRequest_IdentityLocator:
		if outcome.IdentityLocator == nil {
			return nil, transportError(errs.ErrInvalid)
		}
		pin := outcome.IdentityLocator
		input.Identity = &value.ManagedKubernetesIdentityLocator{IdentityRef: pin.GetIdentityRef(), TargetRef: pin.GetTargetRef(), Revision: pin.GetRevision(), Namespace: pin.GetNamespace(), Name: pin.GetSecretName(), UID: pin.GetSecretUid(), ResourceVersion: pin.GetSecretResourceVersion(), ContentSHA256: pin.GetContentSha256()}
	case *cp.CompleteManagedKubernetesIdentityUploadRequest_Unknown:
		input.Unknown = outcome.Unknown != nil
	case *cp.CompleteManagedKubernetesIdentityUploadRequest_RejectedAcknowledgement:
		if outcome.RejectedAcknowledgement != nil {
			witness := outcome.RejectedAcknowledgement
			input.RejectedAcknowledgement = &value.ManagedKubernetesAcknowledgementRejection{ResponseSHA256: witness.GetResponseSha256(), Truncated: witness.GetTruncated(), ObservedUID: witness.GetObservedUid()}
		}
	}
	return queryResponse(ctx, cp.ManagedKubernetesIdentitySetupService_CompleteManagedKubernetesIdentityUpload_FullMethodName, func(p value.Principal) (*cp.CompleteManagedKubernetesIdentityUploadResponse, error) {
		result, err := server.service.CompleteKubernetesIdentityUpload(ctx, p, input)
		if err != nil {
			return nil, err
		}
		view, err := kubernetesIdentitySetupToProto(result)
		if err != nil {
			return nil, err
		}
		return &cp.CompleteManagedKubernetesIdentityUploadResponse{Setup: view}, nil
	})
}

// kubernetesIdentitySetupToProto permits only the closed safe metadata shape, including history.
// It preserves the original expiry and never reports completion as successful target RBAC testing.
// kubernetesIdentitySetupToProto допускает только закрытую safe metadata форму, включая историю.
// Сохраняет original expiry и не объявляет completion успешной target RBAC проверкой.
func kubernetesIdentitySetupToProto(input entity.KubernetesIdentitySetupView) (*cp.ManagedKubernetesIdentitySetupView, error) {
	state, known := cp.ManagedKubernetesIdentitySetupState_value["MANAGED_KUBERNETES_IDENTITY_SETUP_STATE_"+string(input.State)]
	policy, knownPolicy := cp.IntegrationCredentialPolicy_value["INTEGRATION_CREDENTIAL_POLICY_"+string(input.Policy)]
	expires := managedKubernetesTimestamp(input.ExpiresAt)
	if !known || state == 0 || !knownPolicy || policy == 0 || expires == nil || !managedkubernetes.ValidLocator(input.Ref) || !managedkubernetes.ValidLocator(input.ConnectionRef) ||
		!managedkubernetes.ValidLocator(input.IdentityRef) || !managedkubernetes.ValidLocator(input.TargetRef) || !managedkubernetes.ValidLocator(input.CanonicalContext) || input.Version < 1 || input.ConnectionVersion < 1 || input.IdentityRevision < 1 || input.IdentityRevision > 9007199254740991 ||
		(input.Policy == entity.IntegrationCredentialInitiatorRequired && (input.BindingVersion < 1 || input.BindingGeneration < 1)) ||
		(input.Policy == entity.IntegrationCredentialProjectShared && (input.BindingVersion != 0 || input.BindingGeneration != 0)) {
		return nil, errs.ErrUnavailable
	}
	switch input.ReasonCode {
	case "", "SETUP_EXPIRED_PENDING_CLOSURE", "SETUP_COMPLETION_REQUIRED", "SETUP_ACK_REJECTED", "SETUP_ACK_CONFLICT":
	default:
		return nil, errs.ErrUnavailable
	}
	return &cp.ManagedKubernetesIdentitySetupView{SetupRef: input.Ref, ConnectionRef: input.ConnectionRef, ConnectionVersion: input.ConnectionVersion,
		BindingGeneration: input.BindingGeneration, IdentityRef: input.IdentityRef, IdentityRevision: input.IdentityRevision, TargetRef: input.TargetRef,
		State: cp.ManagedKubernetesIdentitySetupState(state), ExpiresAt: expires, ReasonCode: input.ReasonCode, Version: input.Version,
		CredentialPolicy: cp.IntegrationCredentialPolicy(policy), BindingVersion: input.BindingVersion, CanonicalContext: input.CanonicalContext}, nil
}

// kubernetesIdentityProvisionToProto refuses partial private materialization pins without defaults.
// kubernetesIdentityProvisionToProto отклоняет неполные private materialization pins без defaults.
func kubernetesIdentityProvisionToProto(input value.ManagedKubernetesIdentityProvision) *cp.ManagedKubernetesIdentityProvision {
	if input.Validate(input.Namespace) != nil {
		return nil
	}
	return &cp.ManagedKubernetesIdentityProvision{IdentityRef: input.IdentityRef, TargetRef: input.TargetRef, Revision: input.Revision, Namespace: input.Namespace, ContentSha256: input.ContentSHA256}
}

// kubernetesIdentitySnapshotToProto separates completed safe history from uncompleted private input.
// Known storage pins must match the exact immutable provision; no replacement UID is inferred.
// kubernetesIdentitySnapshotToProto отделяет completed safe history от незавершённого private input.
// Known storage pins обязаны совпасть с exact immutable provision; replacement UID не выводится.
func kubernetesIdentitySnapshotToProto(input entity.KubernetesIdentityUploadSnapshot) (*cp.ManagedKubernetesIdentityUploadSnapshot, error) {
	if !managedkubernetes.ValidLocator(input.SetupRef) {
		return nil, errs.ErrUnavailable
	}
	response := &cp.ManagedKubernetesIdentityUploadSnapshot{SetupRef: input.SetupRef, ReconcileOnly: input.ReconcileOnly}
	if input.Completed != nil {
		if !input.ReconcileOnly || input.Completed.Ref != input.SetupRef || input.Completed.State != "COMPLETED" || input.KnownIdentity != nil {
			return nil, errs.ErrUnavailable
		}
		view, err := kubernetesIdentitySetupToProto(*input.Completed)
		response.Completed = view
		return response, err
	}
	response.Provision = kubernetesIdentityProvisionToProto(input.Provision)
	response.ExpiresAt = managedKubernetesTimestamp(input.ExpiresAt)
	if response.Provision == nil || response.ExpiresAt == nil || !managedkubernetes.ValidDigest(input.Digest) || input.ContentBytes == 0 || input.ContentBytes > managedkubernetes.MaximumCredentialBytes ||
		input.Target.Ref != input.Provision.TargetRef || input.Target.Revision < 1 || !managedkubernetes.ValidDigest(input.Target.CertificateAuthoritySHA256) {
		return nil, errs.ErrUnavailable
	}
	response.SnapshotDigest, response.ContentBytes = input.Digest, input.ContentBytes
	response.Target = &cp.ManagedKubernetesTargetSnapshot{TargetRef: input.Target.Ref, Revision: input.Target.Revision, ServerOrigin: input.Target.ServerOrigin, TlsServerName: input.Target.TLSServerName,
		CertificateAuthoritySha256: input.Target.CertificateAuthoritySHA256, AddressIps: append([]string(nil), input.Target.AddressIPs...), EmbeddedContext: input.Target.EmbeddedContext, ClusterUid: input.Target.ClusterUID, KubeconfigSourceOrigins: append([]string(nil), input.Target.KubeconfigSourceOrigins...)}
	if input.KnownIdentity != nil {
		response.KnownIdentity = managedKubernetesIdentityToProto(*input.KnownIdentity, input.Provision.IdentityRef, input.Provision.TargetRef)
		if !input.ReconcileOnly || response.KnownIdentity == nil || input.KnownIdentity.Revision != input.Provision.Revision || input.KnownIdentity.ContentSHA256 != input.Provision.ContentSHA256 || input.KnownIdentity.Namespace != input.Provision.Namespace {
			return nil, errs.ErrUnavailable
		}
	}
	return response, nil
}
