// Package sshidentity executes only the existing owner's purpose-specific identity setup grants; it has no credential selection, retry scheduling or persistent authority of its own.
// Package sshidentity выполняет только purpose-specific identity setup grants существующего owner; собственного credential selection, retry scheduling либо persistent authority у него нет.
package sshidentity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// Service composes owner admission, actual key parsing and the private purpose store; an absent completion never authorizes observation, and only the owner can persist an explicit UNKNOWN receipt as observation-eligible.
// Service связывает owner admission, actual key parsing и private purpose store; отсутствующий completion не разрешает observation, и только owner сохраняет explicit UNKNOWN receipt как observation-eligible.
type Service struct {
	owner               sshrepository.IdentitySetupOwner
	keys                sshrepository.IdentityKeyPreparer
	store               sshrepository.IdentityMaterialStore
	instance, namespace string
	now                 func() time.Time
}

// New requires trusted composition ports and a fixed workload identity/namespace; it neither probes dependencies nor exposes a runnable public API or a claim of completed setup readiness.
// New требует trusted composition ports и fixed workload identity/namespace; dependency probes, runnable public API и утверждение completed setup readiness он не предоставляет.
func New(owner sshrepository.IdentitySetupOwner, keys sshrepository.IdentityKeyPreparer, store sshrepository.IdentityMaterialStore, instance, namespace string) (*Service, error) {
	if owner == nil || keys == nil || store == nil || !managedssh.OwnerReference(instance) || namespace == "" {
		return nil, sshrepository.ErrIdentityRejected
	}
	return &Service{owner: owner, keys: keys, store: store, instance: instance, namespace: namespace, now: time.Now}, nil
}

// Materialize snapshots bounded private input before owner I/O, parses the actual key and spends one exact WRITE admission before one storage request; a successful return acknowledges evidence delivery, not current attachment. Neither a denied/lost claim nor a completion failure can resend the create.
// Materialize фиксирует bounded private input до owner I/O, разбирает actual key и расходует один exact WRITE admission до одного storage request; successful return подтверждает evidence delivery, а не current attachment. Denied/lost claim и completion failure не повторяют create.
func (service *Service) Materialize(ctx context.Context, setupRef string, permit, material []byte) error {
	if ctx == nil || ctx.Err() != nil || len(material) < 1 || len(material) > 16384 {
		return sshrepository.ErrIdentityRejected
	}
	private := bytes.Clone(material)
	defer clear(private)
	snapshot, permitDigest, err := service.resolve(ctx, setupRef, permit, value.SSHIdentityUploadWrite)
	if err != nil {
		return err
	}
	if uint32(len(private)) != snapshot.ContentBytes {
		return sshrepository.ErrIdentityRejected
	}
	key, err := service.keys.PrepareSSHIdentity(private)
	actual := sha256.Sum256(private)
	if err != nil || key.ContentSHA256 != hex.EncodeToString(actual[:]) || key.ContentSHA256 != snapshot.Preparation.ContentSHA256 || !managedssh.Digest(key.AuthenticationSHA256) {
		return sshrepository.ErrIdentityRejected
	}
	admission, err := service.claim(ctx, snapshot, permitDigest, key)
	if err != nil {
		return err
	}
	operation, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	identity, writeErr := service.store.CreateSSHIdentity(operation, admission.Provision, private)
	cancel()
	completion := sshrepository.IdentityUploadCompletion{Admission: admission}
	var rejection *sshrepository.IdentityAcknowledgementRejection
	switch {
	case errors.As(writeErr, &rejection):
		witness := *rejection
		completion.Rejected = &witness
	case errors.Is(writeErr, sshrepository.ErrIdentityUnknown):
		completion.Unknown = true
	case writeErr != nil:
		return sshrepository.ErrIdentityUnavailable
	case !identity.Valid() || identity.Provision() != admission.Provision:
		// A store that violates its typed receipt contract grants no observation credit.
		// Store с нарушенным typed receipt contract не выдаёт observation credit.
		return sshrepository.ErrIdentityUnavailable
	default:
		completion.Identity = &identity
	}
	return service.complete(ctx, completion)
}

// Observe accepts only a fresh owner-issued observation of an explicitly recorded lost acknowledgement; missing WRITE completion, expiry or ACK_REJECTED cannot be promoted into read/rebinding credit here.
// Observe принимает только fresh owner-issued observation явно recorded lost acknowledgement; missing WRITE completion, expiry и ACK_REJECTED не превращаются здесь в read/rebinding credit.
func (service *Service) Observe(ctx context.Context, setupRef string, permit []byte) error {
	snapshot, permitDigest, err := service.resolve(ctx, setupRef, permit, value.SSHIdentityUploadObserve)
	if err != nil {
		return err
	}
	key := value.SSHIdentityKeyMetadata{ContentSHA256: snapshot.Preparation.ContentSHA256, AuthenticationSHA256: snapshot.AuthenticationSHA256}
	admission, err := service.claim(ctx, snapshot, permitDigest, key)
	if err != nil {
		return err
	}
	operation, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	identity, readErr := service.store.ObserveSSHIdentity(operation, admission.Provision)
	cancel()
	completion := sshrepository.IdentityUploadCompletion{Admission: admission}
	var rejection *sshrepository.IdentityAcknowledgementRejection
	if errors.As(readErr, &rejection) {
		witness := *rejection
		completion.Rejected = &witness
	} else if errors.Is(readErr, sshrepository.ErrIdentityAbsent) || errors.Is(readErr, sshrepository.ErrIdentityUnavailable) || errors.Is(readErr, sshrepository.ErrIdentityUnknown) {
		completion.Unknown = true
	} else if readErr != nil {
		return sshrepository.ErrIdentityUnavailable
	} else if !identity.Valid() || identity.Provision() != admission.Provision {
		return sshrepository.ErrIdentityUnavailable
	} else {
		completion.Identity = &identity
	}
	return service.complete(ctx, completion)
}

// resolve hashes the opaque one-shot upload permit before the private owner RPC, retaining no permit in claims, receipts or diagnostics and never treating its presence as storage permission.
// resolve хеширует opaque one-shot upload permit до private owner RPC, не сохраняя permit в claims, receipts либо diagnostics и не считая его наличие storage permission.
func (service *Service) resolve(ctx context.Context, setupRef string, permit []byte, mode value.SSHIdentityUploadMode) (value.SSHIdentityUploadSnapshot, string, error) {
	if ctx == nil || ctx.Err() != nil || !managedssh.OwnerReference(setupRef) || len(permit) != 32 {
		return value.SSHIdentityUploadSnapshot{}, "", sshrepository.ErrIdentityRejected
	}
	digest := sha256.Sum256(permit)
	permitDigest := hex.EncodeToString(digest[:])
	snapshot, err := service.owner.ResolveSSHIdentityUpload(ctx, setupRef, permitDigest)
	if err != nil {
		return value.SSHIdentityUploadSnapshot{}, "", sshrepository.ErrIdentityUnavailable
	}
	if snapshot.SetupRef != setupRef || snapshot.Mode != mode || !snapshot.Valid(service.now()) || snapshot.Preparation.Namespace != service.namespace || ctx.Err() != nil {
		return value.SSHIdentityUploadSnapshot{}, "", sshrepository.ErrIdentityRejected
	}
	return snapshot, permitDigest, nil
}

// claim verifies the exact immutable owner response and remaining deadline before granting the purpose adapter storage credit; no malformed or lost response is retried or reported as an external effect.
// claim проверяет exact immutable owner response и remaining deadline до выдачи purpose adapter storage credit; malformed либо lost response не повторяется и не объявляется external effect.
func (service *Service) claim(ctx context.Context, snapshot value.SSHIdentityUploadSnapshot, permitDigest string, key value.SSHIdentityKeyMetadata) (value.SSHIdentityUploadAdmission, error) {
	admission, err := service.owner.ClaimSSHIdentityUpload(ctx, sshrepository.IdentityUploadClaim{SetupRef: snapshot.SetupRef, PermitSHA256: permitDigest, SnapshotDigest: snapshot.Digest, WorkloadInstance: service.instance, Key: key})
	if err != nil {
		return value.SSHIdentityUploadAdmission{}, sshrepository.ErrIdentityUnavailable
	}
	if !admission.ValidFor(snapshot, key, service.instance, service.now()) || ctx.Err() != nil {
		return value.SSHIdentityUploadAdmission{}, sshrepository.ErrIdentityRejected
	}
	return admission, nil
}

// complete makes exactly one independently bounded evidence delivery after local work joins, including caller cancellation; an unreported result stays owner-closed and never becomes observation-eligible merely because its lease expires.
// complete выполняет ровно одну независимо bounded evidence delivery после local work join, включая caller cancellation; unreported result остаётся owner-closed и не становится observation-eligible лишь из-за lease expiry.
func (service *Service) complete(ctx context.Context, completion sshrepository.IdentityUploadCompletion) error {
	if !completion.Valid() {
		return sshrepository.ErrIdentityUnavailable
	}
	evidence, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := service.owner.CompleteSSHIdentityUpload(evidence, completion); err != nil {
		return sshrepository.ErrIdentityUnavailable
	}
	if completion.Rejected != nil {
		return sshrepository.ErrIdentityAcknowledgementRejected
	}
	if completion.Unknown {
		return sshrepository.ErrIdentityUnknown
	}
	return nil
}
