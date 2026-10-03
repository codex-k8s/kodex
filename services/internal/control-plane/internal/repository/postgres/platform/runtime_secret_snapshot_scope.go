package platform

import (
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func validSecretDraftResultScope(current entity.RuntimeSecretDraft, result entity.RuntimeSecretDraftResult) bool {
	draft := result.Draft
	if runtimesecret.ValidateScope(runtimesecret.ScopeKind(draft.ScopeKind), draft.OrganizationRef, draft.ProjectRef) != nil || draft.ScopeKind != current.ScopeKind || draft.OrganizationRef != current.OrganizationRef || draft.ProjectRef != current.ProjectRef || draft.Ref != current.Ref || draft.SecretRef != current.SecretRef || draft.Generation != current.Generation {
		return false
	}
	secret := result.Secret
	return secret == nil || secret.Ref == current.SecretRef && secret.ScopeKind == current.ScopeKind && secret.OrganizationRef == current.OrganizationRef && secret.ProjectRef == current.ProjectRef
}
