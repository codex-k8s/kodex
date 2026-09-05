package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type RuntimeSecretDraftPrepareInput struct {
	ImpactPlanRef                                                                              string
	SelectedItemRefs                                                                           []string
	Kind, DraftRef, SecretRef, ProjectRef, Name, Description, ValueType, ExpectedContentSHA256 string
	ExpectedSecretVersion                                                                      int64
	Mutation                                                                                   value.Mutation
}

type RuntimeSecretDraftWorkInput struct {
	Action, OperationRef, OperationGrant, ClaimantID, FailureCode string
	ClaimGeneration                                               int64
	Encrypted                                                     *entity.RuntimeSecretDraftEncryptedDescriptor
	Materialization                                               *entity.RuntimeSecretMaterialization
}
