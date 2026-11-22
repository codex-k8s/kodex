package entity

import (
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesIdentityHealthAdmission grants one selected-identity READ to the private broker.
// Its absent subject pin is intentional: the first SelfSubjectReview establishes that observation.
// KubernetesIdentityHealthAdmission разрешает один selected-identity READ private broker.
// Subject pin намеренно отсутствует: первый SelfSubjectReview устанавливает это observation.
type KubernetesIdentityHealthAdmission struct {
	AttemptRef, SnapshotDigest string
	Target                     value.ManagedKubernetesTarget
	Identity                   value.ManagedKubernetesIdentityLocator
	ExpiresAt                  time.Time
}

// KubernetesIdentityHealthReceipt reports whether the exact current binding accepted a fact.
// Late evidence remains recorded without making a revoked or replaced identity usable.
// KubernetesIdentityHealthReceipt сообщает принятие fact точной current binding.
// Late evidence сохраняется без восстановления usable отозванной либо заменённой identity.
type KubernetesIdentityHealthReceipt struct {
	TestRef, State string
	Applied        bool
}
