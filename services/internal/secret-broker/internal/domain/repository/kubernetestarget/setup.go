package kubernetestarget

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

var (
	// ErrSetupRejected refuses invalid private input or mismatched owner admission without details.
	// ErrSetupRejected отклоняет неверный private input либо owner admission без подробностей.
	ErrSetupRejected = errors.New("managed Kubernetes identity setup rejected")
	// ErrSetupUnavailable preserves missing completion as closed unreported evidence, never UNKNOWN authority.
	// ErrSetupUnavailable сохраняет missing completion как закрытый unreported evidence без UNKNOWN authority.
	ErrSetupUnavailable = errors.New("managed Kubernetes identity setup unavailable")
)

// IdentityUploadClaim supplies only exact immutable owner pins and the fixed broker instance.
// IdentityUploadClaim содержит лишь exact immutable owner pins и fixed broker instance.
type IdentityUploadClaim struct {
	SetupRef, PermitSHA256, SnapshotDigest, ContentSHA256, WorkloadInstance string
}

// IdentityUploadCompletion contains exactly one typed local outcome after dependency work joins.
// Missing delivery remains owner-closed; a lease timeout cannot reclassify it as a lost ACK.
// IdentityUploadCompletion содержит ровно один typed local outcome после dependency join.
// Missing delivery остаётся owner-closed; lease timeout не превращает его в lost ACK.
type IdentityUploadCompletion struct {
	Admission value.KubernetesIdentityUploadAdmission
	Instance  string
	Identity  *value.KubernetesIdentityLocator
	Unknown   bool
	Rejected  *value.KubernetesIdentityAcknowledgementRejection
}

// IdentitySetupOwner is the protected owner boundary and supplies no local credential selection.
// IdentitySetupOwner — protected owner boundary без собственного credential selection.
type IdentitySetupOwner interface {
	// ResolveKubernetesIdentityUpload reads current immutable metadata; it grants no storage credit.
	// ResolveKubernetesIdentityUpload читает current immutable metadata без storage credit.
	ResolveKubernetesIdentityUpload(context.Context, string, string) (value.KubernetesIdentityUploadSnapshot, error)
	// ClaimKubernetesIdentityUpload consumes one exact WRITE or qualified OBSERVE admission.
	// ClaimKubernetesIdentityUpload расходует один exact WRITE либо qualified OBSERVE admission.
	ClaimKubernetesIdentityUpload(context.Context, IdentityUploadClaim) (value.KubernetesIdentityUploadAdmission, error)
	// CompleteKubernetesIdentityUpload retains the exact original fact even after caller cancellation.
	// CompleteKubernetesIdentityUpload сохраняет exact original fact даже после caller cancellation.
	CompleteKubernetesIdentityUpload(context.Context, IdentityUploadCompletion) (value.KubernetesIdentitySetupView, error)
}

// IdentityCredentialVerifier parses actual private bytes and verifies the pinned cluster with that identity.
// IdentityCredentialVerifier разбирает actual private bytes и проверяет pinned cluster той же identity.
type IdentityCredentialVerifier interface {
	// VerifyKubernetesIdentity never invokes plugins, ambient config or an elevated fallback identity.
	// VerifyKubernetesIdentity не вызывает plugins, ambient config либо elevated fallback identity.
	VerifyKubernetesIdentity(context.Context, []byte, value.KubernetesTarget) error
}
