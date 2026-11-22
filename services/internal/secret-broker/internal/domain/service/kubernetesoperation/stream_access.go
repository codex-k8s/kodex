package kubernetesoperation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// CurrentStreamAccess binds repeated idle/exchange checks to one immutable original stream.
// It stores only metadata fingerprints, never credential material or a second execution loop.
// CurrentStreamAccess связывает idle/exchange проверки с одним immutable original stream.
// Хранит только metadata fingerprints без credential material либо второго execution loop.
type CurrentStreamAccess struct {
	owner               targetrepo.OperationStreamResolver
	target              targetrepo.StreamTargetAccess
	binding             managedkubernetes.Binding
	origin              managedkubernetes.StreamReceiptOrigin
	pin                 string
	planRef, planDigest string
	planRevision        int64
}

// NewCurrentStreamAccess captures immutable owner lineage before supervision without performing I/O.
// The target verifier must already be bound to the admitted child and its physical transport pins.
// NewCurrentStreamAccess закрепляет immutable owner lineage до supervision без I/O.
// Target verifier заранее привязан к admitted child и его physical transport pins.
func NewCurrentStreamAccess(owner targetrepo.OperationStreamResolver, target targetrepo.StreamTargetAccess, binding managedkubernetes.Binding, origin managedkubernetes.StreamReceiptOrigin, initial value.KubernetesOperationSnapshot) (*CurrentStreamAccess, error) {
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	pin, valid := streamOwnerReadPin(initial)
	if owner == nil || target == nil || err != nil || binding.InvocationRef == "" || origin.Validate() != nil || origin.BindingDigest != bindingDigest || origin.StreamGeneration != binding.Generation || origin.SnapshotDigest != initial.Digest || origin.TargetRef != initial.Target.ID || origin.TargetRevision != int64(initial.Target.Revision) || !valid || !initial.ExpiresAt.After(time.Now()) || !managedkubernetes.ValidLocator(initial.PlanRef) || initial.PlanRevision < 1 || !managedkubernetes.ValidDigest(initial.PlanDigest) {
		return nil, targetrepo.ErrFrameUnavailable
	}
	return &CurrentStreamAccess{owner: owner, target: target, binding: binding, origin: origin, pin: pin, planRef: initial.PlanRef, planRevision: initial.PlanRevision, planDigest: initial.PlanDigest}, nil
}

// AuthorizeStream verifies the current exact owner graph before contacting the selected target.
// Lease renewal may extend credit, but cannot change immutable identity, policy, input or plan.
// AuthorizeStream проверяет current exact owner graph до обращения к selected target.
// Lease renewal может продлить credit, но не меняет immutable identity, policy, input либо plan.
func (access *CurrentStreamAccess) AuthorizeStream(ctx context.Context, binding managedkubernetes.Binding, origin managedkubernetes.StreamReceiptOrigin) (time.Time, error) {
	if ctx == nil || ctx.Err() != nil || access == nil || binding != access.binding || origin != access.origin {
		return time.Time{}, targetrepo.ErrFrameUnavailable
	}
	check, cancel := context.WithTimeout(ctx, outputOwnerTimeout)
	defer cancel()
	current, err := access.owner.ResolveKubernetesStream(check, binding, origin.StreamRef)
	if err != nil || check.Err() != nil {
		return time.Time{}, targetrepo.ErrFrameUnavailable
	}
	pin, valid := streamOwnerReadPin(current)
	if !valid || pin != access.pin || current.PlanRef != access.planRef || current.PlanRevision != access.planRevision || current.PlanDigest != access.planDigest || !current.ExpiresAt.After(time.Now()) {
		return time.Time{}, targetrepo.ErrFrameUnavailable
	}
	deadline, err := access.target.VerifyKubernetesStreamAccess(check, current)
	if err != nil || check.Err() != nil {
		return time.Time{}, targetrepo.ErrFrameUnavailable
	}
	if current.ExpiresAt.Before(deadline) {
		deadline = current.ExpiresAt
	}
	if !deadline.After(time.Now()) {
		return time.Time{}, targetrepo.ErrFrameUnavailable
	}
	return deadline, nil
}

// streamOwnerReadPin hashes only immutable base metadata and excludes renewable lifetime and child node selection.
// The separately bound native verifier checks the exact child node before any new output/input credit.
// streamOwnerReadPin хеширует immutable base metadata без renewable lifetime и child node selection.
// Отдельно привязанный native verifier проверяет exact child node до нового output/input credit.
func streamOwnerReadPin(snapshot value.KubernetesOperationSnapshot) (string, bool) {
	_, canonical, err := managedkubernetes.DecodeOperationInput(snapshot.Operation, snapshot.NormalizedInput)
	if err != nil || !bytes.Equal(canonical, snapshot.NormalizedInput) || !managedkubernetes.ValidDigest(snapshot.Digest) || !managedkubernetes.ValidDigest(snapshot.SubjectDigest) || snapshot.IdentityGeneration < 1 || snapshot.IdentityRef != snapshot.Identity.IdentityRef || snapshot.Identity.TargetRef != snapshot.Target.ID || snapshot.Identity.Validate(snapshot.Identity.Namespace) != nil || snapshot.Target.Revision < 1 {
		return "", false
	}
	raw, err := json.Marshal(struct {
		Digest, Operation, Risk, ApprovalPolicy, IdentityRef, SubjectDigest string
		IdentityGeneration                                                  int64
		IdentityPins                                                        [7]string
		IdentityRevision                                                    int64
		Target                                                              value.KubernetesTarget
		Input                                                               []byte
		Resource                                                            *managedkubernetes.ResourceAddress
		Discovery                                                           *managedkubernetes.DiscoveryAddress
		ProtectedNamespaces                                                 []string
	}{snapshot.Digest, snapshot.Operation, snapshot.Risk, snapshot.ApprovalPolicy, snapshot.IdentityRef, snapshot.SubjectDigest, snapshot.IdentityGeneration, [7]string{snapshot.Identity.IdentityRef, snapshot.Identity.TargetRef, snapshot.Identity.Namespace, snapshot.Identity.Name, snapshot.Identity.UID, snapshot.Identity.ResourceVersion, snapshot.Identity.ContentSHA256}, snapshot.Identity.Revision, snapshot.Target, canonical, snapshot.Resource, snapshot.Discovery, snapshot.ProtectedNamespaces})
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(raw)
	clear(raw)
	return hex.EncodeToString(sum[:]), true
}
