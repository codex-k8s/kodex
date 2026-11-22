package sshtarget

import (
	"context"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// IdentityKeyPreparer parses private bytes only within broker custody and returns actual integrity metadata; the implementation must redact parser errors and never persist or retain its input.
// IdentityKeyPreparer разбирает private bytes только внутри broker custody и возвращает actual integrity metadata; implementation обязана скрыть parser errors и не сохранять input.
type IdentityKeyPreparer interface {
	// PrepareSSHIdentity validates the supported key and derives both hashes before any owner Claim or external write.
	// PrepareSSHIdentity проверяет supported key и выводит оба hashes до owner Claim либо external write.
	PrepareSSHIdentity([]byte) (value.SSHIdentityKeyMetadata, error)
}

// IdentityUploadClaim addresses the immutable snapshot and actual parsed key; authenticated owner state still resolves every principal, policy, target and binding generation.
// IdentityUploadClaim адресует immutable snapshot и actual parsed key; authenticated owner state разрешает все principal, policy, target и binding generation.
type IdentityUploadClaim struct {
	SetupRef, PermitSHA256, SnapshotDigest, WorkloadInstance string
	Key                                                      value.SSHIdentityKeyMetadata
}

// IdentityUploadCompletion is an exact oneof material pins, ambiguous storage outcome or terminal rejected acknowledgement; absence never means that an in-flight create did not happen.
// IdentityUploadCompletion содержит exact oneof material pins, ambiguous storage outcome либо terminal rejected acknowledgement; absence не означает, что in-flight create не произошёл.
type IdentityUploadCompletion struct {
	Admission value.SSHIdentityUploadAdmission `json:"-"`
	Identity  *value.SSHIdentityLocator        `json:"-"`
	Unknown   bool
	Rejected  *IdentityAcknowledgementRejection `json:"-"`
}

// Valid checks receipt cardinality without granting permission to attach; the setup owner must recheck current generation, expiry and all reference/dependency guards atomically before commit.
// Valid проверяет receipt cardinality без permission на attachment; setup owner обязан атомарно перепроверить current generation, expiry и все reference/dependency guards до commit.
func (completion IdentityUploadCompletion) Valid() bool {
	count := 0
	if completion.Identity != nil {
		if !completion.Identity.Valid() || completion.Identity.Provision() != completion.Admission.Provision {
			return false
		}
		count++
	}
	if completion.Unknown {
		count++
	}
	if completion.Rejected != nil {
		if !managedssh.Digest(completion.Rejected.ResponseSHA256) || len(completion.Rejected.ObservedUID) > 256 || !utf8.ValidString(completion.Rejected.ObservedUID) || (completion.Admission.Mode != value.SSHIdentityUploadWrite && completion.Admission.Mode != value.SSHIdentityUploadObserve) {
			return false
		}
		count++
	}
	return count == 1
}

// String conceals private locators, response witnesses and the fenced admission from completion diagnostics.
// String скрывает private locators, response witnesses и fenced admission из completion diagnostics.
func (IdentityUploadCompletion) String() string { return "[SSH private identity upload completion]" }

// GoString preserves the same privacy boundary for detailed Go formatting.
// GoString сохраняет ту же privacy boundary при подробном Go formatting.
func (completion IdentityUploadCompletion) GoString() string { return completion.String() }

// IdentitySetupOwner is the existing control-plane owner's purpose-specific setup port, not a second credential selector or retry engine; private key and raw permit bytes never cross it.
// IdentitySetupOwner — purpose-specific setup port существующего control-plane owner, а не второй credential selector либо retry engine; private key и raw permit bytes не пересекают его.
type IdentitySetupOwner interface {
	// ResolveSSHIdentityUpload reads fresh setup state without admitting storage or recreating a consumed permit.
	// ResolveSSHIdentityUpload читает fresh setup state без допуска storage либо повторного создания consumed permit.
	ResolveSSHIdentityUpload(context.Context, string, string) (value.SSHIdentityUploadSnapshot, error)
	// ClaimSSHIdentityUpload durably chooses the sole WRITE or a new observation generation for the immutable original intent; rejected ACKs never receive another grant.
	// ClaimSSHIdentityUpload устойчиво выбирает sole WRITE либо новое observation generation immutable original intent; rejected ACK не получает другого grant.
	ClaimSSHIdentityUpload(context.Context, IdentityUploadClaim) (value.SSHIdentityUploadAdmission, error)
	// CompleteSSHIdentityUpload retains late/unknown/rejected evidence even after grant closure; only fresh current authorization may atomically attach known material.
	// CompleteSSHIdentityUpload сохраняет late/unknown/rejected evidence даже после grant closure; только fresh current authorization может атомарно присоединить known material.
	CompleteSSHIdentityUpload(context.Context, IdentityUploadCompletion) error
}
