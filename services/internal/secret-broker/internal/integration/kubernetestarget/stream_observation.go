package kubernetestarget

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// ObservePodStream reads one exact Pod through registered discovery and the selected identity.
// It returns only a proposal: node UID and serving transport must be assigned by the owner.
// No stream, node GET, command or external effect is started during observation.
// ObservePodStream читает exact Pod через registered discovery и выбранную identity.
// Возвращается только proposal: UID узла и serving transport назначает owner.
// Наблюдение не запускает stream, GET узла, команду либо внешний effect.
func (credential Credential) ObservePodStream(ctx context.Context, operation string, input managedkubernetes.OperationInput, subjectDigest string, protectedNamespaces []string) (managedkubernetes.ChildCandidate, error) {
	candidate, err := credential.observePodStream(ctx, operation, input, subjectDigest, protectedNamespaces)
	if operation == "kubernetes.logs" {
		candidate.PodWitness = nil
	}
	return candidate, err
}

// observePodStream retains private container selection for preparation; HTTP logs never publish a node-transport witness.
// observePodStream сохраняет private container selection для подготовки; HTTP logs не публикуют node-transport witness.
func (credential Credential) observePodStream(ctx context.Context, operation string, input managedkubernetes.OperationInput, subjectDigest string, protectedNamespaces []string) (managedkubernetes.ChildCandidate, error) {
	denied := errors.New(privilegedTransportUnavailable)
	if ctx == nil || ctx.Err() != nil || (operation != "kubernetes.logs" && operation != "kubernetes.exec" && operation != "kubernetes.attach" && operation != "kubernetes.port-forward" && operation != "kubernetes.cp.upload" && operation != "kubernetes.cp.download") {
		return managedkubernetes.ChildCandidate{}, denied
	}
	raw, err := managedkubernetes.EncodeOperationInput(operation, input)
	if err != nil {
		return managedkubernetes.ChildCandidate{}, denied
	}
	defer clear(raw)
	owned, canonical, err := managedkubernetes.DecodeOperationInput(operation, raw)
	defer clear(canonical)
	if err != nil || owned.Group != "" || owned.Version != "v1" || owned.Resource != "pods" || owned.Namespace == "" || owned.Name == "" || owned.Subresource != "" {
		return managedkubernetes.ChildCandidate{}, denied
	}
	check, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	read := managedkubernetes.OperationInput{Version: "v1", Resource: "pods", Namespace: owned.Namespace, Name: owned.Name, UID: owned.UID, ResourceVersion: owned.ResourceVersion}
	address := ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: read.Namespace, Name: read.Name, UID: read.UID, ResourceVersion: read.ResourceVersion}
	prepared, err := credential.PrepareResourceOperation(check, "kubernetes.get", read, address, subjectDigest, protectedNamespaces)
	if err != nil {
		return managedkubernetes.ChildCandidate{}, denied
	}
	defer prepared.Close()
	observed := prepared.Observation()
	if observed.State != managedkubernetes.ObservationPresent || prepared.RegisteredKind() != "Pod" ||
		credential.ValidateQualifiedResourcePolicy(check, operation, observed.Resource, "Pod", observed.RegisteredSchemaDigest, subjectDigest, prepared.current, protectedNamespaces) != nil {
		return managedkubernetes.ChildCandidate{}, denied
	}
	options, err := resourceOperationOptions(owned)
	if err != nil {
		return managedkubernetes.ChildCandidate{}, denied
	}
	requested, _ := options["container"].(string)
	witness, err := observedPodPlacement(prepared.current, requested)
	if err != nil || check.Err() != nil {
		return managedkubernetes.ChildCandidate{}, denied
	}
	candidate := managedkubernetes.ChildCandidate{ObservationState: observed.State, Resource: observed.Resource,
		ObservedResourceDigest: observed.ObservedResourceDigest, RegisteredSchemaDigest: observed.RegisteredSchemaDigest,
		NamespaceUID: observed.NamespaceUID, PodWitness: &witness}
	if candidate.ValidateProposal() != nil {
		return managedkubernetes.ChildCandidate{}, denied
	}
	return candidate, nil
}

// observedPodPlacement follows kubectl's explicit, annotated, then first-container selection.
// It refuses ambiguous container identities and terminating Pods; nodeName never becomes a UID.
// observedPodPlacement выбирает explicit, annotated, затем первый container по правилам kubectl.
// Неоднозначные имена и terminating Pods отвергаются; nodeName не превращается в UID.
func observedPodPlacement(object map[string]any, requested string) (managedkubernetes.PodNodeWitness, error) {
	denied := errors.New(privilegedTransportUnavailable)
	metadata, metadataOK := object["metadata"].(map[string]any)
	spec, specOK := object["spec"].(map[string]any)
	if !metadataOK || !specOK || metadata["deletionTimestamp"] != nil {
		return managedkubernetes.PodNodeWitness{}, denied
	}
	node, _ := spec["nodeName"].(string)
	regular, _ := spec["containers"].([]any)
	if node == "" || len(regular) == 0 {
		return managedkubernetes.PodNodeWitness{}, denied
	}
	names := make(map[string]bool)
	for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
		entries, _ := spec[field].([]any)
		for _, entry := range entries {
			container, _ := entry.(map[string]any)
			name, _ := container["name"].(string)
			if name == "" || names[name] {
				return managedkubernetes.PodNodeWitness{}, denied
			}
			names[name] = true
		}
	}
	selected := requested
	if selected == "" {
		annotations, _ := metadata["annotations"].(map[string]any)
		annotated, _ := annotations["kubectl.kubernetes.io/default-container"].(string)
		if names[annotated] {
			selected = annotated
		} else {
			first, _ := regular[0].(map[string]any)
			selected, _ = first["name"].(string)
		}
	}
	if !names[selected] {
		return managedkubernetes.PodNodeWitness{}, denied
	}
	return managedkubernetes.PodNodeWitness{NodeName: node, Container: selected}, nil
}
