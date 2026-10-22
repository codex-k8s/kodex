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

// ProviderRenewalOwner gates write renewal and read observation separately against exact owner
// task/binding digests; a trusted transport selector alone cannot authorize seed access.
// ProviderRenewalOwner раздельно разрешает write renewal и read observation по exact owner task/binding
// digests; одного trusted transport selector недостаточно для seed access.
type ProviderRenewalOwner interface {
	// AuthorizeProviderCredentialRenewal confirms the exact fresh owner-bound renewal before the materializer
	// may read the seed or perform an external refresh.
	// AuthorizeProviderCredentialRenewal подтверждает exact fresh owner-bound renewal до чтения seed и
	// внешнего refresh materializer.
	AuthorizeProviderCredentialRenewal(context.Context, *cp.ProviderCredentialRenewalBinding, string) error
	// ValidateProviderCredentialRenewalObservation rechecks read eligibility of the exact immutable
	// task/digest without allowing another credential refresh.
	// ValidateProviderCredentialRenewalObservation повторно проверяет read eligibility exact immutable
	// task/digest без разрешения нового credential refresh.
	ValidateProviderCredentialRenewalObservation(context.Context, *cp.ProviderCredentialRenewalBinding, string) error
}

// managedCredentialRenewer is the optional materializer capability separating one renewal effect from
// observation of its durable outcome.
// managedCredentialRenewer — optional materializer capability, разделяющая renewal effect и observation
// его durable outcome.
type managedCredentialRenewer interface {
	// RenewManagedCredential performs the authorized exact renewal and may report an unknown effect outcome
	// that callers must not blindly replay.
	// RenewManagedCredential выполняет разрешённый exact renewal и может сообщить unknown effect outcome,
	// который caller не должен слепо повторять.
	RenewManagedCredential(context.Context, kstore.ProviderRenewalIdentity) (providercredential.ManagedRenewalResult, error)
	// ObserveManagedRenewal reads the exact renewal receipt/outcome without executing another provider
	// refresh.
	// ObserveManagedRenewal читает exact renewal receipt/outcome без выполнения нового provider refresh.
	ObserveManagedRenewal(context.Context, kstore.ProviderRenewalIdentity) (providercredential.ManagedRenewalResult, error)
}

// WithProviderRenewalOwner installs the optional owner port during trusted composition without validating
// readiness or issuing renewal authority.
// WithProviderRenewalOwner устанавливает optional owner port при доверенной composition без readiness
// validation и выдачи renewal authority.
func WithProviderRenewalOwner(owner ProviderRenewalOwner) Option {
	return func(server *Server) { server.providerRenewalOwner = owner }
}

// providerRenewalIdentity validates known/bounded binding messages, references, versions and timestamp
// shape, then hashes the normalized Renew request and copies prior pins. The same identity digest is used
// by observation; actual prior-source eligibility and clock freshness are enforced downstream.
// providerRenewalIdentity проверяет known/bounded binding messages, references, versions и timestamp
// shape, затем хеширует normalized Renew request и копирует prior pins. Observation использует тот же
// identity digest; actual prior-source eligibility и clock freshness проверяются ниже.
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

// providerRenewalAuthority checks bounded request shape and exact trusted admission or unary
// ABI/digest-bound verified proof. Trusted admission yields a forty-second ceiling, not owner permission;
// the concrete request digest is distinct from the normalized task-identity digest.
// providerRenewalAuthority проверяет bounded request shape и exact trusted admission либо unary
// ABI/digest-bound verified proof. Trusted admission задаёт сорокасекундный ceiling, не owner permission;
// concrete request digest отличается от normalized task-identity digest.
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

// RenewManagedProviderCredential validates transport/binding, chooses the earliest
// proof/task/thirty-five-second deadline and calls owner authorization before the optional renewer.
// Unknown effects return UNKNOWN_OUTCOME without success pins; other failures remain fixed errors and no
// automatic retry occurs here.
// RenewManagedProviderCredential проверяет transport/binding, выбирает earliest
// proof/task/тридцатипятисекундный deadline и вызывает owner authorization до optional renewer. Unknown
// effects дают UNKNOWN_OUTCOME без success pins; прочие failures остаются fixed errors, automatic retry
// здесь нет.
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

// ObserveManagedProviderCredentialRenewal validates current read authority, bounds observation by
// proof/ten seconds rather than the old immutable task expiry, then gates receipt reading through the
// owner. It returns success pins or UNKNOWN_OUTCOME without executing another renewal.
// ObserveManagedProviderCredentialRenewal проверяет current read authority, ограничивает observation
// proof/десятью секундами, не старым immutable task expiry, затем запрашивает owner перед чтением receipt.
// Возвращает success pins либо UNKNOWN_OUTCOME без нового renewal.
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
