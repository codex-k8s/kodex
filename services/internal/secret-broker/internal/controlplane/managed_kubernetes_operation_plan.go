package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// maximumKubernetesPlanChildren matches the owner cap after expansion, including dependent stages.
// maximumKubernetesPlanChildren соответствует owner cap после expansion, включая зависимые стадии.
const maximumKubernetesPlanChildren = 256

// PrepareKubernetesChildren sends only finite observations and never manufactures a child or effect.
// The protected owner derives operations and permissions; this adapter verifies wire consistency.
// PrepareKubernetesChildren передаёт только finite observations без создания child либо effect.
// Protected owner выводит operations и permissions; адаптер проверяет wire consistency.
func (owner *Owner) PrepareKubernetesChildren(ctx context.Context, binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, candidates []managedkubernetes.ChildCandidate) (targetrepo.OperationPlan, error) {
	var empty targetrepo.OperationPlan
	proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, candidates)
	if !owner.kubernetesEffectReady(ctx, binding) || err != nil || !snapshot.ExpiresAt.After(time.Now()) || !managedkubernetes.ValidLocator(snapshot.Target.ID) || snapshot.Target.Revision == 0 ||
		snapshot.PlanRef == "" && (snapshot.PlanRevision != 0 || snapshot.PlanDigest != "") || snapshot.PlanRef != "" && (!managedkubernetes.ValidLocator(snapshot.PlanRef) || snapshot.PlanRevision < 1 || !managedkubernetes.ValidDigest(snapshot.PlanDigest)) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	request := &cp.PrepareManagedKubernetesChildrenRequest{Binding: kubernetesHealthBindingToProto(binding), SnapshotDigest: snapshot.Digest, ProposalDigest: proposal}
	for _, candidate := range candidates {
		state, known := kubernetesObservationToProto(candidate.ObservationState)
		if !known {
			return empty, targetrepo.ErrOperationUnavailable
		}
		entry := &cp.ManagedKubernetesChildCandidate{Resource: kubernetesResourceToProto(candidate.Resource), ObservedResourceDigest: candidate.ObservedResourceDigest, RegisteredSchemaDigest: candidate.RegisteredSchemaDigest, ObservationState: state, NamespaceUid: candidate.NamespaceUID}
		if candidate.PodWitness != nil {
			entry.PodWitness = &cp.ManagedKubernetesPodNodeWitness{NodeName: candidate.PodWitness.NodeName, NodeUid: candidate.PodWitness.NodeUID, Container: candidate.PodWitness.Container}
		}
		request.Candidates = append(request.Candidates, entry)
	}
	response, err := owner.client.KubernetesOperations.PrepareManagedKubernetesChildren(ctx, request)
	if err != nil || ctx.Err() != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(response.GetPlanRef()) || response.GetPlanRevision() < 1 || !managedkubernetes.ValidDigest(response.GetPlanDigest()) || len(response.GetChildren()) > maximumKubernetesPlanChildren ||
		snapshot.PlanRef != "" && (response.GetPlanRef() != snapshot.PlanRef || response.GetPlanRevision() != snapshot.PlanRevision || response.GetPlanDigest() != snapshot.PlanDigest) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	result := targetrepo.OperationPlan{Ref: response.GetPlanRef(), Revision: response.GetPlanRevision(), Digest: response.GetPlanDigest(), Children: make([]targetrepo.OperationChild, 0, len(response.GetChildren()))}
	seen := make(map[string]struct{}, len(response.GetChildren()))
	for _, child := range response.GetChildren() {
		parsed, valid := kubernetesOperationChild(child, snapshot.Target)
		if !valid {
			return empty, targetrepo.ErrOperationUnavailable
		}
		if _, exists := seen[parsed.Ref]; exists {
			return empty, targetrepo.ErrOperationUnavailable
		}
		seen[parsed.Ref] = struct{}{}
		result.Children = append(result.Children, parsed)
	}
	return result, nil
}

// kubernetesOperationChild validates canonical bytes and exact coordinates without inferring grant eligibility.
// Repeated resource coordinates are allowed for owner-expanded dependent stages; child refs stay unique.
// kubernetesOperationChild проверяет canonical bytes и exact coordinates без вывода grant eligibility.
// Повтор coordinates допустим для owner-expanded dependent stages; child refs остаются уникальными.
func kubernetesOperationChild(input *cp.ManagedKubernetesChildEffect, target value.KubernetesTarget) (targetrepo.OperationChild, bool) {
	var empty targetrepo.OperationChild
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(input.GetChildRef()) || !managedkubernetes.ValidDigest(input.GetInputDigest()) {
		return empty, false
	}
	switch input.GetState() {
	case "WAITING_OWNER", "WAITING_PREDECESSOR", "READY", "ATTEMPTING", "SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED":
	default:
		return empty, false
	}
	registered, known := integrationpackage.KubernetesOperationByKey(input.GetOperation())
	parsed, canonical, err := managedkubernetes.DecodeOperationInput(input.GetOperation(), input.GetNormalizedInputJson())
	digest := sha256.Sum256(canonical)
	if !known || err != nil || !bytes.Equal(canonical, input.GetNormalizedInputJson()) || hex.EncodeToString(digest[:]) != input.GetInputDigest() {
		return empty, false
	}
	result := targetrepo.OperationChild{Ref: input.GetChildRef(), Operation: input.GetOperation(), InputDigest: input.GetInputDigest(), State: input.GetState(), Input: parsed, NormalizedInput: canonical}
	if input.GetResource() == nil {
		if parsed.Resource != "" || input.GetObservation() != nil || input.GetNodeTransport() != nil || registered.Execution != integrationpackage.KubernetesPlan && registered.Execution != integrationpackage.KubernetesOffline {
			return empty, false
		}
		return result, true
	}
	resource, valid := kubernetesResourceFromProto(input.GetResource())
	all := input.GetOperation() == "kubernetes.list.all-namespaces" || input.GetOperation() == "kubernetes.watch.all-namespaces"
	if !valid || registered.Execution == integrationpackage.KubernetesOffline || resource.AllNamespaces != all || resource.Group != parsed.Group || resource.Version != parsed.Version || resource.Resource != parsed.Resource || resource.Namespace != parsed.Namespace || resource.Name != parsed.Name || resource.Subresource != parsed.Subresource || parsed.UID != "" && resource.UID != parsed.UID || parsed.ResourceVersion != "" && resource.ResourceVersion != parsed.ResourceVersion {
		return empty, false
	}
	observation, valid := kubernetesChildObservation(input.GetObservation())
	if !valid || observation.Resource != resource {
		return empty, false
	}
	result.Resource, result.Observation = resource, &observation
	if input.GetNodeTransport() != nil {
		if registered.Execution != integrationpackage.KubernetesStream {
			return empty, false
		}
		node := input.GetNodeTransport()
		pinClock := time.Now()
		switch result.State {
		case "SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED":
			// Historical pins grant no I/O; unfinished children still require current validity.
			// Historical pins не разрешают I/O; незавершённые children требуют current validity.
			if node.GetNotBefore() == nil || node.GetNotBefore().AsTime().After(pinClock) {
				return empty, false
			}
			pinClock = node.GetNotBefore().AsTime()
		}
		pin, valid := kubernetesOperationNode(node, target, pinClock)
		if !valid {
			return empty, false
		}
		result.NodeTransport = &pin
		if observation.PodWitness == nil || observation.PodWitness.NodeName != pin.NodeName || observation.PodWitness.NodeUID != pin.NodeUID {
			return empty, false
		}
	}
	return result, true
}

// kubernetesChildObservation preserves every original observation pin and rejects unknown nested authority fields.
// kubernetesChildObservation сохраняет все original observation pins и отвергает unknown nested authority fields.
func kubernetesChildObservation(input *cp.ManagedKubernetesChildCandidate) (managedkubernetes.ChildCandidate, bool) {
	var empty managedkubernetes.ChildCandidate
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return empty, false
	}
	resource, valid := kubernetesResourceFromProto(input.GetResource())
	if !valid {
		return empty, false
	}
	state := managedkubernetes.ObservationState("")
	switch input.GetObservationState() {
	case cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_PRESENT:
		state = managedkubernetes.ObservationPresent
	case cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_ABSENT:
		state = managedkubernetes.ObservationAbsent
	case cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_COLLECTION:
		state = managedkubernetes.ObservationCollection
	case cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_UNVERSIONED_READ:
		state = managedkubernetes.ObservationUnversionedRead
	default:
		return empty, false
	}
	result := managedkubernetes.ChildCandidate{ObservationState: state, Resource: resource, ObservedResourceDigest: input.GetObservedResourceDigest(), RegisteredSchemaDigest: input.GetRegisteredSchemaDigest(), NamespaceUID: input.GetNamespaceUid()}
	if witness := input.GetPodWitness(); witness != nil {
		if len(witness.ProtoReflect().GetUnknown()) != 0 {
			return empty, false
		}
		result.PodWitness = &managedkubernetes.PodNodeWitness{NodeName: witness.GetNodeName(), NodeUID: witness.GetNodeUid(), Container: witness.GetContainer()}
	}
	return result, result.Validate() == nil
}

// kubernetesResourceToProto preserves every observed coordinate and concurrency pin without defaulting scope.
// kubernetesResourceToProto сохраняет все observed coordinates и concurrency pins без scope defaults.
func kubernetesResourceToProto(resource managedkubernetes.ResourceAddress) *cp.ManagedKubernetesResourceAddress {
	return &cp.ManagedKubernetesResourceAddress{Group: resource.Group, Version: resource.Version, Resource: resource.Resource, Namespace: resource.Namespace, Name: resource.Name, Subresource: resource.Subresource, Uid: resource.UID, ResourceVersion: resource.ResourceVersion, Namespaced: resource.Namespaced, AllNamespaces: resource.AllNamespaces}
}

// kubernetesResourceFromProto rejects unknown authority-bearing coordinates before address use.
// kubernetesResourceFromProto отклоняет unknown authority-bearing coordinates до использования адреса.
func kubernetesResourceFromProto(input *cp.ManagedKubernetesResourceAddress) (managedkubernetes.ResourceAddress, bool) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return managedkubernetes.ResourceAddress{}, false
	}
	result := managedkubernetes.ResourceAddress{Group: input.GetGroup(), Version: input.GetVersion(), Resource: input.GetResource(), Namespace: input.GetNamespace(), Name: input.GetName(), Subresource: input.GetSubresource(), UID: input.GetUid(), ResourceVersion: input.GetResourceVersion(), Namespaced: input.GetNamespaced(), AllNamespaces: input.GetAllNamespaces()}
	return result, result.Validate() == nil
}

// kubernetesObservationToProto maps the closed observation set without treating unknown as PRESENT.
// kubernetesObservationToProto отображает closed observation set без превращения unknown в PRESENT.
func kubernetesObservationToProto(state managedkubernetes.ObservationState) (cp.ManagedKubernetesObservationState, bool) {
	switch state {
	case managedkubernetes.ObservationPresent:
		return cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_PRESENT, true
	case managedkubernetes.ObservationAbsent:
		return cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_ABSENT, true
	case managedkubernetes.ObservationCollection:
		return cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_COLLECTION, true
	case managedkubernetes.ObservationUnversionedRead:
		return cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_UNVERSIONED_READ, true
	default:
		return cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_UNSPECIFIED, false
	}
}
