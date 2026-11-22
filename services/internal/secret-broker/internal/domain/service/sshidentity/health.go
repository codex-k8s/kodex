package sshidentity

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"time"
)

// HealthService owns a bounded synchronous health call, never a Claim loop, generic materializer or resource execution engine.
// HealthService владеет bounded synchronous health call без Claim loop, generic materializer либо resource execution engine.
type HealthService struct {
	owner  sshrepository.IdentityHealthOwner
	reader sshrepository.IdentityMaterialReader
	probe  sshrepository.IdentityHealthProber
	keys   sshrepository.IdentityKeyPreparer
}

// NewHealthService requires the private purpose ports before accepting any request and performs no network access during construction.
// NewHealthService требует private purpose ports до request admission и не выполняет network access при construction.
func NewHealthService(owner sshrepository.IdentityHealthOwner, reader sshrepository.IdentityMaterialReader, keys sshrepository.IdentityKeyPreparer, probe sshrepository.IdentityHealthProber) (*HealthService, error) {
	if owner == nil || reader == nil || keys == nil || probe == nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	return &HealthService{owner: owner, reader: reader, keys: keys, probe: probe}, nil
}

// Probe consumes one original health admission before reading a key and authenticating; after positive synchronous transport return it records completion with an independent bounded context even if the request was cancelled.
// Probe расходует original health admission до key read и authentication; после positive synchronous transport return записывает completion с independent bounded context даже при cancelled request.
func (service *HealthService) Probe(ctx context.Context, binding managedssh.Binding) (managedssh.HealthReceipt, error) {
	if ctx == nil || ctx.Err() != nil || binding.Validate() != nil || binding.ConnectionTestRef == "" {
		return managedssh.HealthReceipt{}, sshrepository.ErrIdentityRejected
	}
	admission, err := service.owner.BeginSSHIdentityHealthProbe(ctx, binding)
	if err != nil {
		return managedssh.HealthReceipt{}, err
	}
	if !managedssh.OwnerReference(admission.AttemptRef) || !managedssh.Digest(admission.SnapshotDigest) || !admission.Identity.Valid() || (admission.Mode != managedssh.ManagedTransport && admission.Mode != managedssh.ExecuteTransport) || !admission.ExpiresAt.After(time.Now()) || admission.ExpiresAt.After(time.Now().Add(30*time.Second)) {
		return managedssh.HealthReceipt{}, sshrepository.ErrIdentityRejected
	}
	operation, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	var observation managedssh.HealthObservation
	material, readErr := service.reader.ReadSSHIdentity(operation, admission.Identity)
	if readErr == nil {
		metadata, keyErr := service.keys.PrepareSSHIdentity(material)
		if len(material) > 0 && len(material) <= 16384 && keyErr == nil && metadata.ContentSHA256 == admission.Identity.ContentSHA256 && metadata.AuthenticationSHA256 == admission.Identity.AuthenticationSHA256 {
			actual, probeErr := service.probe.ProbeSSHIdentity(operation, admission, material)
			if probeErr == nil && operation.Err() == nil && managedssh.Digest(actual.ObservedSubjectDigest) && actual.QualifiedUntil.After(time.Now()) && actual.QualifiedUntil.Nanosecond() == 0 {
				observation = actual
			}
		}
	}
	clear(material)
	cancel()
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	receipt, err := service.owner.CompleteSSHIdentityHealthProbe(cleanup, binding, admission, observation)
	if err != nil {
		return managedssh.HealthReceipt{}, err
	}
	if receipt.TestRef != binding.ConnectionTestRef || (receipt.State != "SUCCEEDED" && receipt.State != "FAILED" && receipt.State != "CANCELLED") || receipt.Applied == (receipt.State == "CANCELLED") || receipt.State == "SUCCEEDED" && observation.ObservedSubjectDigest == "" {
		return managedssh.HealthReceipt{}, sshrepository.ErrIdentityRejected
	}
	return receipt, nil
}
