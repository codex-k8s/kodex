package kubernetestarget

import (
	"context"
	"reflect"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// prepareLogsStream binds the exact owner observation to private API/container coordinates without a privileged node transport.
// It reuses the one-attempt preparation and worker handoff used by other streams.
// prepareLogsStream связывает exact owner observation с private API/container coordinates без privileged node transport.
// Используется общая one-attempt preparation и worker handoff остальных streams.
func (factory *PodStreamPreparer) prepareLogsStream(ctx context.Context, snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild, input managedkubernetes.OperationInput) (targetrepo.PodStreamPreparation, error) {
	if child.NodeTransport != nil || child.Observation.PodWitness != nil || snapshot.NodeTransport != nil || snapshot.Identity.Validate(factory.identity.namespace) != nil || snapshot.IdentityRef != snapshot.Identity.IdentityRef || snapshot.Identity.TargetRef != snapshot.Target.ID || !managedkubernetes.ValidDigest(snapshot.SubjectDigest) || snapshot.IdentityGeneration < 1 || !snapshot.ExpiresAt.After(time.Now()) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	check, cancel := context.WithDeadline(ctx, snapshot.ExpiresAt)
	defer cancel()
	credential, err := LoadCredential(check, factory.identity.reader, snapshot.Identity, snapshot.Target, time.Now())
	if err != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	observed, err := credential.observePodStream(check, "kubernetes.logs", input, snapshot.SubjectDigest, snapshot.ProtectedNamespaces)
	if err != nil || observed.PodWitness == nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	witness := *observed.PodWitness
	observed.PodWitness = nil
	if observed.NamespaceUID == "" {
		observed.NamespaceUID = child.Observation.NamespaceUID
	}
	if !reflect.DeepEqual(observed, *child.Observation) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	_, duration, _, err := credential.logsRequest(check, input, child.Resource, witness.Container)
	if err != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	current, err := ownedPodStreamAccessSnapshot(snapshot, value.KubernetesNodeTransport{})
	if err != nil {
		return nil, err
	}
	current.NodeTransport = nil
	pod := PodStreamTarget{Namespace: child.Resource.Namespace, Name: child.Resource.Name, UID: child.Resource.UID, NodeName: witness.NodeName, Container: witness.Container}
	access := &boundLogsAccess{reader: factory.identity.reader, identity: current.Identity, subject: current.SubjectDigest, metadataDigest: current.Target.MetadataDigest(), protected: slices.Clone(current.ProtectedNamespaces), pod: pod, input: input, address: child.Resource}
	deadline := time.Now().Add(duration)
	if check.Err() != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	return &preparedPodStream{current: current, logsAccess: access, binding: targetrepo.PodStreamPreparationBinding{SnapshotDigest: snapshot.Digest, ChildRef: child.Ref, Operation: child.Operation, InputDigest: child.InputDigest, TargetDigest: snapshot.Target.MetadataDigest(), SubjectDigest: snapshot.SubjectDigest, Observation: observed, HardDeadline: deadline}}, nil
}
