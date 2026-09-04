// Package projection адаптирует generated RPC владельцев policy и credential
// к доменным портам STT.
package projection

import (
	"context"
	"errors"
	"time"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/protectedrpc"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const credentialProjectionIncomplete = "transcription credential projection is incomplete"

type Policy struct {
	client sttv1.TranscriptionPolicyProjectionServiceClient
}

func NewPolicy(client sttv1.TranscriptionPolicyProjectionServiceClient) (*Policy, error) {
	if client == nil {
		return nil, errors.New("transcription policy projection client is required")
	}
	return &Policy{client: client}, nil
}

func (client *Policy) Resolve(ctx context.Context, principal value.Principal, requestID string) (value.Policy, error) {
	ctx, err := protectedrpc.WithProjectReference(ctx, principal.ProjectID)
	if err != nil {
		return value.Policy{}, err
	}
	response, err := client.client.ResolveTranscriptionPolicy(ctx, &sttv1.ResolveTranscriptionPolicyRequest{
		ProjectId: principal.ProjectID, RequestId: requestID, ActorId: principal.ActorID, TenantId: principal.TenantID,
		AuthorityRevision: principal.AuthorityRevision, AuthorityDigestSha256: principal.AuthorityDigestSHA256,
	})
	if err != nil {
		return value.Policy{}, classifyProjectionError(ctx, err)
	}
	if response == nil || response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil || response.GetRequestId() != requestID ||
		response.GetActorId() != principal.ActorID || response.GetTenantId() != principal.TenantID ||
		response.GetProjectId() != principal.ProjectID || response.GetAuthorityRevision() != principal.AuthorityRevision ||
		response.GetAuthorityDigestSha256() != principal.AuthorityDigestSHA256 {
		return value.Policy{}, errors.New("transcription policy projection is incomplete")
	}
	return value.Policy{
		Revision: response.GetConfigRevision(), DigestSHA256: response.GetConfigDigestSha256(),
		Model: response.GetModel(), Language: response.GetLanguage(), MaximumAudioBytes: int(response.GetMaximumAudioBytes()),
		MaximumAudioDuration: time.Duration(response.GetMaximumAudioDurationMilliseconds()) * time.Millisecond,
		ProviderTimeout:      time.Duration(response.GetProviderTimeoutMilliseconds()) * time.Millisecond,
		ProviderAccountRef:   response.GetProviderAccountRef(), ProviderCredentialGeneration: response.GetProviderCredentialGeneration(),
		CredentialProjectionGrant: response.GetCredentialProjectionGrant(), ExpiresAt: response.GetExpiresAt().AsTime(),
	}, nil
}

func (client *Policy) Check(ctx context.Context) error {
	response, err := client.client.CheckReadiness(ctx, &sttv1.TranscriptionPolicyProjectionServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("transcription policy projection is not ready")
	}
	return nil
}

type Credential struct {
	client sttv1.TranscriptionCredentialProjectionServiceClient
}

func NewCredential(client sttv1.TranscriptionCredentialProjectionServiceClient) (*Credential, error) {
	if client == nil {
		return nil, errors.New("transcription credential projection client is required")
	}
	return &Credential{client: client}, nil
}

func (client *Credential) Project(ctx context.Context, principal value.Principal, requestID string, policy value.Policy) (value.Credential, error) {
	ctx, err := protectedrpc.WithProjectReference(ctx, principal.ProjectID)
	if err != nil {
		return value.Credential{}, err
	}
	response, err := client.client.ProjectTranscriptionCredential(ctx, &sttv1.ProjectTranscriptionCredentialRequest{
		ProjectId: principal.ProjectID, RequestId: requestID, ProviderAccountRef: policy.ProviderAccountRef,
		ProviderCredentialGeneration: policy.ProviderCredentialGeneration, ConfigRevision: policy.Revision,
		ConfigDigestSha256: policy.DigestSHA256, CredentialProjectionGrant: policy.CredentialProjectionGrant,
		ActorId: principal.ActorID, TenantId: principal.TenantID, AuthorityRevision: principal.AuthorityRevision,
		AuthorityDigestSha256: principal.AuthorityDigestSHA256,
	})
	if err != nil {
		return value.Credential{}, classifyProjectionError(ctx, err)
	}
	if response == nil {
		return value.Credential{}, errors.New(credentialProjectionIncomplete)
	}
	projectedKey := response.GetApiKey()
	defer clear(projectedKey)
	if response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil || response.GetRequestId() != requestID ||
		response.GetActorId() != principal.ActorID || response.GetTenantId() != principal.TenantID ||
		response.GetProjectId() != principal.ProjectID || response.GetConfigRevision() != policy.Revision ||
		response.GetAuthorityRevision() != principal.AuthorityRevision ||
		response.GetAuthorityDigestSha256() != principal.AuthorityDigestSHA256 {
		return value.Credential{}, errors.New(credentialProjectionIncomplete)
	}
	apiKey := append([]byte(nil), projectedKey...)
	return value.Credential{
		APIKey: apiKey, ProviderAccountRef: response.GetProviderAccountRef(),
		ProviderCredentialGeneration: response.GetProviderCredentialGeneration(), ConfigDigestSHA256: response.GetConfigDigestSha256(),
		ExpiresAt: response.GetExpiresAt().AsTime(),
	}, nil
}

func (client *Credential) Check(ctx context.Context) error {
	response, err := client.client.CheckReadiness(ctx, &sttv1.TranscriptionCredentialProjectionServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("transcription credential projection is not ready")
	}
	return nil
}

func classifyProjectionError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition, codes.NotFound:
		return errs.ErrGrantRevoked
	default:
		return errors.New("transcription projection request failed")
	}
}
