package httptransport

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	stdhttp "net/http"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
)

var errProviderWebhookVerifierUnavailable = errors.New("provider webhook verifier is not configured")

type rejectingProviderWebhookVerifier struct{}

func (rejectingProviderWebhookVerifier) VerifyProviderWebhook(context.Context, *stdhttp.Request, ProviderWebhookVerificationInput) error {
	return errProviderWebhookVerifierUnavailable
}

// GitHubProviderWebhookVerifier verifies GitHub webhook HMAC signatures.
type GitHubProviderWebhookVerifier struct {
	resolver  secretresolver.Resolver
	secretRef secretresolver.SecretRef
}

// NewGitHubProviderWebhookVerifier creates a GitHub verifier from a value-safe secret reference.
func NewGitHubProviderWebhookVerifier(resolver secretresolver.Resolver, secretRef secretresolver.SecretRef) *GitHubProviderWebhookVerifier {
	return &GitHubProviderWebhookVerifier{resolver: resolver, secretRef: secretRef}
}

// VerifyProviderWebhook checks source binding and X-Hub-Signature-256.
func (v *GitHubProviderWebhookVerifier) VerifyProviderWebhook(ctx context.Context, req *stdhttp.Request, input ProviderWebhookVerificationInput) error {
	if strings.ToLower(strings.TrimSpace(input.ProviderSlug)) != "github" {
		return NewSafeError(stdhttp.StatusBadRequest, CodeSourceNotAllowed, "provider webhook source is not allowed", false)
	}
	if v == nil || v.resolver == nil {
		return WrapSafeError(stdhttp.StatusServiceUnavailable, CodeDownstreamUnavailable, "provider webhook verifier is unavailable", true, errProviderWebhookVerifierUnavailable)
	}
	signature := strings.TrimSpace(req.Header.Get("X-Hub-Signature-256"))
	if signature == "" {
		return NewSafeError(stdhttp.StatusUnauthorized, CodeSignatureInvalid, "provider webhook signature is invalid", false)
	}
	providedMAC, err := parseGitHubSignature(signature)
	if err != nil {
		return WrapSafeError(stdhttp.StatusUnauthorized, CodeSignatureInvalid, "provider webhook signature is invalid", false, err)
	}
	secret, err := v.resolver.Resolve(ctx, v.secretRef)
	if err != nil {
		return verifierSecretError(err)
	}
	defer secret.Clear()
	secretBytes := secret.Bytes()
	defer clearBytes(secretBytes)
	if len(secretBytes) == 0 {
		return WrapSafeError(stdhttp.StatusServiceUnavailable, CodeDownstreamUnavailable, "provider webhook verifier is unavailable", false, secretresolver.ErrSecretNotFound)
	}
	expectedMAC := githubWebhookMAC(secretBytes, input.Payload)
	if !hmac.Equal(providedMAC, expectedMAC) {
		return NewSafeError(stdhttp.StatusUnauthorized, CodeSignatureInvalid, "provider webhook signature is invalid", false)
	}
	return nil
}

func parseGitHubSignature(signature string) ([]byte, error) {
	algorithm, encodedMAC, ok := strings.Cut(signature, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(algorithm), "sha256") {
		return nil, errors.New("invalid github signature algorithm")
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(encodedMAC))
	if err != nil {
		return nil, errors.New("invalid github signature digest")
	}
	if len(decoded) != sha256.Size {
		return nil, errors.New("invalid github signature length")
	}
	return decoded, nil
}

func githubWebhookMAC(secret []byte, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func clearBytes(raw []byte) {
	for i := range raw {
		raw[i] = 0
	}
}

func verifierSecretError(err error) *SafeError {
	retryable := true
	if errors.Is(err, secretresolver.ErrInvalidRef) ||
		errors.Is(err, secretresolver.ErrUnsupportedStoreType) ||
		errors.Is(err, secretresolver.ErrSecretNotFound) {
		retryable = false
	}
	return WrapSafeError(stdhttp.StatusServiceUnavailable, CodeDownstreamUnavailable, "provider webhook verifier is unavailable", retryable, err)
}

func providerWebhookVerificationError(err error) *SafeError {
	var safeErr *SafeError
	if errors.As(err, &safeErr) {
		return safeErr
	}
	return WrapSafeError(stdhttp.StatusUnauthorized, CodeSignatureInvalid, "provider webhook signature is invalid", false, err)
}
