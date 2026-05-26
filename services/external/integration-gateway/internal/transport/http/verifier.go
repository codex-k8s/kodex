package httptransport

import (
	"context"
	"errors"
	stdhttp "net/http"
)

var errProviderWebhookVerifierUnavailable = errors.New("provider webhook verifier is not configured")

type rejectingProviderWebhookVerifier struct{}

func (rejectingProviderWebhookVerifier) VerifyProviderWebhook(context.Context, *stdhttp.Request, ProviderWebhookVerificationInput) error {
	return errProviderWebhookVerifierUnavailable
}
