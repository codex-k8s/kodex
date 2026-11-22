package kubernetesoperation

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
)

// Health uses only exact purpose material after the owner's one-shot test admission.
// Owner completion must join before any successful authentication result leaves this service.
// Health использует exact purpose material только после owner one-shot test admission.
// Owner completion завершается до выдачи successful authentication result из service.
type Health struct {
	owner     targetrepo.IdentityHealthOwner
	reader    targetrepo.IdentityMaterialReader
	verifier  targetrepo.IdentityHealthVerifier
	namespace string
}

// NewHealth requires explicit purpose storage and verification ports without a generic Secret fallback.
// NewHealth требует explicit purpose storage и verification ports без generic Secret fallback.
func NewHealth(owner targetrepo.IdentityHealthOwner, reader targetrepo.IdentityMaterialReader, verifier targetrepo.IdentityHealthVerifier, namespace string) (*Health, error) {
	address := managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: namespace}
	if owner == nil || reader == nil || verifier == nil || address.Validate() != nil || namespace == "kodex-system" {
		return nil, targetrepo.ErrHealthUnavailable
	}
	return &Health{owner: owner, reader: reader, verifier: verifier, namespace: namespace}, nil
}

// Execute tests an existing health claim exactly once and reports even failed joined observations.
// Loss of Begin/Complete acknowledgements never permits a new probe through this original binding.
// Execute проверяет existing health claim ровно один раз и сообщает также failed joined observations.
// Потеря Begin/Complete acknowledgements не разрешает новый probe через original binding.
func (service *Health) Execute(ctx context.Context, binding managedkubernetes.Binding) (targetrepo.IdentityHealthReceipt, error) {
	if ctx == nil || binding.Validate() != nil || binding.ConnectionTestRef == "" || binding.InvocationRef != "" {
		return targetrepo.IdentityHealthReceipt{}, targetrepo.ErrHealthUnavailable
	}
	admission, err := service.owner.BeginKubernetesIdentityHealth(ctx, binding)
	if err != nil {
		return targetrepo.IdentityHealthReceipt{}, targetrepo.ErrHealthUnavailable
	}
	if !managedkubernetes.ValidLocator(admission.AttemptRef) || !managedkubernetes.ValidDigest(admission.SnapshotDigest) ||
		admission.Identity.Validate(service.namespace) != nil || admission.Target.ID != admission.Identity.TargetRef || admission.Target.Revision < 1 ||
		!admission.ExpiresAt.After(time.Now()) || admission.ExpiresAt.After(time.Now().Add(30*time.Second)) {
		return targetrepo.IdentityHealthReceipt{}, targetrepo.ErrHealthUnavailable
	}
	work, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	material, readErr := service.reader.ReadKubernetesIdentity(work, admission.Identity)
	outcome, subject := "UNAVAILABLE", ""
	if readErr == nil && len(material) > 0 && len(material) <= managedkubernetes.MaximumCredentialBytes {
		observed, observeErr := service.verifier.ObserveKubernetesIdentity(work, material, admission.Target)
		if observeErr == nil && managedkubernetes.ValidDigest(observed) {
			outcome, subject = "AUTHENTICATED", observed
		} else if errors.Is(observeErr, targetrepo.ErrHealthAuthenticationRejected) {
			outcome = "AUTH_REJECTED"
		}
	}
	clear(material)
	cancel()
	complete, finish := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finish()
	receipt, err := service.owner.CompleteKubernetesIdentityHealth(complete, targetrepo.IdentityHealthCompletion{
		Binding: binding, Admission: admission, Outcome: outcome, SubjectDigest: subject})
	if err != nil || receipt.TestRef != binding.ConnectionTestRef || !receipt.Applied ||
		(outcome == "AUTHENTICATED" && receipt.State != "SUCCEEDED") ||
		(outcome != "AUTHENTICATED" && receipt.State != "FAILED") {
		return targetrepo.IdentityHealthReceipt{}, targetrepo.ErrHealthUnavailable
	}
	return receipt, nil
}
