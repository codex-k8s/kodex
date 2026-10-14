package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	kstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type ProviderRenewalOwner interface {
	AuthorizeProviderCredentialRenewal(context.Context, *cp.ProviderCredentialRenewalBinding, string) error
	ValidateProviderCredentialRenewalObservation(context.Context, *cp.ProviderCredentialRenewalBinding, string) error
}

type managedCredentialRenewer interface {
	RenewManagedCredential(context.Context, kstore.ProviderRenewalIdentity) (providercredential.ManagedRenewalResult, error)
	ObserveManagedRenewal(context.Context, kstore.ProviderRenewalIdentity) (providercredential.ManagedRenewalResult, error)
}

func WithProviderRenewalOwner(owner ProviderRenewalOwner) Option {
	return func(server *Server) { server.providerRenewalOwner = owner }
}

func providerRenewalIdentity(binding *cp.ProviderCredentialRenewalBinding) (kstore.ProviderRenewalIdentity, error) {
	if binding == nil || len(binding.ProtoReflect().GetUnknown()) != 0 || proto.Size(binding) > 16<<10 ||
		binding.GetPreviousCredential() == nil || len(binding.GetPreviousCredential().ProtoReflect().GetUnknown()) != 0 ||
		binding.GetExpiresAt() == nil || binding.GetExpiresAt().CheckValid() != nil || len(binding.GetExpiresAt().ProtoReflect().GetUnknown()) != 0 {
		return kstore.ProviderRenewalIdentity{}, status.Error(codes.InvalidArgument, "provider renewal binding is invalid")
	}
	for _, ref := range []string{binding.GetTaskRef(), binding.GetAccountRef(), binding.GetCredentialRevisionRef(), binding.GetClaimantId(), binding.GetFence()} {
		if !validCorrelation(ref) {
			return kstore.ProviderRenewalIdentity{}, status.Error(codes.InvalidArgument, "provider renewal reference is invalid")
		}
	}
	for _, version := range []int64{binding.GetAccountVersion(), binding.GetCredentialRevision(), binding.GetClaimGeneration()} {
		if version < 1 || uint64(version) > maximumAuthorityRevision {
			return kstore.ProviderRenewalIdentity{}, status.Error(codes.InvalidArgument, "provider renewal version is invalid")
		}
	}
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&cp.RenewManagedProviderCredentialRequest{Binding: binding})
	digest := sha256.Sum256(raw)
	if err != nil {
		return kstore.ProviderRenewalIdentity{}, status.Error(codes.InvalidArgument, "provider renewal input is invalid")
	}
	previous := binding.GetPreviousCredential()
	return kstore.ProviderRenewalIdentity{TaskRef: binding.GetTaskRef(), AccountRef: binding.GetAccountRef(), CredentialRef: binding.GetCredentialRevisionRef(),
		AccountVersion: binding.GetAccountVersion(), CredentialRevision: binding.GetCredentialRevision(), ClaimantID: binding.GetClaimantId(), Generation: binding.GetClaimGeneration(),
		Fence: binding.GetFence(), ExpiresAt: binding.GetExpiresAt().AsTime(), RequestDigest: hex.EncodeToString(digest[:]),
		Previous: kstore.ProviderCredentialDescriptor{SecretName: previous.GetSecretName(), SecretUID: previous.GetSecretUid(), SecretResourceVersion: previous.GetSecretResourceVersion(), ContentSHA256: previous.GetContentSha256()}}, nil
}

func providerRenewalAuthority(ctx context.Context, request proto.Message, method, operation string) (time.Time, error) {
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || proto.Size(request) > 16<<10 {
		return time.Time{}, status.Error(codes.InvalidArgument, "provider renewal request is invalid")
	}
	if admission, ok := serviceidentity.FromContext(ctx); ok && admission.RPCProfile == transportprofile.TrustedCluster {
		if admission.Peer.SPIFFEID != "spiffe://kodex.local/ns/kodex-system/sa/control-plane" || admission.TargetSPIFFEID != secretBrokerSPIFFEID ||
			admission.FullMethod != method || admission.OperationID != operation || admission.Permission != operation || admission.ActorMode != serviceidentity.ServiceActor || admission.ProjectRequired {
			return time.Time{}, status.Error(codes.PermissionDenied, "trusted provider renewal admission rejected")
		}
		return time.Now().Add(40 * time.Second), nil
	}
	_, verified, err := verifiedProjectionAuthority(ctx, "control-plane", "spiffe://kodex.local/ns/kodex-system/sa/control-plane", method, operation)
	if err != nil {
		return time.Time{}, err
	}
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	digest := sha256.Sum256(raw)
	if err != nil || verified.GetAuthorityAbiVersion() != internalrpcauth.AuthorityABIVersion || verified.GetRequestBindingMode() != internalrpcauth.RequestBindingUnary ||
		verified.GetContinuation() != nil || verified.GetRequestDigestSha256() != hex.EncodeToString(digest[:]) {
		return time.Time{}, status.Error(codes.PermissionDenied, "provider renewal request binding is invalid")
	}
	return verified.GetExpiresAt().AsTime(), nil
}

func (server *Server) RenewManagedProviderCredential(ctx context.Context, request *cp.RenewManagedProviderCredentialRequest) (*cp.RenewManagedProviderCredentialResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "provider renewal request is invalid")
	}
	expires, err := providerRenewalAuthority(ctx, request, cp.ProviderCredentialMaterializerService_RenewManagedProviderCredential_FullMethodName, "platform.provider-accounts.credential.renew")
	if err != nil {
		return nil, err
	}
	identity, err := providerRenewalIdentity(request.GetBinding())
	if err != nil {
		return nil, err
	}
	renewer, ok := server.providerCredentials.(managedCredentialRenewer)
	if !ok || server.providerRenewalOwner == nil {
		return nil, status.Error(codes.Unavailable, "provider renewal is unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, minCatalogDeadline(expires, identity.ExpiresAt, time.Now().Add(35*time.Second)))
	defer cancel()
	// Даже trusted profile повторно потребляет exact свежую owner task до seed read.
	if err := server.providerRenewalOwner.AuthorizeProviderCredentialRenewal(ctx, request.GetBinding(), identity.RequestDigest); err != nil {
		return nil, status.Error(codes.PermissionDenied, "provider renewal owner authorization rejected")
	}
	result, err := renewer.RenewManagedCredential(ctx, identity)
	if err != nil {
		if providercredential.ManagedRenewalOutcomeUnknown(err) {
			return &cp.RenewManagedProviderCredentialResponse{State: cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_UNKNOWN_OUTCOME}, nil
		}
		return nil, status.Error(codes.Unavailable, "provider credential renewal failed")
	}
	return &cp.RenewManagedProviderCredentialResponse{State: cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_SUCCEEDED,
		Credential: providerCredentialDescriptor(result.Credential), MaterializerReceiptUid: result.ReceiptUID, MaterializerReceiptResourceVersion: result.ReceiptVersion}, nil
}

func (server *Server) ObserveManagedProviderCredentialRenewal(ctx context.Context, request *cp.ObserveManagedProviderCredentialRenewalRequest) (*cp.ObserveManagedProviderCredentialRenewalResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "provider renewal observation request is invalid")
	}
	expires, err := providerRenewalAuthority(ctx, request, cp.ProviderCredentialMaterializerService_ObserveManagedProviderCredentialRenewal_FullMethodName, "platform.provider-accounts.credential.renewal.observe")
	if err != nil {
		return nil, err
	}
	identity, err := providerRenewalIdentity(request.GetBinding())
	if err != nil {
		return nil, err
	}
	renewer, ok := server.providerCredentials.(managedCredentialRenewer)
	if !ok || server.providerRenewalOwner == nil {
		return nil, status.Error(codes.Unavailable, "provider renewal observation is unavailable")
	}
	// Immutable старый task expiry не назначает новый срок READ recovery.
	ctx, cancel := context.WithDeadline(ctx, minCatalogDeadline(expires, time.Now().Add(10*time.Second)))
	defer cancel()
	if err := server.providerRenewalOwner.ValidateProviderCredentialRenewalObservation(ctx, request.GetBinding(), identity.RequestDigest); err != nil {
		return nil, status.Error(codes.PermissionDenied, "provider renewal observation authority rejected")
	}
	result, err := renewer.ObserveManagedRenewal(ctx, identity)
	if err != nil {
		if providercredential.ManagedRenewalOutcomeUnknown(err) {
			return &cp.ObserveManagedProviderCredentialRenewalResponse{State: cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_UNKNOWN_OUTCOME}, nil
		}
		return nil, status.Error(codes.Unavailable, "provider renewal observation failed")
	}
	return &cp.ObserveManagedProviderCredentialRenewalResponse{State: cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_SUCCEEDED,
		Credential: providerCredentialDescriptor(result.Credential), MaterializerReceiptUid: result.ReceiptUID, MaterializerReceiptResourceVersion: result.ReceiptVersion}, nil
}
