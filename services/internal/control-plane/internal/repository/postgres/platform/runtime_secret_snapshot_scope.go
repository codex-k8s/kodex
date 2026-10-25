package platform

import (
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// validSecretDraftResultScope requires a valid returned draft scope and exact scope kind,
// organization/project, draft/secret refs and generation matching the current draft. An optional
// returned Secret must match the current secret ref and scope; nil is accepted. It checks result
// identity only, not draft state, secret content, grant provenance, authority or storage
// completion.
// validSecretDraftResultScope требует допустимый scope возвращённого draft и точное совпадение
// scope kind, organization/project, draft/secret refs и generation с текущим draft. Необязательный
// возвращённый Secret должен совпадать с current secret ref и scope; nil допустим. Проверяет
// только identity результата, а не draft state, secret content, grant provenance, полномочия или
// завершение storage.
func validSecretDraftResultScope(current entity.RuntimeSecretDraft, result entity.RuntimeSecretDraftResult) bool {
	draft := result.Draft
	if runtimesecret.ValidateScope(runtimesecret.ScopeKind(draft.ScopeKind), draft.OrganizationRef, draft.ProjectRef) != nil || draft.ScopeKind != current.ScopeKind || draft.OrganizationRef != current.OrganizationRef || draft.ProjectRef != current.ProjectRef || draft.Ref != current.Ref || draft.SecretRef != current.SecretRef || draft.Generation != current.Generation {
		return false
	}
	secret := result.Secret
	return secret == nil || secret.Ref == current.SecretRef && secret.ScopeKind == current.ScopeKind && secret.OrganizationRef == current.OrganizationRef && secret.ProjectRef == current.ProjectRef
}
