// Package projection адаптирует generated RPC владельцев policy и credential
// к доменным портам STT.
package projection

import (
	"context"
	"errors"
	"time"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	credentialProjectionIncomplete = "transcription credential projection is incomplete"
	policyProjectionIncomplete     = "transcription policy projection is incomplete"
	minimumAudioBytes              = uint64(1024)
	maximumAudioBytes              = uint64(value.MaximumAbsoluteBytes)
	minimumAudioDurationMillis     = uint64(1000)
	maximumAudioDurationMillis     = uint64(1800000)
	minimumProviderTimeoutMillis   = uint64(1000)
	maximumProviderTimeoutMillis   = uint64(15000)
)

type DelegatedBinder interface {
	BindDelegated(context.Context, value.Principal, string, string, string, string) (context.Context, error)
}

type Policy struct {
	client sttv1.TranscriptionPolicyProjectionServiceClient
	binder DelegatedBinder
}

func NewPolicy(client sttv1.TranscriptionPolicyProjectionServiceClient, binder DelegatedBinder) (*Policy, error) {
	if client == nil || binder == nil {
		return nil, errors.New("transcription policy projection dependencies are required")
	}
	return &Policy{client: client, binder: binder}, nil
}

func (client *Policy) Resolve(ctx context.Context, principal value.Principal, requestID, correlationID string) (value.Policy, error) {
	bound, err := client.binder.BindDelegated(ctx, principal, requestID, correlationID,
		sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName, "platform.stt.policy.resolve")
	if err != nil {
		return value.Policy{}, err
	}
	locator := authorityLocator(principal, requestID, correlationID)
	response, err := client.client.ResolveTranscriptionPolicy(bound, &sttv1.ResolveTranscriptionPolicyRequest{Authority: locator})
	if err != nil {
		return value.Policy{}, classifyProjectionError(bound, err)
	}
	if response == nil || response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil ||
		!sameAuthority(response.GetAuthority(), locator) {
		return value.Policy{}, errors.New(policyProjectionIncomplete)
	}
	maximumBytes := response.GetMaximumAudioBytes()
	maximumDuration := response.GetMaximumAudioDurationMilliseconds()
	providerTimeout := response.GetProviderTimeoutMilliseconds()
	if maximumBytes < minimumAudioBytes || maximumBytes > maximumAudioBytes ||
		maximumDuration < minimumAudioDurationMillis || maximumDuration > maximumAudioDurationMillis ||
		providerTimeout < minimumProviderTimeoutMillis || providerTimeout > maximumProviderTimeoutMillis {
		return value.Policy{}, errors.New(policyProjectionIncomplete)
	}
	return value.Policy{
		Revision: response.GetConfigRevision(), DigestSHA256: response.GetConfigDigestSha256(),
		Model: response.GetModel(), Language: response.GetLanguage(), MaximumAudioBytes: int64(maximumBytes),
		MaximumAudioDuration:         time.Duration(maximumDuration) * time.Millisecond,
		ProviderTimeout:              time.Duration(providerTimeout) * time.Millisecond,
		ProviderAccountRef:           response.GetProviderAccountRef(),
		ProviderCredentialGeneration: response.GetProviderCredentialGeneration(),
		ExpiresAt:                    response.GetExpiresAt().AsTime().UTC(),
	}, nil
}

type Credential struct {
	client sttv1.TranscriptionCredentialProjectionServiceClient
	binder DelegatedBinder
}

func NewCredential(client sttv1.TranscriptionCredentialProjectionServiceClient, binder DelegatedBinder) (*Credential, error) {
	if client == nil || binder == nil {
		return nil, errors.New("transcription credential projection dependencies are required")
	}
	return &Credential{client: client, binder: binder}, nil
}

func (client *Credential) Project(ctx context.Context, principal value.Principal, requestID, correlationID string, policy value.Policy) (value.Credential, error) {
	bound, err := client.binder.BindDelegated(ctx, principal, requestID, correlationID,
		sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName, "platform.stt.credential.project")
	if err != nil {
		return value.Credential{}, err
	}
	locator := authorityLocator(principal, requestID, correlationID)
	response, err := client.client.ProjectTranscriptionCredential(bound, &sttv1.ProjectTranscriptionCredentialRequest{
		Authority: locator, ProviderAccountRef: policy.ProviderAccountRef,
		ProviderCredentialGeneration: policy.ProviderCredentialGeneration,
		ConfigRevision:               policy.Revision, ConfigDigestSha256: policy.DigestSHA256,
	})
	if err != nil {
		return value.Credential{}, classifyProjectionError(bound, err)
	}
	if response == nil {
		return value.Credential{}, errors.New(credentialProjectionIncomplete)
	}
	projectedKey := response.GetApiKey()
	defer clear(projectedKey)
	if response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil ||
		response.GetConfigRevision() != policy.Revision || !sameAuthority(response.GetAuthority(), locator) {
		return value.Credential{}, errors.New(credentialProjectionIncomplete)
	}
	return value.Credential{
		APIKey: append([]byte(nil), projectedKey...), ProviderAccountRef: response.GetProviderAccountRef(),
		ProviderCredentialGeneration: response.GetProviderCredentialGeneration(), ConfigDigestSHA256: response.GetConfigDigestSha256(),
		ExpiresAt: response.GetExpiresAt().AsTime().UTC(),
	}, nil
}

func authorityLocator(principal value.Principal, requestID, correlationID string) *sttv1.DelegatedAuthorityLocator {
	return &sttv1.DelegatedAuthorityLocator{
		RequestId: requestID, CorrelationId: correlationID, RootActorId: principal.ActorID,
		TenantId: principal.TenantID, ProjectId: principal.ProjectID,
		SourceRevision: principal.AuthorityRevision, SourceDigestSha256: principal.AuthorityDigestSHA256,
		Actor: provenance(principal.Actor), Tenant: provenance(principal.Tenant), Project: provenance(principal.Project),
		ExpiresAt: timestamppb.New(principal.ExpiresAt),
	}
}

func provenance(input value.AuthorityProvenance) *sttv1.AuthorityIdentityProvenance {
	return &sttv1.AuthorityIdentityProvenance{
		Source: input.Source, Reference: input.Reference,
		Revision: input.Revision, DigestSha256: input.DigestSHA256,
	}
}

func sameAuthority(actual, expected *sttv1.DelegatedAuthorityLocator) bool {
	if actual == nil || expected == nil || actual.GetExpiresAt() == nil || expected.GetExpiresAt() == nil {
		return false
	}
	return actual.GetRequestId() == expected.GetRequestId() && actual.GetCorrelationId() == expected.GetCorrelationId() &&
		actual.GetRootActorId() == expected.GetRootActorId() && actual.GetTenantId() == expected.GetTenantId() &&
		actual.GetProjectId() == expected.GetProjectId() && actual.GetSourceRevision() == expected.GetSourceRevision() &&
		actual.GetSourceDigestSha256() == expected.GetSourceDigestSha256() &&
		sameProvenance(actual.GetActor(), expected.GetActor()) && sameProvenance(actual.GetTenant(), expected.GetTenant()) &&
		sameProvenance(actual.GetProject(), expected.GetProject()) && actual.GetExpiresAt().AsTime().Equal(expected.GetExpiresAt().AsTime())
}

func sameProvenance(actual, expected *sttv1.AuthorityIdentityProvenance) bool {
	return actual != nil && expected != nil && actual.GetSource() == expected.GetSource() &&
		actual.GetReference() == expected.GetReference() && actual.GetRevision() == expected.GetRevision() &&
		actual.GetDigestSha256() == expected.GetDigestSha256()
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
