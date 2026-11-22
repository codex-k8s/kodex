package command

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// KubernetesIdentityHealthCompletion contains only a bounded private authentication fact.
// Neither username/groups nor provider responses and resource permissions are accepted here.
// KubernetesIdentityHealthCompletion содержит только bounded private authentication fact.
// Username/groups, provider responses и resource permissions здесь не принимаются.
type KubernetesIdentityHealthCompletion struct {
	AttemptRef, SnapshotDigest, ObservedSubjectDigest string
	Outcome                                           entity.PersonalIntegrationCredentialTestOutcome
}
