package controlplane

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// AuthorizeProviderCredentialRenewal forwards binding/request digest to the configured renewal owner and
// requires a known-field authorized response; nil/unknown/denied replies fail closed without retry.
// AuthorizeProviderCredentialRenewal передаёт binding/request digest настроенному renewal owner и требует
// known-field authorized response; nil/unknown/denied ответы закрыто отклоняет без retry.
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

// ValidateProviderCredentialRenewalObservation forwards the exact observation binding/digest and requires
// an eligible response without unknown fields; it does not issue renewal credentials or mutate policy.
// ValidateProviderCredentialRenewalObservation передаёт exact observation binding/digest и требует
// eligible response без unknown fields; renewal credentials не выдаёт и policy не меняет.
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
