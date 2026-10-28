package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// RuntimeSecretDraftPrepareInput describes a scoped draft operation with exact secret/version/content preconditions, mutation metadata and optional impact-plan selection. It carries metadata, not plaintext secret bytes or caller-issued materialization authority.
// RuntimeSecretDraftPrepareInput описывает операцию черновика в своей области с точными предусловиями секрета, версии и содержимого, метаданными изменения и необязательным выбором плана влияния. Передаются метаданные, а не открытые байты секрета или выданные вызывающим полномочия материализации.
type RuntimeSecretDraftPrepareInput struct {
	Purpose                                                                                    string `json:",omitempty"`
	ScopeKind                                                                                  string
	ImpactPlanRef                                                                              string
	SelectedItemRefs                                                                           []string
	Kind, DraftRef, SecretRef, ProjectRef, Name, Description, ValueType, ExpectedContentSHA256 string
	ExpectedSecretVersion                                                                      int64
	Mutation                                                                                   value.Mutation
}

// RuntimeSecretDraftWorkInput binds worker action, operation grant and claimant generation to optional encrypted-draft or materialization descriptors and a failure code. These references remain subject to owner checks; they are not plaintext secret transport.
// RuntimeSecretDraftWorkInput связывает действие исполнителя, полномочие операции и поколение захвата с необязательными дескрипторами зашифрованного черновика или материализации и кодом ошибки. Ссылки подлежат проверкам владельца и не являются транспортом открытого секрета.
type RuntimeSecretDraftWorkInput struct {
	Action, OperationRef, OperationGrant, ClaimantID, FailureCode string
	ClaimGeneration                                               int64
	Encrypted                                                     *entity.RuntimeSecretDraftEncryptedDescriptor
	Materialization                                               *entity.RuntimeSecretMaterialization
}
