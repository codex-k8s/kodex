package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const providerCatalogOperation = "platform.provider-accounts.model-catalog.observe"

const providerCatalogDiagnosticMessage = "provider model catalog observation rejected"

func WithCatalogLogger(logger *slog.Logger) Option {
	return func(server *Server) { server.catalogLogger = logger }
}

func (server *Server) logCatalogFailure(ctx context.Context, result providercredential.ModelCatalog) {
	if server.catalogLogger == nil {
		return
	}
	switch result.Failure {
	case providercredential.CatalogFailureUnverified, providercredential.CatalogFailureUnavailable,
		providercredential.CatalogFailureAuthorization:
		server.catalogLogger.WarnContext(ctx, providerCatalogDiagnosticMessage,
			"stage", result.DiagnosticStage(), "failure", string(result.Failure))
	}
}

func (server *Server) ObserveProviderModelCatalog(ctx context.Context, request *cp.ObserveProviderModelCatalogRequest) (*cp.ObserveProviderModelCatalogResponse, error) {
	_, verified, err := verifiedProjectionAuthority(ctx, "control-plane", "spiffe://kodex.local/ns/kodex-system/sa/control-plane",
		cp.ProviderCredentialMaterializerService_ObserveProviderModelCatalog_FullMethodName, providerCatalogOperation)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || proto.Size(request) > 16<<10 {
		return nil, status.Error(codes.InvalidArgument, "provider catalog request is invalid")
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	digest := sha256.Sum256(raw)
	if err != nil || verified.GetAuthorityAbiVersion() != internalrpcauth.AuthorityABIVersion || verified.GetRequestBindingMode() != internalrpcauth.RequestBindingUnary || verified.GetContinuation() != nil || verified.GetRequestDigestSha256() != hex.EncodeToString(digest[:]) {
		return nil, status.Error(codes.PermissionDenied, "provider catalog request binding is invalid")
	}
	if !validCatalogRequest(request) {
		return nil, status.Error(codes.InvalidArgument, "provider catalog task binding is invalid")
	}
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, "provider catalog observer is unavailable")
	}
	deadline := minCatalogDeadline(time.Now().Add(15*time.Second), verified.GetExpiresAt().AsTime(), request.GetExpiresAt().AsTime())
	if !deadline.After(time.Now()) {
		return nil, status.Error(codes.DeadlineExceeded, "provider catalog task expired")
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	method := providercredential.CatalogMethodDeviceCode
	if request.GetAuthorizationMethod() == cp.ProviderAuthorizationMethod_PROVIDER_AUTHORIZATION_METHOD_API_KEY {
		method = providercredential.CatalogMethodAPIKey
	}
	descriptor := request.GetCredential()
	result, err := server.providerCredentials.ObserveModelCatalog(ctx, request.GetAccountRef(), kubernetesstore.ProviderCredentialDescriptor{
		SecretName: descriptor.GetSecretName(), SecretUID: descriptor.GetSecretUid(), SecretResourceVersion: descriptor.GetSecretResourceVersion(), ContentSHA256: descriptor.GetContentSha256(),
	}, method)
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "provider catalog observation failed")
	}
	response := &cp.ObserveProviderModelCatalogResponse{AccountRef: request.GetAccountRef(), CredentialRevisionRef: request.GetCredentialRevisionRef(), ObservedAt: timestamppb.New(result.ObservedAt)}
	if response.ObservedAt.CheckValid() != nil || result.ObservedAt.IsZero() || result.ObservedAt.After(time.Now()) {
		return nil, status.Error(codes.Unavailable, "provider catalog observation is invalid")
	}
	switch result.Failure {
	case providercredential.CatalogFailureNone:
		response.Failure = cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_NONE
		if result.Source == providercredential.CatalogRemoteAPI {
			response.Source = cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_API
		} else if result.Source == providercredential.CatalogRemoteCodex {
			response.Source = cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_CODEX
		} else {
			return nil, status.Error(codes.Unavailable, "provider catalog source is invalid")
		}
		for _, model := range result.Models {
			response.Models = append(response.Models, &cp.ProviderModelCatalogRecord{Id: model.ID, DefaultReasoningEffort: model.DefaultReasoningEffort, ReasoningEfforts: append([]string(nil), model.ReasoningEfforts...)})
		}
	case providercredential.CatalogFailureUnavailable:
		response.Failure = cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNAVAILABLE
	case providercredential.CatalogFailureUnverified:
		response.Failure = cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNVERIFIED_SOURCE
	case providercredential.CatalogFailureAuthorization:
		response.Failure = cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_AUTHORIZATION_REJECTED
	default:
		return nil, status.Error(codes.Unavailable, "provider catalog outcome is invalid")
	}
	if proto.Size(response) > 128<<10 {
		return nil, status.Error(codes.Unavailable, "provider catalog response exceeded its bound")
	}
	server.logCatalogFailure(ctx, result)
	return response, nil
}

func validCatalogRequest(request *cp.ObserveProviderModelCatalogRequest) bool {
	for _, ref := range []string{request.GetTaskRef(), request.GetAccountRef(), request.GetCredentialRevisionRef(), request.GetClaimantId(), request.GetFence()} {
		if !validCorrelation(ref) {
			return false
		}
	}
	for _, version := range []int64{request.GetClaimGeneration(), request.GetAccountVersion(), request.GetCredentialRevision()} {
		if version < 1 || uint64(version) > maximumAuthorityRevision {
			return false
		}
	}
	descriptor := request.GetCredential()
	return descriptor != nil && len(descriptor.ProtoReflect().GetUnknown()) == 0 && descriptor.GetSecretName() != "" && descriptor.GetSecretUid() != "" && descriptor.GetSecretResourceVersion() != "" && validProjectionSHA256(descriptor.GetContentSha256()) &&
		request.GetExpiresAt() != nil && request.GetExpiresAt().CheckValid() == nil &&
		(request.GetAuthorizationMethod() == cp.ProviderAuthorizationMethod_PROVIDER_AUTHORIZATION_METHOD_API_KEY || request.GetAuthorizationMethod() == cp.ProviderAuthorizationMethod_PROVIDER_AUTHORIZATION_METHOD_DEVICE_CODE)
}

func minCatalogDeadline(values ...time.Time) time.Time {
	result := values[0]
	for _, value := range values[1:] {
		if value.Before(result) {
			result = value
		}
	}
	return result
}
