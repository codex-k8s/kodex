// Package kubernetesidentity consumes the existing owner's one-shot setup admission inside the broker.
// It owns no selection, retry scheduler or persistent authority and never returns private material.
// Пакет kubernetesidentity расходует существующий owner setup admission внутри broker.
// Не владеет selection, retry scheduler либо persistent authority и не возвращает private material.
package kubernetesidentity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// Service binds one fixed broker instance/private namespace to protected owner and purpose ports.
// The same selected identity supplies parser and cluster preflight; there is no elevated fallback.
// Service связывает fixed broker instance/private namespace с protected owner и purpose ports.
// Та же selected identity используется parser и cluster preflight без elevated fallback.
type Service struct {
	owner               targetrepo.IdentitySetupOwner
	verifier            targetrepo.IdentityCredentialVerifier
	writer              targetrepo.IdentityMaterialWriter
	reader              targetrepo.IdentityMaterialReader
	instance, namespace string
	now                 func() time.Time
}

// New requires all trusted ports and a fixed namespace before a direct setup listener can serve.
// Construction performs no dial, material access or readiness claim.
// New требует все trusted ports и fixed namespace до запуска direct setup listener.
// Constructor не делает dial, material access либо readiness claim.
func New(owner targetrepo.IdentitySetupOwner, verifier targetrepo.IdentityCredentialVerifier, writer targetrepo.IdentityMaterialWriter, reader targetrepo.IdentityMaterialReader, instance, namespace string) (*Service, error) {
	if owner == nil || verifier == nil || writer == nil || reader == nil || !managedkubernetes.ValidLocator(instance) || namespace == "" {
		return nil, targetrepo.ErrSetupRejected
	}
	return &Service{owner: owner, verifier: verifier, writer: writer, reader: reader, instance: instance, namespace: namespace, now: time.Now}, nil
}

// Materialize validates a private owned copy against exact owner metadata before spending one WRITE.
// A replay may reconcile an explicitly reported lost ACK, but can never send another Create.
// Materialize проверяет private owned copy по exact owner metadata до расхода одного WRITE.
// Replay может сверить явно reported lost ACK, но никогда не делает второй Create.
func (service *Service) Materialize(ctx context.Context, setupRef string, permit, material []byte) (value.KubernetesIdentitySetupView, error) {
	if len(material) == 0 || len(material) > managedkubernetes.MaximumCredentialBytes {
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupRejected
	}
	private := bytes.Clone(material)
	defer clear(private)
	snapshot, permitDigest, err := service.resolve(ctx, setupRef, permit)
	if err != nil {
		return value.KubernetesIdentitySetupView{}, err
	}
	if snapshot.Completed != nil {
		return *snapshot.Completed, nil
	}
	hash := sha256.Sum256(private)
	if uint32(len(private)) != snapshot.ContentBytes || hex.EncodeToString(hash[:]) != snapshot.Provision.ContentSHA256 {
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupRejected
	}
	if snapshot.ReconcileOnly {
		return service.observe(ctx, snapshot, permitDigest)
	}
	preflight, cancel := context.WithDeadline(ctx, snapshot.ExpiresAt)
	err = service.verifier.VerifyKubernetesIdentity(preflight, private, snapshot.Target)
	cancel()
	if err != nil || ctx.Err() != nil {
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupRejected
	}
	admission, err := service.claim(ctx, snapshot, permitDigest)
	if err != nil {
		return value.KubernetesIdentitySetupView{}, err
	}
	operation, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	identity, effectErr := service.writer.CreateKubernetesIdentity(operation, admission.Provision, private)
	cancel()
	completion := targetrepo.IdentityUploadCompletion{Admission: admission, Instance: service.instance}
	var rejection *targetrepo.IdentityAcknowledgementRejection
	switch {
	case errors.As(effectErr, &rejection) && rejection.Receipt.Valid():
		witness := rejection.Receipt
		completion.Rejected = &witness
	case errors.Is(effectErr, targetrepo.ErrIdentityCreateUnknown):
		completion.Unknown = true
	case effectErr != nil:
		// Unclassified port failures retain the unreported claim and cannot admit later observation.
		// Неклассифицированный port failure сохраняет unreported claim без допуска позднего observation.
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupUnavailable
	case !matchingIdentity(identity, admission.Provision):
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupUnavailable
	default:
		completion.Identity = &identity
	}
	return service.complete(ctx, completion)
}

// Observe spends only a separately admitted observation of the same immutable setup intent.
// Missing or unreported original completion is denied by the owner and never grants another write.
// Observe расходует лишь отдельно допущенный observation того же immutable setup intent.
// Missing либо unreported original completion отклоняется owner и не даёт другую write.
func (service *Service) Observe(ctx context.Context, setupRef string, permit []byte) (value.KubernetesIdentitySetupView, error) {
	snapshot, digest, err := service.resolve(ctx, setupRef, permit)
	if err != nil {
		return value.KubernetesIdentitySetupView{}, err
	}
	if snapshot.Completed != nil {
		return *snapshot.Completed, nil
	}
	if !snapshot.ReconcileOnly {
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupRejected
	}
	return service.observe(ctx, snapshot, digest)
}

// resolve validates bounded caller locators and exact owner snapshot before any private dependency.
// The raw permit is hashed once and never sent to CP or included in claim/complete diagnostics.
// resolve проверяет bounded caller locators и exact owner snapshot до private dependency.
// Raw permit хешируется один раз и не передаётся CP либо claim/complete diagnostics.
func (service *Service) resolve(ctx context.Context, setupRef string, permit []byte) (value.KubernetesIdentityUploadSnapshot, string, error) {
	if ctx == nil || ctx.Err() != nil || !managedkubernetes.ValidLocator(setupRef) || len(permit) != 32 {
		return value.KubernetesIdentityUploadSnapshot{}, "", targetrepo.ErrSetupRejected
	}
	hash := sha256.Sum256(permit)
	digest := hex.EncodeToString(hash[:])
	input, err := service.owner.ResolveKubernetesIdentityUpload(ctx, setupRef, digest)
	if err != nil {
		return value.KubernetesIdentityUploadSnapshot{}, "", targetrepo.ErrSetupUnavailable
	}
	if input.SetupRef != setupRef || ctx.Err() != nil {
		return value.KubernetesIdentityUploadSnapshot{}, "", targetrepo.ErrSetupRejected
	}
	if input.Completed != nil {
		if !input.ReconcileOnly || input.Completed.SetupRef != setupRef || input.Completed.State != "COMPLETED" || input.Completed.ReasonCode == "SETUP_ACK_CONFLICT" || input.Completed.ReasonCode == "SETUP_ACK_REJECTED" {
			return value.KubernetesIdentityUploadSnapshot{}, "", targetrepo.ErrSetupRejected
		}
		return input, digest, nil
	}
	if input.Provision.Validate(service.namespace) != nil || !managedkubernetes.ValidDigest(input.Digest) || input.ContentBytes == 0 || input.ContentBytes > managedkubernetes.MaximumCredentialBytes ||
		input.Target.ID != input.Provision.TargetRef || !service.now().Before(input.ExpiresAt) || input.ExpiresAt.After(service.now().Add(90*time.Second)) ||
		input.KnownIdentity != nil && (!input.ReconcileOnly || !matchingIdentity(*input.KnownIdentity, input.Provision)) {
		return value.KubernetesIdentityUploadSnapshot{}, "", targetrepo.ErrSetupRejected
	}
	return input, digest, nil
}

// claim checks the exact owner response before allowing a storage port to consume its bounded credit.
// A lost or malformed Claim response stays unreported and does not cause completion or retries.
// claim проверяет exact owner response до передачи bounded credit storage port.
// Lost либо malformed Claim response остаётся unreported без completion либо retries.
func (service *Service) claim(ctx context.Context, snapshot value.KubernetesIdentityUploadSnapshot, digest string) (value.KubernetesIdentityUploadAdmission, error) {
	input, err := service.owner.ClaimKubernetesIdentityUpload(ctx, targetrepo.IdentityUploadClaim{SetupRef: snapshot.SetupRef, PermitSHA256: digest, SnapshotDigest: snapshot.Digest, ContentSHA256: snapshot.Provision.ContentSHA256, WorkloadInstance: service.instance})
	if err != nil {
		return value.KubernetesIdentityUploadAdmission{}, targetrepo.ErrSetupUnavailable
	}
	if ctx.Err() != nil || input.SetupRef != snapshot.SetupRef || !managedkubernetes.ValidLocator(input.AttemptRef) || !managedkubernetes.ValidLocator(input.Fence) || input.Generation < 1 || input.Generation > 8 ||
		input.Provision != snapshot.Provision || input.ReconcileOnly != snapshot.ReconcileOnly || !service.now().Before(input.ExpiresAt) || input.ExpiresAt.After(snapshot.ExpiresAt) ||
		input.ExpiresAt.After(service.now().Add(30*time.Second)) || (input.KnownIdentity == nil) != (snapshot.KnownIdentity == nil) {
		return value.KubernetesIdentityUploadAdmission{}, targetrepo.ErrSetupRejected
	}
	if input.KnownIdentity != nil && *input.KnownIdentity != *snapshot.KnownIdentity {
		return value.KubernetesIdentityUploadAdmission{}, targetrepo.ErrSetupRejected
	}
	return input, nil
}

// observe performs exact known-UID reads or purpose-name observation without falling back to Create.
// A 404 or read failure retains UNKNOWN, never proof that a previously in-flight write did not occur.
// observe делает exact known-UID read либо purpose-name observation без fallback к Create.
// 404 либо read failure сохраняет UNKNOWN, не доказывая отсутствие прежней in-flight write.
func (service *Service) observe(ctx context.Context, snapshot value.KubernetesIdentityUploadSnapshot, digest string) (value.KubernetesIdentitySetupView, error) {
	admission, err := service.claim(ctx, snapshot, digest)
	if err != nil {
		return value.KubernetesIdentitySetupView{}, err
	}
	operation, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	completion := targetrepo.IdentityUploadCompletion{Admission: admission, Instance: service.instance}
	if admission.KnownIdentity != nil {
		material, readErr := service.reader.ReadKubernetesIdentity(operation, *admission.KnownIdentity)
		hash := sha256.Sum256(material)
		valid := readErr == nil && len(material) > 0 && len(material) <= managedkubernetes.MaximumCredentialBytes && hex.EncodeToString(hash[:]) == admission.Provision.ContentSHA256
		clear(material)
		if valid {
			identity := *admission.KnownIdentity
			completion.Identity = &identity
		} else {
			completion.Unknown = true
		}
	} else {
		identity, readErr := service.writer.ObserveKubernetesIdentity(operation, admission.Provision)
		var rejection *targetrepo.IdentityAcknowledgementRejection
		switch {
		case errors.As(readErr, &rejection) && rejection.Receipt.Valid():
			witness := rejection.Receipt
			completion.Rejected = &witness
		case readErr != nil:
			completion.Unknown = true
		case !matchingIdentity(identity, admission.Provision):
			cancel()
			return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupUnavailable
		default:
			completion.Identity = &identity
		}
	}
	cancel()
	return service.complete(ctx, completion)
}

// complete delivers one independently bounded receipt after local work joins, including cancellation.
// Failure to commit the receipt retains owner-closed UNREPORTED instead of inventing UNKNOWN evidence.
// complete передаёт один independently bounded receipt после local work join, включая cancellation.
// Ошибка commit сохраняет owner-closed UNREPORTED вместо выдуманного UNKNOWN evidence.
func (service *Service) complete(ctx context.Context, completion targetrepo.IdentityUploadCompletion) (value.KubernetesIdentitySetupView, error) {
	evidence, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	result, err := service.owner.CompleteKubernetesIdentityUpload(evidence, completion)
	if err != nil {
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupUnavailable
	}
	if result.SetupRef != completion.Admission.SetupRef || result.IdentityRef != completion.Admission.Provision.IdentityRef || result.IdentityRevision != completion.Admission.Provision.Revision || result.TargetRef != completion.Admission.Provision.TargetRef {
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupUnavailable
	}
	return result, nil
}

// matchingIdentity requires every immutable purpose pin before a local result becomes usable evidence.
// matchingIdentity требует каждый immutable purpose pin до превращения local result в usable evidence.
func matchingIdentity(identity value.KubernetesIdentityLocator, provision value.KubernetesIdentityProvision) bool {
	return identity.Validate(provision.Namespace) == nil && identity.IdentityRef == provision.IdentityRef && identity.TargetRef == provision.TargetRef && identity.Revision == provision.Revision && identity.ContentSHA256 == provision.ContentSHA256
}
