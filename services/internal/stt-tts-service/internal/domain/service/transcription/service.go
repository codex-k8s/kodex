// Package transcription реализует stateless-сценарий распознавания речи.
package transcription

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	transcriptionrepo "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/repository/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

const maximumTranscriptBytes = 256 << 10

type Service struct {
	policies    transcriptionrepo.PolicyResolver
	credentials transcriptionrepo.CredentialProjector
	provider    transcriptionrepo.Provider
	now         func() time.Time
	requestTTL  time.Duration
}

type Input struct {
	Principal value.Principal
	RequestID string
	Audio     []byte
	MediaType string
}

func New(
	policies transcriptionrepo.PolicyResolver,
	credentials transcriptionrepo.CredentialProjector,
	provider transcriptionrepo.Provider,
	requestTTL time.Duration,
) (*Service, error) {
	if policies == nil || credentials == nil || provider == nil || requestTTL < time.Second || requestTTL > time.Minute {
		return nil, errors.New("transcription service configuration is invalid")
	}
	return &Service{policies: policies, credentials: credentials, provider: provider, now: time.Now, requestTTL: requestTTL}, nil
}

func (service *Service) Transcribe(ctx context.Context, input Input) (string, error) {
	if ctx == nil || input.RequestID == "" || input.Principal.Permission != value.PermissionTranscribe ||
		input.Principal.ActorID == "" || input.Principal.TenantID == "" || input.Principal.ProjectID == "" ||
		input.Principal.AuthorityRevision == 0 || !validSHA256(input.Principal.AuthorityDigestSHA256) ||
		!service.now().Before(input.Principal.ExpiresAt) {
		return "", errs.ErrPermissionDenied
	}
	requestDeadline := service.now().Add(service.requestTTL)
	if input.Principal.ExpiresAt.Before(requestDeadline) {
		requestDeadline = input.Principal.ExpiresAt
	}
	requestCtx, cancelRequest := context.WithDeadline(ctx, requestDeadline)
	defer cancelRequest()

	policy, err := service.policies.Resolve(requestCtx, input.Principal, input.RequestID)
	if err != nil {
		return "", errors.Join(errs.ErrPolicyUnavailable, err)
	}
	if err := validatePolicy(policy, service.now()); err != nil {
		return "", err
	}
	policyCtx, cancelPolicy := context.WithDeadline(requestCtx, policy.ExpiresAt)
	defer cancelPolicy()
	audio, err := ValidateAudio(input.Audio, input.MediaType, policy.MaximumAudioBytes, policy.MaximumAudioDuration)
	if err != nil {
		return "", err
	}
	defer clear(audio.Bytes)
	credential, err := service.credentials.Project(policyCtx, input.Principal, input.RequestID, policy)
	if err != nil {
		return "", errors.Join(errs.ErrCredentialUnavailable, err)
	}
	defer clear(credential.APIKey)
	if err := validateCredential(credential, policy, service.now()); err != nil {
		return "", err
	}
	credentialCtx, cancelCredential := context.WithDeadline(policyCtx, credential.ExpiresAt)
	defer cancelCredential()
	providerCtx, cancelProvider := context.WithTimeout(credentialCtx, policy.ProviderTimeout)
	defer cancelProvider()
	transcript, err := service.provider.Transcribe(providerCtx, value.ProviderRequest{
		Audio: audio, Model: policy.Model, Language: policy.Language, APIKey: credential.APIKey,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "", errors.Join(errs.ErrProviderUnavailable, err)
		}
		return "", err
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" || len(transcript) > maximumTranscriptBytes {
		return "", errs.ErrProviderUnavailable
	}
	return transcript, nil
}

func (service *Service) Check(ctx context.Context) error {
	if ctx == nil {
		return errors.New("transcription readiness context is required")
	}
	return errors.Join(service.policies.Check(ctx), service.credentials.Check(ctx), service.provider.Check(ctx))
}

func validatePolicy(policy value.Policy, now time.Time) error {
	if policy.Revision == 0 || !validSHA256(policy.DigestSHA256) ||
		policy.Model != value.DefaultModel || policy.Language != value.DefaultLanguage ||
		policy.MaximumAudioBytes < 1024 || policy.MaximumAudioBytes > value.MaximumAbsoluteBytes ||
		policy.MaximumAudioDuration < time.Second || policy.MaximumAudioDuration > 30*time.Minute ||
		policy.ProviderTimeout < time.Second || policy.ProviderTimeout > 45*time.Second ||
		policy.ProviderAccountRef == "" || len(policy.ProviderAccountRef) > 96 || policy.ProviderCredentialGeneration == 0 ||
		policy.CredentialProjectionGrant == "" || len(policy.CredentialProjectionGrant) > 16<<10 ||
		strings.ContainsAny(policy.CredentialProjectionGrant, "\r\n") || !now.Before(policy.ExpiresAt) {
		return errs.ErrGrantRevoked
	}
	return nil
}

func validateCredential(credential value.Credential, policy value.Policy, now time.Time) error {
	if len(credential.APIKey) < 8 || len(credential.APIKey) > 16<<10 ||
		credential.ProviderAccountRef != policy.ProviderAccountRef ||
		credential.ProviderCredentialGeneration != policy.ProviderCredentialGeneration ||
		subtle.ConstantTimeCompare([]byte(credential.ConfigDigestSHA256), []byte(policy.DigestSHA256)) != 1 ||
		!now.Before(credential.ExpiresAt) || credential.ExpiresAt.After(policy.ExpiresAt) {
		return errs.ErrGrantRevoked
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
