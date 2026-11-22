package grpc

import (
	"context"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// BeginManagedKubernetesResourceMaterial forwards metadata to the current owner and exposes
// a one-shot direct-upload permit only on its original admission. No material enters this RPC.
// BeginManagedKubernetesResourceMaterial передаёт metadata current owner и выдаёт one-shot
// direct-upload permit лишь при original admission. Material не входит в этот RPC.
func (server *Server) BeginManagedKubernetesResourceMaterial(ctx context.Context, request *cp.BeginManagedKubernetesResourceMaterialRequest) (*cp.BeginManagedKubernetesResourceMaterialResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesResourceMaterialService_BeginManagedKubernetesResourceMaterial_FullMethodName, func(p value.Principal) (*cp.BeginManagedKubernetesResourceMaterialResponse, error) {
		result, err := server.service.BeginKubernetesResourceMaterial(ctx, p, mutation(request.GetMutation()), command.KubernetesResourceMaterialBegin{ConnectionRef: request.GetConnectionRef(), ExpectedConnectionVersion: request.GetExpectedConnectionVersion(), Operation: request.GetOperation(), Namespace: request.GetNamespace(), Name: request.GetName(), GenerateName: request.GetGenerateName(), SecretType: request.GetSecretType(), PatchType: request.GetPatchType(), ContentSHA256: request.GetContentSha256(), ContentBytes: request.GetContentBytes()})
		if err != nil {
			return nil, err
		}
		defer clear(result.Permit)
		view, err := kubernetesResourceMaterialToProto(result.View)
		if err != nil {
			return nil, err
		}
		response := &cp.ManagedKubernetesResourceMaterialAdmission{Material: view, ReconcileOnly: result.ReconcileOnly}
		if result.ReconcileOnly {
			if len(result.Permit) != 0 || result.Endpoint.ServerOrigin != "" {
				return nil, errs.ErrUnavailable
			}
			return &cp.BeginManagedKubernetesResourceMaterialResponse{Admission: response}, nil
		}
		if len(result.Permit) != 32 || result.View.State != enum.KubernetesResourceMaterialPrepared || result.Endpoint.Validate(time.Now().UTC()) != nil {
			return nil, errs.ErrUnavailable
		}
		response.UploadPermit = append([]byte(nil), result.Permit...)
		response.Endpoint = &cp.ManagedKubernetesIdentityUploadEndpoint{ServerOrigin: result.Endpoint.ServerOrigin, TlsServerName: result.Endpoint.TLSServerName, CertificateAuthorityPem: append([]byte(nil), result.Endpoint.CertificateAuthorityPEM...), CertificateAuthoritySha256: result.Endpoint.CertificateAuthoritySHA256, AddressIps: append([]string(nil), result.Endpoint.AddressIPs...)}
		return &cp.BeginManagedKubernetesResourceMaterialResponse{Admission: response}, nil
	})
}

// GetManagedKubernetesResourceMaterial reads actor-local safe history without reconstructing
// an upload permit, private material bytes or a target execution authorization.
// GetManagedKubernetesResourceMaterial читает actor-local safe history без восстановления
// upload permit, private material bytes либо target execution authorization.
func (server *Server) GetManagedKubernetesResourceMaterial(ctx context.Context, request *cp.GetManagedKubernetesResourceMaterialRequest) (*cp.GetManagedKubernetesResourceMaterialResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesResourceMaterialService_GetManagedKubernetesResourceMaterial_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesResourceMaterialResponse, error) {
		result, err := server.service.GetKubernetesResourceMaterial(ctx, p, command.KubernetesResourceMaterialRead{ConnectionRef: request.GetConnectionRef(), MaterialRef: request.GetMaterialRef(), BeginIdempotencyKey: request.GetBeginIdempotencyKey()})
		if err != nil {
			return nil, err
		}
		view, err := kubernetesResourceMaterialToProto(result)
		if err != nil {
			return nil, err
		}
		return &cp.GetManagedKubernetesResourceMaterialResponse{Material: view}, nil
	})
}

// CancelManagedKubernetesResourceMaterial closes future use through exact owner addressing/OCC.
// It preserves previous UNKNOWN/consumed facts instead of declaring remote effects undone.
// CancelManagedKubernetesResourceMaterial закрывает future use через exact owner addressing/OCC.
// Сохраняет previous UNKNOWN/consumed facts, не объявляя remote effects отменёнными.
func (server *Server) CancelManagedKubernetesResourceMaterial(ctx context.Context, request *cp.CancelManagedKubernetesResourceMaterialRequest) (*cp.CancelManagedKubernetesResourceMaterialResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesResourceMaterialService_CancelManagedKubernetesResourceMaterial_FullMethodName, func(p value.Principal) (*cp.CancelManagedKubernetesResourceMaterialResponse, error) {
		result, err := server.service.CancelKubernetesResourceMaterial(ctx, p, mutation(request.GetMutation()), command.KubernetesResourceMaterialCancel{ConnectionRef: request.GetConnectionRef(), MaterialRef: request.GetMaterialRef()})
		if err != nil {
			return nil, err
		}
		view, err := kubernetesResourceMaterialToProto(result)
		if err != nil {
			return nil, err
		}
		return &cp.CancelManagedKubernetesResourceMaterialResponse{Material: view}, nil
	})
}

// kubernetesResourceMaterialToProto closes the safe public projection while preserving original
// historical expiry. AVAILABLE is stored artifact metadata, never target readiness or effect success.
// kubernetesResourceMaterialToProto закрывает safe public projection с сохранением original
// historical expiry. AVAILABLE — stored artifact metadata, не target readiness/effect success.
func kubernetesResourceMaterialToProto(input entity.KubernetesResourceMaterialView) (*cp.ManagedKubernetesResourceMaterialView, error) {
	state, known := cp.ManagedKubernetesResourceMaterialState_value["MANAGED_KUBERNETES_RESOURCE_MATERIAL_STATE_"+string(input.State)]
	policy, knownPolicy := cp.IntegrationCredentialPolicy_value["INTEGRATION_CREDENTIAL_POLICY_"+string(input.Policy)]
	expires := managedKubernetesTimestamp(input.ExpiresAt)
	destination := command.KubernetesResourceMaterialBegin{Operation: input.Operation, Namespace: input.Namespace, Name: input.Name, GenerateName: input.GenerateName, SecretType: input.SecretType, PatchType: input.PatchType}
	if !known || state == 0 || !knownPolicy || policy == 0 || !input.State.Valid() || !enum.ValidKubernetesResourceMaterialReasonCode(input.ReasonCode) || expires == nil || !destination.ValidDestination() ||
		!managedkubernetes.ValidIdentityReference(input.Ref) || !strings.HasPrefix(input.Ref, "krs_") || !managedkubernetes.ValidIdentityReference(input.ConnectionRef) || !managedkubernetes.ValidIdentityReference(input.IdentityRef) || !managedkubernetes.ValidIdentityReference(input.TargetRef) ||
		!validMaterialPublicVersion(input.Version) || !validMaterialPublicVersion(input.ConnectionVersion) || !validMaterialPublicVersion(input.IdentityRevision) ||
		input.Policy == entity.IntegrationCredentialInitiatorRequired && !validMaterialPublicVersion(input.BindingVersion) || input.Policy == entity.IntegrationCredentialProjectShared && input.BindingVersion != 0 {
		return nil, errs.ErrUnavailable
	}
	return &cp.ManagedKubernetesResourceMaterialView{MaterialRef: input.Ref, ConnectionRef: input.ConnectionRef, ConnectionVersion: input.ConnectionVersion, IdentityRef: input.IdentityRef, IdentityRevision: input.IdentityRevision, TargetRef: input.TargetRef, Namespace: input.Namespace, Name: input.Name, GenerateName: input.GenerateName, Operation: input.Operation, SecretType: input.SecretType, PatchType: input.PatchType, State: cp.ManagedKubernetesResourceMaterialState(state), ExpiresAt: expires, Version: input.Version, ReasonCode: input.ReasonCode, CredentialPolicy: cp.IntegrationCredentialPolicy(policy), BindingVersion: input.BindingVersion}, nil
}

// validMaterialPublicVersion preserves exact positive OCC versions across JSON/browser consumers.
// validMaterialPublicVersion сохраняет exact positive OCC versions между JSON/browser consumers.
func validMaterialPublicVersion(version int64) bool {
	return version >= 1 && version <= 9007199254740991
}
