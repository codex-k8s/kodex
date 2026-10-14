package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

func renewalTask(binding *cp.ProviderCredentialRenewalBinding, digest string) (repository.ProviderCredentialRenewalTask, error) {
	if binding == nil || len(binding.ProtoReflect().GetUnknown()) != 0 || proto.Size(binding) > 16<<10 ||
		binding.GetPreviousCredential() == nil || len(binding.GetPreviousCredential().ProtoReflect().GetUnknown()) != 0 ||
		binding.GetExpiresAt() == nil || len(binding.GetExpiresAt().ProtoReflect().GetUnknown()) != 0 || binding.GetExpiresAt().CheckValid() != nil {
		return repository.ProviderCredentialRenewalTask{}, errs.ErrInvalid
	}
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&cp.RenewManagedProviderCredentialRequest{Binding: binding})
	actual := sha256.Sum256(raw)
	if err != nil || digest != hex.EncodeToString(actual[:]) {
		return repository.ProviderCredentialRenewalTask{}, errs.ErrInvalid
	}
	previous := binding.GetPreviousCredential()
	return repository.ProviderCredentialRenewalTask{Ref: binding.GetTaskRef(), AccountRef: binding.GetAccountRef(),
		AccountVersion: binding.GetAccountVersion(), CredentialRef: binding.GetCredentialRevisionRef(), CredentialRevision: binding.GetCredentialRevision(),
		ClaimantID: binding.GetClaimantId(), Fence: binding.GetFence(), Generation: binding.GetClaimGeneration(), ExpiresAt: binding.GetExpiresAt().AsTime(), RequestDigest: digest,
		Previous: entity.ProviderCredentialDescriptor{SecretName: previous.GetSecretName(), SecretUID: previous.GetSecretUid(), SecretResourceVersion: previous.GetSecretResourceVersion(), ContentSHA256: previous.GetContentSha256()}}, nil
}

func (server *Server) CheckProviderCredentialRenewalWorkReadiness(ctx context.Context, _ *cp.CheckProviderCredentialRenewalWorkReadinessRequest) (*cp.CheckProviderCredentialRenewalWorkReadinessResponse, error) {
	if _, err := principal(ctx, cp.ProviderCredentialRenewalWorkService_CheckProviderCredentialRenewalWorkReadiness_FullMethodName); err != nil {
		return nil, err
	}
	if err := server.service.CheckProviderCredentialRenewalReadiness(ctx); err != nil {
		return nil, transportError(err)
	}
	return &cp.CheckProviderCredentialRenewalWorkReadinessResponse{Ready: true}, nil
}

func (server *Server) AuthorizeProviderCredentialRenewal(ctx context.Context, request *cp.AuthorizeProviderCredentialRenewalRequest) (*cp.AuthorizeProviderCredentialRenewalResponse, error) {
	p, err := principal(ctx, cp.ProviderCredentialRenewalWorkService_AuthorizeProviderCredentialRenewal_FullMethodName)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, transportError(errs.ErrInvalid)
	}
	task, err := renewalTask(request.GetBinding(), request.GetRenewalRequestSha256())
	if err != nil {
		return nil, transportError(err)
	}
	if err := server.service.AuthorizeProviderCredentialRenewal(ctx, p, task); err != nil {
		return nil, transportError(err)
	}
	return &cp.AuthorizeProviderCredentialRenewalResponse{Authorized: true}, nil
}

func (server *Server) ValidateProviderCredentialRenewalObservation(ctx context.Context, request *cp.ValidateProviderCredentialRenewalObservationRequest) (*cp.ValidateProviderCredentialRenewalObservationResponse, error) {
	p, err := principal(ctx, cp.ProviderCredentialRenewalWorkService_ValidateProviderCredentialRenewalObservation_FullMethodName)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, transportError(errs.ErrInvalid)
	}
	task, err := renewalTask(request.GetBinding(), request.GetRenewalRequestSha256())
	if err != nil {
		return nil, transportError(err)
	}
	if err := server.service.ValidateProviderCredentialRenewalObservation(ctx, p, task); err != nil {
		return nil, transportError(err)
	}
	return &cp.ValidateProviderCredentialRenewalObservationResponse{Eligible: true}, nil
}
