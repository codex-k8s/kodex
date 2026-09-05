package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

const ProviderModelCatalogOperation = "platform.provider-accounts.model-catalog.observe"

type ProviderModelCatalogTask struct {
	Ref, OrganizationID, AccountRef, ProviderDefinitionKey string
	AccountVersion                                         int64
	CredentialRef                                          string
	CredentialRevision                                     int64
	Credential                                             entity.ProviderCredentialDescriptor
	AuthorizationMethod                                    string
	ClaimantID, Fence, RequestDigest                       string
	ClaimGeneration                                        int64
	ExpiresAt                                              time.Time
}

type ProviderModelCatalogRecord struct {
	ID                     string   `json:"id"`
	DefaultReasoningEffort string   `json:"defaultReasoningEffort"`
	ReasoningEfforts       []string `json:"reasoningEfforts"`
}

type ProviderModelCatalogObservation struct {
	AccountRef, CredentialRef, Source, Failure string
	ObservedAt                                 time.Time
	Models                                     []ProviderModelCatalogRecord
}

// Encoder принадлежит composition root и не принимает browser input.
// Owner вызывает его с task, собранной из locked authoritative rows, до commit.
type ProviderModelCatalogEncoder interface {
	ModelCatalogRequestDigest(ProviderModelCatalogTask) (string, error)
}

type ProviderModelCatalogRepository interface {
	ClaimProviderModelCatalogTasks(context.Context, string, int32, ProviderModelCatalogEncoder) ([]ProviderModelCatalogTask, error)
	CompleteProviderModelCatalogTask(context.Context, ProviderModelCatalogTask, ProviderModelCatalogObservation) error
}
