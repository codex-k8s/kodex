package controlplane

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func (owner *Owner) AuthorizeProviderCredentialRenewal(ctx context.Context, binding *cp.ProviderCredentialRenewalBinding, digest string) error {
	response, err := owner.client.ProviderRenewals.AuthorizeProviderCredentialRenewal(ctx, &cp.AuthorizeProviderCredentialRenewalRequest{Binding: binding, RenewalRequestSha256: digest})
	if err != nil {
		return err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !response.GetAuthorized() {
		return errors.New("provider renewal owner authorization is unavailable")
	}
	return nil
}

func (owner *Owner) ValidateProviderCredentialRenewalObservation(ctx context.Context, binding *cp.ProviderCredentialRenewalBinding, digest string) error {
	response, err := owner.client.ProviderRenewals.ValidateProviderCredentialRenewalObservation(ctx, &cp.ValidateProviderCredentialRenewalObservationRequest{Binding: binding, RenewalRequestSha256: digest})
	if err != nil {
		return err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !response.GetEligible() {
		return errors.New("provider renewal observation authority is unavailable")
	}
	return nil
}
