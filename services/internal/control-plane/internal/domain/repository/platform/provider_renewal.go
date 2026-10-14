package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const (
	ProviderCredentialRenewalOperation          = "platform.provider-accounts.credential.renew"
	ProviderCredentialRenewalObserveOperation   = "platform.provider-accounts.credential.renewal.observe"
	ProviderCredentialRenewalAuthorizeOperation = "platform.provider-accounts.credential.renewal.authorize"
	ProviderCredentialRenewalValidateOperation  = "platform.provider-accounts.credential.renewal.validate"
)

// Pins назначает owner под account lock; Org не передаётся внешним caller.
type ProviderCredentialRenewalTask struct {
	Ref, OrganizationID, AccountRef, CredentialRef, ClaimantID, Fence string
	AccountVersion, CredentialRevision, Generation                    int64
	Previous                                                          entity.ProviderCredentialDescriptor
	ExpiresAt                                                         time.Time
	State, RequestDigest, OwnerRequestDigest                          string
}

type ProviderCredentialRenewalResult struct {
	// Назначает READ client adapter, не поле RPC/payload.
	Recovered                         bool `json:"-"`
	State, ReceiptUID, ReceiptVersion string
	Credential                        *entity.ProviderCredentialDescriptor
}

type ProviderCredentialRenewalEncoder interface {
	CredentialRenewalRequestDigests(ProviderCredentialRenewalTask) (string, string, error)
}

type ProviderCredentialRenewalRepository interface {
	ClaimProviderCredentialRenewalTasks(context.Context, string, ProviderCredentialRenewalEncoder) ([]ProviderCredentialRenewalTask, error)
	ListProviderCredentialRenewalRecoveryTasks(context.Context) ([]ProviderCredentialRenewalTask, error)
	AuthorizeProviderCredentialRenewal(context.Context, value.Principal, ProviderCredentialRenewalTask) error
	ValidateProviderCredentialRenewalObservation(context.Context, value.Principal, ProviderCredentialRenewalTask) error
	CompleteProviderCredentialRenewalTask(context.Context, ProviderCredentialRenewalTask, ProviderCredentialRenewalResult) error
	CheckProviderCredentialRenewalReadiness(context.Context) error
}
