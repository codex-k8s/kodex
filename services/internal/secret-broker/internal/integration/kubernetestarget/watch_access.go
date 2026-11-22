package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// observeWatch authenticates collection scope and actual registered schema without opening a watch or inventing object UID/RV.
// observeWatch проверяет collection scope и actual registered schema без открытия watch либо выдуманных object UID/RV.
func (credential Credential) observeWatch(ctx context.Context, operation string, input managedkubernetes.OperationInput, subject string, protected []string) (managedkubernetes.ChildCandidate, *registeredResourceSchema, error) {
	address := ResourceAddress{Group: input.Group, Version: input.Version, Resource: input.Resource, Namespace: input.Namespace, Namespaced: input.Namespace != "" || operation == "kubernetes.watch.all-namespaces", AllNamespaces: operation == "kubernetes.watch.all-namespaces"}
	if _, _, err := credential.watchRequest(ctx, operation, input, address); err != nil || resourceNamespaceProtected(address, protected) {
		return managedkubernetes.ChildCandidate{}, nil, targetrepo.ErrOperationUnavailable
	}
	registered, known := integrationpackage.KubernetesOperationByKey(operation)
	if !known || credential.VerifyClusterIdentity(ctx) != nil {
		return managedkubernetes.ChildCandidate{}, nil, targetrepo.ErrOperationUnavailable
	}
	requirement := AccessRequirement{Group: address.Group, Version: address.Version, Resource: address.Resource, Namespace: address.Namespace, Namespaced: address.Namespaced, AllNamespaces: address.AllNamespaces, Verb: "watch"}
	if credential.Authorize(ctx, subject, []AccessRequirement{requirement}) != nil {
		return managedkubernetes.ChildCandidate{}, nil, targetrepo.ErrOperationUnavailable
	}
	schema, err := credential.discoverResource(ctx, subject, address, registered)
	if err != nil {
		return managedkubernetes.ChildCandidate{}, nil, targetrepo.ErrOperationUnavailable
	}
	observation := managedkubernetes.ResourceObservation{State: managedkubernetes.ObservationCollection, Resource: address, RegisteredSchemaDigest: schema.digest}
	raw, err := json.Marshal(observation)
	if err != nil {
		return managedkubernetes.ChildCandidate{}, nil, targetrepo.ErrOperationUnavailable
	}
	digest := sha256.Sum256(raw)
	candidate := managedkubernetes.ChildCandidate{ObservationState: observation.State, Resource: address, RegisteredSchemaDigest: schema.digest, ObservedResourceDigest: hex.EncodeToString(digest[:])}
	if candidate.Validate() != nil || ctx.Err() != nil {
		return managedkubernetes.ChildCandidate{}, nil, targetrepo.ErrOperationUnavailable
	}
	return candidate, schema, nil
}

// boundWatchAccess retains only admitted scope/schema and private identity coordinates; every output credit repeats native authorization.
// boundWatchAccess сохраняет только admitted scope/schema и private identity coordinates; каждый output credit повторяет native authorization.
type boundWatchAccess struct {
	reader                             targetrepo.IdentityMaterialReader
	identity                           value.KubernetesIdentityLocator
	subject, metadataDigest, operation string
	protected                          []string
	input                              managedkubernetes.OperationInput
	observation                        managedkubernetes.ChildCandidate
	schema                             *registeredResourceSchema
}

// authorizedCredential refuses changed scope, schema, identity or current permissions without reopening the watched collection.
// authorizedCredential отклоняет changed scope, schema, identity либо current permissions без повторного открытия watched collection.
func (access *boundWatchAccess) authorizedCredential(ctx context.Context, current value.KubernetesOperationSnapshot) (Credential, time.Time, error) {
	if access == nil || ctx == nil || ctx.Err() != nil || current.NodeTransport != nil || current.Target.MetadataDigest() != access.metadataDigest || current.Identity != access.identity || current.IdentityRef != access.identity.IdentityRef || current.SubjectDigest != access.subject || !slices.Equal(current.ProtectedNamespaces, access.protected) || !current.ExpiresAt.After(time.Now()) {
		return Credential{}, time.Time{}, targetrepo.ErrOperationUnavailable
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now()
	credential, err := LoadCredential(check, access.reader, access.identity, current.Target, now)
	if err != nil {
		return Credential{}, time.Time{}, targetrepo.ErrOperationUnavailable
	}
	deadline, err := credentialOutputDeadline(credential, now, current.ExpiresAt)
	if err != nil {
		return Credential{}, time.Time{}, targetrepo.ErrOperationUnavailable
	}
	credit, finish := context.WithDeadline(check, deadline)
	defer finish()
	observed, _, err := credential.observeWatch(credit, access.operation, access.input, access.subject, access.protected)
	// Namespace UID, when present, is owner evidence checked by the owner; discovery does not manufacture a namespace GET.
	// Namespace UID при наличии — owner evidence с owner-проверкой; discovery не выдумывает namespace GET.
	observed.NamespaceUID = access.observation.NamespaceUID
	if err != nil || !reflect.DeepEqual(observed, access.observation) || credit.Err() != nil || !deadline.After(time.Now()) {
		return Credential{}, time.Time{}, targetrepo.ErrOperationUnavailable
	}
	return credential, deadline, nil
}

// VerifyKubernetesStreamAccess binds each delivery credit to fresh native checks while the existing owner verifies grants and leases.
// VerifyKubernetesStreamAccess связывает каждый delivery credit со fresh native checks; grants и leases проверяет existing owner.
func (access *boundWatchAccess) VerifyKubernetesStreamAccess(ctx context.Context, current value.KubernetesOperationSnapshot) (time.Time, error) {
	_, deadline, err := access.authorizedCredential(ctx, current)
	return deadline, err
}

// prepareWatchStream validates the original collection proposal and shares the existing one-attempt stream preparation lifecycle.
// prepareWatchStream проверяет original collection proposal и использует existing one-attempt stream preparation lifecycle.
func (factory *PodStreamPreparer) prepareWatchStream(ctx context.Context, snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild, input managedkubernetes.OperationInput) (targetrepo.PodStreamPreparation, error) {
	if child.NodeTransport != nil || child.Observation.PodWitness != nil || snapshot.NodeTransport != nil || snapshot.Identity.Validate(factory.identity.namespace) != nil || snapshot.IdentityRef != snapshot.Identity.IdentityRef || snapshot.Identity.TargetRef != snapshot.Target.ID || !managedkubernetes.ValidDigest(snapshot.SubjectDigest) || snapshot.IdentityGeneration < 1 || !snapshot.ExpiresAt.After(time.Now()) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	check, cancel := context.WithDeadline(ctx, snapshot.ExpiresAt)
	defer cancel()
	credential, err := LoadCredential(check, factory.identity.reader, snapshot.Identity, snapshot.Target, time.Now())
	if err != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	observed, schema, err := credential.observeWatch(check, child.Operation, input, snapshot.SubjectDigest, snapshot.ProtectedNamespaces)
	observed.NamespaceUID = child.Observation.NamespaceUID
	if err != nil || !reflect.DeepEqual(observed, *child.Observation) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	_, duration, err := credential.watchRequest(check, child.Operation, input, child.Resource)
	if err != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	current, err := ownedPodStreamAccessSnapshot(snapshot, value.KubernetesNodeTransport{})
	if err != nil {
		return nil, err
	}
	current.NodeTransport = nil
	access := &boundWatchAccess{reader: factory.identity.reader, identity: current.Identity, subject: current.SubjectDigest, metadataDigest: current.Target.MetadataDigest(), operation: child.Operation, protected: slices.Clone(current.ProtectedNamespaces), input: input, observation: observed, schema: schema}
	if check.Err() != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	return &preparedPodStream{current: current, watchAccess: access, binding: targetrepo.PodStreamPreparationBinding{SnapshotDigest: snapshot.Digest, ChildRef: child.Ref, Operation: child.Operation, InputDigest: child.InputDigest, TargetDigest: snapshot.Target.MetadataDigest(), SubjectDigest: snapshot.SubjectDigest, Observation: observed, HardDeadline: time.Now().Add(duration)}}, nil
}

// String hides private identity and scope details from diagnostics.
// String скрывает private identity и scope details из diagnostics.
func (*boundWatchAccess) String() string { return "[managed Kubernetes watch authorization]" }

// GoString preserves redaction under detailed formatting.
// GoString сохраняет redaction при подробном форматировании.
func (access *boundWatchAccess) GoString() string { return access.String() }
