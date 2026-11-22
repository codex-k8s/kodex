package grpc

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// kubernetesChildFixture builds an owner child with canonical input and independent observation pins.
// kubernetesChildFixture создаёт дочерний план с каноническим вводом и отдельным наблюдением владельца.
func kubernetesChildFixture(t *testing.T) entity.ManagedKubernetesChildEffect {
	t.Helper()
	_, canonical, err := managedkubernetes.DecodeOperationInput("kubernetes.get", []byte(`{"group":"","version":"v1","resource":"configmaps","namespace":"synthetic","name":"example"}`))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	resource := managedkubernetes.ResourceAddress{Version: "v1", Resource: "configmaps", Namespace: "synthetic", Name: "example", Namespaced: true, UID: "20000000-0000-4000-8000-000000000001", ResourceVersion: "7"}
	return entity.ManagedKubernetesChildEffect{Ref: "child_synthetic", Operation: "kubernetes.get", InputDigest: hex.EncodeToString(digest[:]), NormalizedInputJSON: canonical, Resource: resource, State: "READY", Observation: &managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: resource, ObservedResourceDigest: strings.Repeat("d", 64), RegisteredSchemaDigest: strings.Repeat("e", 64), NamespaceUID: "30000000-0000-4000-8000-000000000001"}}
}

// TestManagedKubernetesChildPinsSurviveWire copies all persisted evidence without reconstructing or aliasing it.
// TestManagedKubernetesChildPinsSurviveWire копирует все закреплённые данные без восстановления и общей памяти.
func TestManagedKubernetesChildPinsSurviveWire(t *testing.T) {
	child := kubernetesChildFixture(t)
	wire := managedKubernetesChildToProto(child)
	if wire == nil || wire.Observation == nil || wire.Observation.GetObservedResourceDigest() != child.Observation.ObservedResourceDigest || wire.Observation.GetRegisteredSchemaDigest() != child.Observation.RegisteredSchemaDigest || wire.Observation.GetNamespaceUid() != child.Observation.NamespaceUID || wire.Observation.GetResource().GetUid() != child.Resource.UID || wire.Observation.GetObservationState().String() != "MANAGED_KUBERNETES_OBSERVATION_STATE_PRESENT" {
		t.Fatal("owner observation lost on wire")
	}
	child.NormalizedInputJSON[0] = '!'
	child.Observation.Resource.Name = "foreign"
	child.Observation.RegisteredSchemaDigest = ""
	if wire.NormalizedInputJson[0] != '{' || wire.Observation.Resource.Name != "example" || wire.Observation.RegisteredSchemaDigest == "" {
		t.Fatal("wire observation aliases owner memory")
	}
}

// TestManagedKubernetesChildRejectsMissingEvidence closes malformed owner replies before any broker admission.
// TestManagedKubernetesChildRejectsMissingEvidence отклоняет повреждённый ответ до допуска брокером.
func TestManagedKubernetesChildRejectsMissingEvidence(t *testing.T) {
	for _, variant := range []string{"missing", "schema", "body", "state", "resource", "input-resource", "noncanonical", "input-digest", "unknown-operation", "namespace"} {
		t.Run(variant, func(t *testing.T) {
			child := kubernetesChildFixture(t)
			switch variant {
			case "missing":
				child.Observation = nil
			case "schema":
				child.Observation.RegisteredSchemaDigest = ""
			case "body":
				child.Observation.ObservedResourceDigest = ""
			case "state":
				child.Observation.ObservationState = "UNKNOWN"
			case "resource":
				child.Observation.Resource.Name = "foreign"
			case "input-resource":
				child.Resource.Name = "foreign"
				child.Observation.Resource = child.Resource
			case "noncanonical":
				child.NormalizedInputJSON = append([]byte(" "), child.NormalizedInputJSON...)
			case "input-digest":
				child.InputDigest = strings.Repeat("0", 64)
			case "unknown-operation":
				child.Operation = "kubernetes.unknown"
			case "namespace":
				child.Observation.NamespaceUID = "invalid"
			}
			if managedKubernetesChildToProto(child) != nil {
				t.Fatal("malformed owner observation serialized")
			}
		})
	}
}

// TestManagedKubernetesChildAddresslessIsClosed admits registered metadata commands without fabricating a resource.
// TestManagedKubernetesChildAddresslessIsClosed допускает команды метаданных из реестра без выдуманного ресурса.
func TestManagedKubernetesChildAddresslessIsClosed(t *testing.T) {
	for _, operation := range []string{"kubernetes.help", "kubernetes.version"} {
		t.Run(operation, func(t *testing.T) {
			_, canonical, err := managedkubernetes.DecodeOperationInput(operation, []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(canonical)
			child := entity.ManagedKubernetesChildEffect{Ref: "child_synthetic", Operation: operation, InputDigest: hex.EncodeToString(digest[:]), NormalizedInputJSON: canonical, State: "READY"}
			wire := managedKubernetesChildToProto(child)
			if wire == nil || wire.Resource != nil || wire.Observation != nil {
				t.Fatal("addressless command fabricated or lost")
			}
			child.Operation = "kubernetes.unknown"
			if managedKubernetesChildToProto(child) != nil {
				t.Fatal("unknown command borrowed addressless exception")
			}
		})
	}
}
