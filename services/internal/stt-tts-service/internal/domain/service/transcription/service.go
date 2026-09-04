// Package transcription реализует stateless-сценарий распознавания речи.
package transcription

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

const maximumTranscriptBytes = 256 << 10

type Service struct {
	policies    PolicyResolver
	credentials CredentialProjector
	provider    Provider
	observer    Observer
	now         func() time.Time
	requestTTL  time.Duration
}

type Input struct {
	Principal                value.Principal
	RequestID, CorrelationID string
	AudioReader              interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	}
	AudioSizeBytes int64
	MediaType      string
}

func New(policies PolicyResolver, credentials CredentialProjector, provider Provider, observer Observer, requestTTL time.Duration) (*Service, error) {
	if policies == nil || credentials == nil || provider == nil || observer == nil || requestTTL < time.Second || requestTTL > 20*time.Second {
		return nil, errors.New("transcription service configuration is invalid")
	}
	return &Service{policies: policies, credentials: credentials, provider: provider, observer: observer, now: time.Now, requestTTL: requestTTL}, nil
}

func (service *Service) Transcribe(ctx context.Context, input Input) (value.TranscriptionResult, error) {
	fail := func(stage value.Stage, class value.ErrorClass, err error) (value.TranscriptionResult, error) {
		service.observer.Observe(stage, class)
		return value.TranscriptionResult{}, err
	}
	if ctx == nil || input.RequestID == "" || input.CorrelationID == "" || input.Principal.RequestID != input.RequestID ||
		input.Principal.Permission != value.PermissionTranscribe || input.Principal.ActorID == "" ||
		input.Principal.TenantID == "" || input.Principal.ProjectID == "" || input.Principal.AuthorityRevision == 0 ||
		!validSHA256(input.Principal.AuthorityDigestSHA256) || !service.now().Before(input.Principal.ExpiresAt) {
		return fail(value.StageAuthority, value.ErrorDenied, errs.ErrPermissionDenied)
	}
	requestDeadline := service.now().Add(service.requestTTL)
	if input.Principal.ExpiresAt.Before(requestDeadline) {
		requestDeadline = input.Principal.ExpiresAt
	}
	requestCtx, cancelRequest := context.WithDeadline(ctx, requestDeadline)
	defer cancelRequest()
	policy, err := service.policies.Resolve(requestCtx, input.Principal, input.RequestID, input.CorrelationID)
	if err != nil {
		return fail(value.StagePolicy, projectionClass(err), errors.Join(errs.ErrPolicyUnavailable, err))
	}
	if err := validatePolicy(policy, service.now()); err != nil {
		return fail(value.StagePolicy, value.ErrorRejected, err)
	}
	policyCtx, cancelPolicy := context.WithDeadline(requestCtx, policy.ExpiresAt)
	defer cancelPolicy()
	audio, err := ValidateAudio(input.AudioReader, input.AudioSizeBytes, input.MediaType, policy.MaximumAudioBytes, policy.MaximumAudioDuration)
	if err != nil {
		return fail(value.StageAudio, value.ErrorInvalid, err)
	}
	credential, err := service.credentials.Project(policyCtx, input.Principal, input.RequestID, input.CorrelationID, policy)
	if err != nil {
		return fail(value.StageCredential, projectionClass(err), errors.Join(errs.ErrCredentialUnavailable, err))
	}
	defer clear(credential.APIKey)
	if err := validateCredential(credential, policy, service.now()); err != nil {
		return fail(value.StageCredential, value.ErrorRejected, err)
	}
	credentialCtx, cancelCredential := context.WithDeadline(policyCtx, credential.ExpiresAt)
	defer cancelCredential()
	providerCtx, cancelProvider := context.WithTimeout(credentialCtx, policy.ProviderTimeout)
	defer cancelProvider()
	transcript, err := service.provider.Transcribe(providerCtx, value.ProviderRequest{Audio: audio, Model: policy.Model, Language: policy.Language, APIKey: credential.APIKey})
	if err != nil {
		stage, class := value.StageProvider, value.ErrorUnavailable
		if errors.Is(err, errs.ErrEgressUnavailable) {
			stage = value.StageEgress
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(providerCtx.Err(), context.DeadlineExceeded) {
			class = value.ErrorTimeout
		} else if errors.Is(err, errs.ErrProviderRejected) {
			class = value.ErrorRejected
		}
		return fail(stage, class, err)
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" || len(transcript) > maximumTranscriptBytes {
		return fail(value.StageProvider, value.ErrorRejected, errs.ErrProviderUnavailable)
	}
	service.observer.Observe(value.StageSuccess, value.ErrorNone)
	return value.TranscriptionResult{Text: transcript, Receipt: value.TranscriptionReceipt{
		RequestID: input.RequestID, CorrelationID: input.CorrelationID,
		ActorID: input.Principal.ActorID, TenantID: input.Principal.TenantID, ProjectID: input.Principal.ProjectID,
		AuthoritySourceRevision:     input.Principal.AuthorityRevision,
		AuthoritySourceDigestSHA256: input.Principal.AuthorityDigestSHA256,
		ConfigRevision:              policy.Revision, ConfigDigestSHA256: policy.DigestSHA256,
		Model: policy.Model, Language: policy.Language, ProviderAccountRef: policy.ProviderAccountRef,
		ProviderCredentialGeneration: policy.ProviderCredentialGeneration, CompletedStage: value.StageSuccess,
	}}, nil
}

func (service *Service) CheckLocal(ctx context.Context) error {
	if ctx == nil {
		return errors.New("transcription local readiness context is required")
	}
	return service.provider.CheckLocal(ctx)
}

func (service *Service) CheckProtectedPath(context.Context) error {
	return errs.ErrDelegatedProofPending
}

func projectionClass(err error) value.ErrorClass {
	if errors.Is(err, errs.ErrGrantRevoked) || errors.Is(err, errs.ErrDelegatedProofPending) {
		return value.ErrorDenied
	}
	return value.ErrorUnavailable
}

func validatePolicy(policy value.Policy, now time.Time) error {
	if policy.Revision == 0 || !validSHA256(policy.DigestSHA256) || policy.Model != value.DefaultModel ||
		policy.Language != value.DefaultLanguage || policy.MaximumAudioBytes < 1024 || policy.MaximumAudioBytes > value.MaximumAbsoluteBytes ||
		policy.MaximumAudioDuration < time.Second || policy.MaximumAudioDuration > 30*time.Minute ||
		policy.ProviderTimeout < time.Second || policy.ProviderTimeout > 15*time.Second ||
		policy.ProviderAccountRef == "" || len(policy.ProviderAccountRef) > 96 || policy.ProviderCredentialGeneration == 0 || !now.Before(policy.ExpiresAt) {
		return errs.ErrGrantRevoked
	}
	return nil
}

func validateCredential(credential value.Credential, policy value.Policy, now time.Time) error {
	if len(credential.APIKey) < 8 || len(credential.APIKey) > 16<<10 || credential.ProviderAccountRef != policy.ProviderAccountRef ||
		credential.ProviderCredentialGeneration != policy.ProviderCredentialGeneration ||
		subtle.ConstantTimeCompare([]byte(credential.ConfigDigestSHA256), []byte(policy.DigestSHA256)) != 1 ||
		!now.Before(credential.ExpiresAt) || credential.ExpiresAt.After(policy.ExpiresAt) {
		return errs.ErrGrantRevoked
	}
	return nil
}

func validSHA256(input string) bool {
	if len(input) != 64 {
		return false
	}
	for _, character := range input {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
