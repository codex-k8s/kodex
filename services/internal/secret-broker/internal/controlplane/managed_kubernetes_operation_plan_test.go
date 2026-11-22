package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// planClientFixture captures the protected owner proposal without implementing owner derivation or storage.
// planClientFixture фиксирует protected owner proposal без реализации owner derivation либо storage.
type planClientFixture struct {
	frameClientFixture
	response *cp.PrepareManagedKubernetesChildrenResponse
	request  *cp.PrepareManagedKubernetesChildrenRequest
	calls    int
}

// PrepareManagedKubernetesChildren returns controlled wire data and never retries a lost response.
// PrepareManagedKubernetesChildren возвращает controlled wire data без повторения lost response.
func (client *planClientFixture) PrepareManagedKubernetesChildren(_ context.Context, request *cp.PrepareManagedKubernetesChildrenRequest, _ ...grpc.CallOption) (*cp.PrepareManagedKubernetesChildrenResponse, error) {
	client.calls++
	client.request = request
	return client.response, client.err
}

// operationPlanFixture starts from a real adapter-resolved synthetic snapshot and concrete observation.
// operationPlanFixture начинает с synthetic snapshot через настоящий adapter и concrete observation.
func operationPlanFixture(t *testing.T) (*Owner, *planClientFixture, managedkubernetes.Binding, value.KubernetesOperationSnapshot, []managedkubernetes.ChildCandidate) {
	t.Helper()
	owner, resolver, binding := operationResolveFixture(t)
	snapshot, err := owner.ResolveKubernetesOperation(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	address := *snapshot.Resource
	address.UID, address.ResourceVersion = "20000000-0000-4000-8000-000000000001", "7"
	candidates := []managedkubernetes.ChildCandidate{{ObservationState: managedkubernetes.ObservationPresent, Resource: address, ObservedResourceDigest: strings.Repeat("d", 64), RegisteredSchemaDigest: strings.Repeat("e", 64)}}
	digest := sha256.Sum256(snapshot.NormalizedInput)
	client := &planClientFixture{frameClientFixture: resolver.frameClientFixture, response: &cp.PrepareManagedKubernetesChildrenResponse{PlanRef: "plan_synthetic", PlanRevision: 1, PlanDigest: strings.Repeat("f", 64), Children: []*cp.ManagedKubernetesChildEffect{{ChildRef: "child_synthetic", Operation: snapshot.Operation, InputDigest: hex.EncodeToString(digest[:]), Resource: kubernetesResourceToProto(address), NormalizedInputJson: append([]byte(nil), snapshot.NormalizedInput...), State: "READY"}}}}
	client.response.Children[0].Observation = &cp.ManagedKubernetesChildCandidate{Resource: kubernetesResourceToProto(address), ObservationState: cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_PRESENT, ObservedResourceDigest: candidates[0].ObservedResourceDigest, RegisteredSchemaDigest: candidates[0].RegisteredSchemaDigest}
	owner.client.KubernetesOperations = client
	return owner, client, binding, snapshot, candidates
}

// TestOperationPlanPreservesClaimProposalAndCopies proves observed pins reach one RPC unchanged.
// TestOperationPlanPreservesClaimProposalAndCopies доказывает передачу observed pins в один неизменённый RPC.
func TestOperationPlanPreservesClaimProposalAndCopies(t *testing.T) {
	owner, client, binding, snapshot, candidates := operationPlanFixture(t)
	plan, err := owner.PrepareKubernetesChildren(t.Context(), binding, snapshot, candidates)
	proposal, _ := managedkubernetes.ChildProposalDigest(snapshot.Digest, candidates)
	if err != nil || len(plan.Children) != 1 || client.calls != 1 || plan.Children[0].State != "READY" || !proto.Equal(client.request.Binding, kubernetesHealthBindingToProto(binding)) || client.request.SnapshotDigest != snapshot.Digest || client.request.ProposalDigest != proposal || !proto.Equal(client.request.Candidates[0].Resource, kubernetesResourceToProto(candidates[0].Resource)) {
		t.Fatal("plan changed claim or observations")
	}
	client.response.Children[0].NormalizedInputJson[0] = '!'
	client.response.Children[0].Resource.Name = "foreign"
	client.response.Children[0].Observation.Resource.Name = "foreign"
	client.response.Children[0].Observation.RegisteredSchemaDigest = ""
	if plan.Children[0].NormalizedInput[0] != '{' || plan.Children[0].Resource.Name != "example" || plan.Children[0].Observation == nil || plan.Children[0].Observation.Resource.Name != "example" || plan.Children[0].Observation.RegisteredSchemaDigest != candidates[0].RegisteredSchemaDigest {
		t.Fatal("wire mutation changed immutable child")
	}
	plan.Children[0].Input.Body = json.RawMessage(`{"private":"synthetic-business-value"}`)
	plan.Children[0].NormalizedInput = []byte(`{"private":"synthetic-business-value"}`)
	raw, err := json.Marshal(plan)
	if err != nil || strings.Contains(string(raw), "synthetic-business-value") || strings.Contains(fmt.Sprintf("%v %#v", plan.Children[0], plan.Children[0]), "synthetic-business-value") {
		t.Fatal("private child body escaped diagnostics")
	}
}

// TestOperationPlanRejectsMalformedOwnerReplies rejects changed scope and malformed plans before returning any child.
// TestOperationPlanRejectsMalformedOwnerReplies отклоняет changed scope и malformed plans до выдачи child.
func TestOperationPlanRejectsMalformedOwnerReplies(t *testing.T) {
	for _, scenario := range []string{"nil", "lost", "unknown", "ref", "revision", "digest", "too-many", "nil-child", "child-unknown", "child-ref", "duplicate", "operation", "input", "noncanonical", "input-digest", "state", "resource-nil", "resource-unknown", "resource-scope", "resource-name", "resource-version", "all-namespaces", "node-on-resource", "observation-missing", "observation-unknown", "observation-resource-unknown", "observation-state", "observation-schema", "observation-resource", "observation-witness", "existing-ref", "existing-revision", "existing-digest"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, snapshot, candidates := operationPlanFixture(t)
			child := client.response.Children[0]
			switch scenario {
			case "nil":
				client.response = nil
			case "lost":
				client.err = errors.New("synthetic lost plan response")
			case "unknown":
				client.response.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "ref":
				client.response.PlanRef = ""
			case "revision":
				client.response.PlanRevision = 0
			case "digest":
				client.response.PlanDigest = ""
			case "too-many":
				for range maximumKubernetesPlanChildren {
					client.response.Children = append(client.response.Children, child)
				}
			case "nil-child":
				client.response.Children[0] = nil
			case "child-unknown":
				child.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "child-ref":
				child.ChildRef = ""
			case "duplicate":
				client.response.Children = append(client.response.Children, proto.Clone(child).(*cp.ManagedKubernetesChildEffect))
			case "operation":
				child.Operation = "kubernetes.unknown"
			case "input":
				child.NormalizedInputJson = []byte(`{"x":1,"x":2}`)
			case "noncanonical":
				child.NormalizedInputJson = append([]byte(" "), child.NormalizedInputJson...)
			case "input-digest":
				child.InputDigest = strings.Repeat("0", 64)
			case "state":
				child.State = "COMPLETED"
			case "resource-nil":
				child.Resource = nil
			case "resource-unknown":
				child.Resource.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "resource-scope":
				child.Resource.Namespaced = false
			case "resource-name":
				child.Resource.Name = "foreign"
			case "resource-version":
				child.Resource.Version = "v2"
			case "all-namespaces":
				child.Resource.AllNamespaces = true
			case "observation-missing":
				child.Observation = nil
			case "observation-unknown":
				child.Observation.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "observation-resource-unknown":
				child.Observation.Resource.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "observation-state":
				child.Observation.ObservationState = cp.ManagedKubernetesObservationState(99)
			case "observation-schema":
				child.Observation.RegisteredSchemaDigest = ""
			case "observation-resource":
				child.Observation.Resource.Name = "foreign"
			case "observation-witness":
				child.Observation.PodWitness = &cp.ManagedKubernetesPodNodeWitness{}
				child.Observation.PodWitness.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "node-on-resource":
				child.NodeTransport = &cp.ManagedKubernetesNodeTransportSnapshot{}
			case "existing-ref", "existing-revision", "existing-digest":
				snapshot.PlanRef, snapshot.PlanRevision, snapshot.PlanDigest = client.response.PlanRef, client.response.PlanRevision, client.response.PlanDigest
				if scenario == "existing-ref" {
					client.response.PlanRef = "plan_foreign"
				}
				if scenario == "existing-revision" {
					client.response.PlanRevision++
				}
				if scenario == "existing-digest" {
					client.response.PlanDigest = strings.Repeat("0", 64)
				}
			}
			plan, err := owner.PrepareKubernetesChildren(t.Context(), binding, snapshot, candidates)
			if err == nil || plan.Ref != "" || len(plan.Children) != 0 || client.calls != 1 {
				t.Fatal("malformed owner plan escaped or request retried")
			}
		})
	}
}

// TestOperationPlanRejectsInvalidProposalsBeforeRPC covers local malformed evidence without an external owner attempt.
// TestOperationPlanRejectsInvalidProposalsBeforeRPC проверяет local malformed evidence без external owner attempt.
func TestOperationPlanRejectsInvalidProposalsBeforeRPC(t *testing.T) {
	for _, scenario := range []string{"snapshot", "expired", "target", "candidate", "duplicate", "health", "plan-pin", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, snapshot, candidates := operationPlanFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "snapshot":
				snapshot.Digest = ""
			case "expired":
				snapshot.ExpiresAt = time.Now().Add(-time.Second)
			case "target":
				snapshot.Target.Revision = 0
			case "candidate":
				candidates[0].ObservationState = "UNKNOWN"
			case "duplicate":
				candidates = append(candidates, candidates[0])
			case "health":
				binding.ConnectionTestRef = "health_synthetic"
				binding.InvocationRef = ""
			case "plan-pin":
				snapshot.PlanDigest = strings.Repeat("f", 64)
			case "cancel":
				cancel()
			}
			if _, err := owner.PrepareKubernetesChildren(ctx, binding, snapshot, candidates); err == nil || client.calls != 0 {
				t.Fatal("invalid proposal reached owner RPC")
			}
		})
	}
}

// TestOperationPlanPreservesOwnerOrderAndClosedStates keeps dependent ordering and valid terminal readbacks.
// TestOperationPlanPreservesOwnerOrderAndClosedStates сохраняет dependent ordering и valid terminal readbacks.
func TestOperationPlanPreservesOwnerOrderAndClosedStates(t *testing.T) {
	owner, client, binding, snapshot, candidates := operationPlanFixture(t)
	states := []string{"WAITING_OWNER", "WAITING_PREDECESSOR", "READY", "ATTEMPTING", "SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED"}
	first := client.response.Children[0]
	client.response.Children = nil
	for i, state := range states {
		child := proto.Clone(first).(*cp.ManagedKubernetesChildEffect)
		child.ChildRef = fmt.Sprintf("child_%d", len(states)-i)
		child.State = state
		client.response.Children = append(client.response.Children, child)
	}
	plan, err := owner.PrepareKubernetesChildren(t.Context(), binding, snapshot, candidates)
	if err != nil || len(plan.Children) != len(states) {
		t.Fatal("valid plan readback rejected")
	}
	for i, child := range plan.Children {
		if child.Ref != fmt.Sprintf("child_%d", len(states)-i) || child.State != states[i] {
			t.Fatal("owner order changed")
		}
	}
	client.response.Children = nil
	if plan, err = owner.PrepareKubernetesChildren(t.Context(), binding, snapshot, nil); err != nil || len(plan.Children) != 0 {
		t.Fatal("empty owner plan was fabricated or rejected")
	}
}

// TestOperationPlanObservationMappings preserves each exact observation rather than fabricating a PRESENT object.
// TestOperationPlanObservationMappings сохраняет exact observation без создания вымышленного PRESENT object.
func TestOperationPlanObservationMappings(t *testing.T) {
	for _, state := range []managedkubernetes.ObservationState{managedkubernetes.ObservationPresent, managedkubernetes.ObservationAbsent, managedkubernetes.ObservationCollection, managedkubernetes.ObservationUnversionedRead} {
		t.Run(string(state), func(t *testing.T) {
			owner, client, binding, snapshot, candidates := operationPlanFixture(t)
			candidate := &candidates[0]
			candidate.ObservationState = state
			if state != managedkubernetes.ObservationPresent {
				candidate.Resource.UID, candidate.Resource.ResourceVersion = "", ""
			}
			if state == managedkubernetes.ObservationCollection {
				candidate.Resource.Name = ""
			}
			if _, err := owner.PrepareKubernetesChildren(t.Context(), binding, snapshot, candidates); err != nil || client.request.Candidates[0].ObservationState.String() != "MANAGED_KUBERNETES_OBSERVATION_STATE_"+string(state) {
				t.Fatal("observation state changed")
			}
		})
	}
}

// TestOperationPlanHistoricalNodePinsDoNotRenewCredit permits expired metadata only on terminal readback.
// TestOperationPlanHistoricalNodePinsDoNotRenewCredit допускает expired metadata только для terminal readback.
func TestOperationPlanHistoricalNodePinsDoNotRenewCredit(t *testing.T) {
	for _, state := range []string{"WAITING_OWNER", "WAITING_PREDECESSOR", "READY", "ATTEMPTING", "SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED"} {
		t.Run(state, func(t *testing.T) {
			owner, client, binding, snapshot, candidates := operationPlanFixture(t)
			child := client.response.Children[0]
			child.Operation, child.State = "kubernetes.exec", state
			_, canonical, err := managedkubernetes.DecodeOperationInput(child.Operation, []byte(`{"group":"","version":"v1","resource":"pods","namespace":"synthetic","name":"example","uid":"20000000-0000-4000-8000-000000000001","resourceVersion":"7","options":{"command":["true"],"timeoutSeconds":10}}`))
			if err != nil {
				t.Fatal(err)
			}
			child.NormalizedInputJson = canonical
			digest := sha256.Sum256(canonical)
			child.InputDigest = hex.EncodeToString(digest[:])
			child.Resource.Resource = "pods"
			ca := []byte("synthetic-node-metadata-not-a-certificate")
			caHash := sha256.Sum256(ca)
			child.NodeTransport = &cp.ManagedKubernetesNodeTransportSnapshot{PinRef: "node_pin_synthetic", Generation: 1, TargetRef: snapshot.Target.ID, TargetRevision: int64(snapshot.Target.Revision), NodeName: "synthetic-node", NodeUid: "2fbad8aa-1b5d-4f34-a2b6-60793435c6a3", ServerOrigin: "https://node.example.invalid:10250", TlsServerName: "node.example.invalid", CertificateAuthorityPem: ca, CertificateAuthoritySha256: hex.EncodeToString(caHash[:]), ServerPublicKeySha256: strings.Repeat("e", 64), AddressIps: []string{"192.0.2.11"}, NotBefore: timestamppb.New(time.Now().Add(-2 * time.Hour)), ExpiresAt: timestamppb.New(time.Now().Add(-time.Hour)), Protocol: string(managedkubernetes.WebSocketV5)}
			child.Observation.Resource = proto.Clone(child.Resource).(*cp.ManagedKubernetesResourceAddress)
			child.Observation.PodWitness = &cp.ManagedKubernetesPodNodeWitness{NodeName: child.NodeTransport.NodeName, NodeUid: child.NodeTransport.NodeUid, Container: "app"}
			plan, err := owner.PrepareKubernetesChildren(t.Context(), binding, snapshot, candidates)
			terminal := state == "SUCCEEDED" || state == "FAILED" || state == "UNKNOWN" || state == "CANCELLED"
			if terminal {
				if err != nil || len(plan.Children) != 1 || plan.Children[0].NodeTransport == nil || plan.Children[0].NodeTransport.ExpiresAt.After(time.Now()) {
					t.Fatal("historical pin lost or expiry renewed")
				}
				child.NodeTransport.TargetRef = "target_foreign"
				if _, err = owner.PrepareKubernetesChildren(t.Context(), binding, snapshot, candidates); err == nil {
					t.Fatal("historical readback weakened target binding")
				}
			} else if err == nil || len(plan.Children) != 0 {
				t.Fatal("unfinished child accepted expired node pin")
			}
		})
	}
}

// TestOperationPlanAddresslessKeepsClosedRegistry allows only registered addressless commands without observation pins.
// TestOperationPlanAddresslessKeepsClosedRegistry допускает только зарегистрированные безадресные команды без наблюдения.
func TestOperationPlanAddresslessKeepsClosedRegistry(t *testing.T) {
	for _, operation := range []string{"kubernetes.help", "kubernetes.version"} {
		t.Run(operation, func(t *testing.T) {
			_, canonical, err := managedkubernetes.DecodeOperationInput(operation, []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(canonical)
			wire := &cp.ManagedKubernetesChildEffect{ChildRef: "child_synthetic", Operation: operation, InputDigest: hex.EncodeToString(digest[:]), NormalizedInputJson: canonical, State: "READY"}
			child, ok := kubernetesOperationChild(wire, value.KubernetesTarget{})
			if !ok || child.Observation != nil || child.Resource != (managedkubernetes.ResourceAddress{}) {
				t.Fatal("addressless command fabricated or lost")
			}
			wire.Observation = &cp.ManagedKubernetesChildCandidate{}
			if _, ok := kubernetesOperationChild(wire, value.KubernetesTarget{}); ok {
				t.Fatal("addressless command accepted inconsistent evidence")
			}
			wire.Observation = nil
			wire.Operation = "kubernetes.unknown"
			if _, ok := kubernetesOperationChild(wire, value.KubernetesTarget{}); ok {
				t.Fatal("unknown command borrowed addressless exception")
			}
		})
	}
}
