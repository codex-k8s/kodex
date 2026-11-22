package kubernetestarget

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ErrHealthUnavailable prevents failed health or uncertain owner completion from releasing a result.
// ErrHealthUnavailable не выдаёт result при failed health либо uncertain owner completion.
var ErrHealthUnavailable = errors.New("managed Kubernetes health unavailable")

// ErrHealthAuthenticationRejected records a proven selected-identity authentication denial only.
// ErrHealthAuthenticationRejected обозначает лишь доказанный selected-identity authentication denial.
var ErrHealthAuthenticationRejected = errors.New("managed Kubernetes health authentication rejected")

// IdentityHealthAdmission contains private pins from one already-consumed original test claim.
// IdentityHealthAdmission содержит private pins одного уже израсходованного original test claim.
type IdentityHealthAdmission struct {
	AttemptRef, SnapshotDigest string
	Target                     value.KubernetesTarget
	Identity                   value.KubernetesIdentityLocator
	ExpiresAt                  time.Time
}

// IdentityHealthCompletion carries only a finite fact; no provider body or broad RBAC assertion is valid.
// IdentityHealthCompletion содержит только finite fact; provider body и broad RBAC assertion недопустимы.
type IdentityHealthCompletion struct {
	Binding                managedkubernetes.Binding
	Admission              IdentityHealthAdmission
	Outcome, SubjectDigest string
}

// IdentityHealthReceipt describes owner application, not independent permission to return stale data.
// IdentityHealthReceipt описывает owner application, не отдельное разрешение выдавать stale data.
type IdentityHealthReceipt struct {
	TestRef, State string
	Applied        bool
}

// IdentityHealthOwner owns exact claim admission and final current-binding verification.
// IdentityHealthOwner владеет exact claim admission и final current-binding verification.
type IdentityHealthOwner interface {
	// BeginKubernetesIdentityHealth consumes one original test claim before material is selected.
	// BeginKubernetesIdentityHealth расходует original test claim до выбора material.
	BeginKubernetesIdentityHealth(context.Context, managedkubernetes.Binding) (IdentityHealthAdmission, error)
	// CompleteKubernetesIdentityHealth records one exact outcome without retrying the probe.
	// CompleteKubernetesIdentityHealth сохраняет exact outcome без повторного probe.
	CompleteKubernetesIdentityHealth(context.Context, IdentityHealthCompletion) (IdentityHealthReceipt, error)
}

// IdentityHealthVerifier parses private bytes and observes actual cluster and subject with that identity.
// IdentityHealthVerifier разбирает private bytes и наблюдает actual cluster/subject той же identity.
type IdentityHealthVerifier interface {
	// ObserveKubernetesIdentity must never replace the selected identity with platform or service credentials.
	// ObserveKubernetesIdentity не подменяет selected identity platform либо service credentials.
	ObserveKubernetesIdentity(context.Context, []byte, value.KubernetesTarget) (string, error)
}
