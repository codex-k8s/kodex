package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestKubernetesCommandChildKeepsExactGrantScope admits registered single-object recipes without granting discovery permission to retarget them.
// TestKubernetesCommandChildKeepsExactGrantScope допускает registered single-object recipes без права discovery менять их target.
func TestKubernetesCommandChildKeepsExactGrantScope(t *testing.T) {
	for _, test := range []struct {
		operation, group, resource, options string
		cluster                             bool
	}{
		{"kubernetes.set.image", "apps", "deployments", `{"assignments":{"app":"example.invalid/app:new"}}`, false},
		{"kubernetes.set.env", "apps", "deployments", `{"assignments":{"MODE":"new"}}`, false},
		{"kubernetes.set.resources", "apps", "deployments", `{"limits":{"cpu":"1"}}`, false},
		{"kubernetes.set.serviceaccount", "apps", "deployments", `{"serviceAccount":"default"}`, false},
		{"kubernetes.set.selector", "", "services", `{"selectorAssignments":{"app":"new"}}`, false},
		{"kubernetes.set.subject", "rbac.authorization.k8s.io", "rolebindings", `{"subjects":[{"kind":"User","name":"synthetic"}]}`, false},
		{"kubernetes.rollout.pause", "apps", "deployments", "", false},
		{"kubernetes.rollout.resume", "apps", "deployments", "", false},
		{"kubernetes.cordon", "", "nodes", "", true},
		{"kubernetes.uncordon", "", "nodes", "", true},
		{"kubernetes.taint", "", "nodes", `{"taints":[{"key":"workload","value":"test","effect":"NoSchedule"}]}`, true},
	} {
		t.Run(test.operation, func(t *testing.T) {
			namespace, scope := "example", "example"
			if test.cluster {
				namespace, scope = "", "@cluster"
			}
			address := managedkubernetes.ResourceAddress{Group: test.group, Version: "v1", Resource: test.resource, Namespace: namespace, Namespaced: !test.cluster, Name: "selected", UID: "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", ResourceVersion: "17"}
			input := managedkubernetes.OperationInput{Group: address.Group, Version: address.Version, Resource: address.Resource, Namespace: address.Namespace, Name: address.Name, UID: address.UID, ResourceVersion: address.ResourceVersion, Options: json.RawMessage(test.options)}
			raw, err := managedkubernetes.EncodeOperationInput(test.operation, input)
			if err != nil {
				t.Fatal(err)
			}
			parsed, canonical, err := managedkubernetes.DecodeOperationInput(test.operation, raw)
			if err != nil {
				t.Fatal(err)
			}
			grant, _ := json.Marshal(map[string]string{"target_ref": "target_fixture", "namespace": scope})
			owner := kubernetesOperationOwner{input: parsed, normalized: canonical, candidate: integrationInvocationCandidate{operation: test.operation, risk: "WRITE", approvalPolicy: "HUMAN_EACH_EFFECT", resourceScope: grant}}
			candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: address, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64)}
			child, err := kubernetesDirectChild(owner, candidate)
			if err != nil || child.Operation != test.operation || child.Resource != address || string(child.NormalizedInputJSON) != string(canonical) {
				t.Fatal("command changed owner input or lost admission", err)
			}
			for _, change := range []func(*managedkubernetes.ChildCandidate){
				func(c *managedkubernetes.ChildCandidate) { c.Resource.ResourceVersion = "18" },
				func(c *managedkubernetes.ChildCandidate) { c.Resource.UID = "dc7b5a7f-2421-41bf-bf3b-71f7b362fda9" },
				func(c *managedkubernetes.ChildCandidate) { c.Resource.Name = "foreign" },
				func(c *managedkubernetes.ChildCandidate) { c.Resource.Namespaced = !c.Resource.Namespaced },
			} {
				foreign := candidate
				change(&foreign)
				if _, err := kubernetesDirectChild(owner, foreign); err == nil {
					t.Fatal("foreign or stale observation admitted")
				}
			}
			owner.candidate.approvalPolicy = "NONE"
			if _, err := kubernetesDirectChild(owner, candidate); err == nil {
				t.Fatal("mutation became ungated")
			}
		})
	}
}

// TestKubernetesDirectChildPreservesInputAndObservation proves that discovery does not rewrite the
// original request, move its scope, or synthesize absent storage identity for create-only callers.
// TestKubernetesDirectChildPreservesInputAndObservation доказывает, что discovery не меняет original
// request/scope и не выдумывает absent storage identity для create-only caller.
func TestKubernetesDirectChildPreservesInputAndObservation(t *testing.T) {
	for _, test := range []struct {
		operation, raw, risk, policy string
		state                        managedkubernetes.ObservationState
	}{
		{"kubernetes.get", `{"group":"","version":"v1","resource":"configmaps","namespace":"example","name":"settings"}`, "READ", "NONE", managedkubernetes.ObservationPresent},
		{"kubernetes.create", `{"group":"","version":"v1","resource":"configmaps","namespace":"example","body":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"example"},"data":{"mode":"safe"}}}`, "WRITE", "HUMAN_SCOPED", managedkubernetes.ObservationCollection},
		{"kubernetes.run", `{"group":"","version":"v1","resource":"pods","namespace":"example","options":{"podName":"worker","image":"example.invalid/worker:fixture"}}`, "WRITE", "HUMAN_SCOPED", managedkubernetes.ObservationCollection},
	} {
		t.Run(test.operation, func(t *testing.T) {
			parsed, canonical, err := managedkubernetes.DecodeOperationInput(test.operation, []byte(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			owner := kubernetesOperationOwner{input: parsed, normalized: canonical, candidate: integrationInvocationCandidate{operation: test.operation, risk: test.risk, approvalPolicy: test.policy, resourceScope: []byte(`{"target_ref":"target_fixture","namespace":"example"}`)}}
			address := managedkubernetes.ResourceAddress{Group: "", Version: "v1", Resource: parsed.Resource, Namespace: "example", Namespaced: true}
			if test.state == managedkubernetes.ObservationPresent {
				address.Name = "settings"
				address.UID = "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8"
				address.ResourceVersion = "17"
			}
			candidate := managedkubernetes.ChildCandidate{ObservationState: test.state, Resource: address, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64)}
			child, err := kubernetesDirectChild(owner, candidate)
			if err != nil || child.Observation == nil || *child.Observation != candidate || string(child.NormalizedInputJSON) != string(canonical) {
				t.Fatal("discovery rewrote canonical input or lost original evidence", err)
			}
			if child.Operation != test.operation || child.Resource != address || !managedkubernetes.ValidDigest(child.InputDigest) {
				t.Fatal("child lost original operation or coordinates")
			}
			foreign := candidate
			foreign.Resource.Namespace = "foreign"
			if _, err := kubernetesDirectChild(owner, foreign); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("discovery moved the granted namespace", err)
			}
			damaged := candidate
			damaged.RegisteredSchemaDigest = ""
			if _, err := kubernetesDirectChild(owner, damaged); !errors.Is(err, errs.ErrInvalid) {
				t.Fatal("incomplete schema evidence admitted", err)
			}
			if test.state == managedkubernetes.ObservationCollection {
				fabricated := candidate
				fabricated.ObservationState = managedkubernetes.ObservationAbsent
				fabricated.Resource.Name = "settings"
				if _, err := kubernetesDirectChild(owner, fabricated); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("create-only request required or invented a named absence proof", err)
				}
			}
		})
	}
}

// TestKubernetesPlanDigestKeepsMutableAttemptsSeparate preserves plan hashes across completion
// while changing an immutable child input or proposal schema changes the plan's identity.
// TestKubernetesPlanDigestKeepsMutableAttemptsSeparate сохраняет plan hash после completion,
// но смена immutable child input либо proposal schema меняет plan identity.
func TestKubernetesPlanDigestKeepsMutableAttemptsSeparate(t *testing.T) {
	child := entity.ManagedKubernetesChildEffect{Ref: "kch_fixture_exact", Operation: "kubernetes.get", InputDigest: strings.Repeat("a", 64), State: "READY"}
	first, err := kubernetesPlanDigest(strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), []entity.ManagedKubernetesChildEffect{child})
	if err != nil {
		t.Fatal(err)
	}
	child.Observation = &managedkubernetes.ChildCandidate{ObservedResourceDigest: strings.Repeat("a", 64)}
	observed, err := kubernetesPlanDigest(strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), []entity.ManagedKubernetesChildEffect{child})
	if err != nil || observed == first {
		t.Fatal("original observation omitted from immutable plan", err)
	}
	first = observed
	child.State = "SUCCEEDED"
	same, err := kubernetesPlanDigest(strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), []entity.ManagedKubernetesChildEffect{child})
	if err != nil || same != first {
		t.Fatal("completion changed immutable plan", err)
	}
	different, err := kubernetesPlanDigest(strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("e", 64), []entity.ManagedKubernetesChildEffect{child})
	if err != nil || different == first {
		t.Fatal("changed proposal retained prior plan identity", err)
	}
}

// TestKubernetesStreamPlacementIsEnrichedOnlyByOwner binds a proposed Pod placement to the exact
// operator-attested node without GET Node authority, caller pin selection or mutation of proposal bytes.
// TestKubernetesStreamPlacementIsEnrichedOnlyByOwner связывает proposed Pod placement с exact
// operator-attested node без GET Node authority, caller pin selection либо изменения proposal bytes.
func TestKubernetesStreamPlacementIsEnrichedOnlyByOwner(t *testing.T) {
	registry := kubernetesOperatorComponentFixture(t)
	if len(registry.Nodes) == 0 || len(registry.Targets) == 0 {
		t.Fatal("missing operator node fixture")
	}
	pin := registry.Nodes[0].Transport
	raw := []byte(`{"group":"","version":"v1","resource":"pods","namespace":"example","name":"web","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","options":{"container":"main","command":["true"],"timeoutSeconds":20}}`)
	input, canonical, err := managedkubernetes.DecodeOperationInput("kubernetes.exec", raw)
	if err != nil {
		t.Fatal(err)
	}
	owner := kubernetesOperationOwner{input: input, normalized: canonical, candidate: integrationInvocationCandidate{operation: "kubernetes.exec", risk: "SENSITIVE", approvalPolicy: "HUMAN_EACH_EFFECT", resourceScope: []byte(`{"target_ref":"target_fixture","namespace":"example"}`)}}
	owner.selected.source.registry = registry
	owner.selected.target = registry.Targets[0].Target
	owner.selected.target.Ref, owner.selected.target.Revision = pin.TargetRef, pin.TargetRevision
	candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespace: "example", Name: "web", UID: input.UID, ResourceVersion: input.ResourceVersion, Namespaced: true}, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64), PodWitness: &managedkubernetes.PodNodeWitness{NodeName: pin.NodeName, Container: "main"}}
	child, err := kubernetesDirectChild(owner, candidate)
	if err != nil || child.Observation == nil || child.Observation.Validate() != nil || child.Observation.PodWitness.NodeUID != pin.NodeUID || child.NodeTransport == nil || child.NodeTransport.Ref != pin.Ref || candidate.PodWitness.NodeUID != "" {
		t.Fatal("owner node enrichment mutated proposal or missed the exact attestation", err)
	}
	foreign := candidate
	witness := *candidate.PodWitness
	witness.Container = "sidecar"
	foreign.PodWitness = &witness
	if _, err := kubernetesDirectChild(owner, foreign); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("placement changed the explicit selected container", err)
	}
	witness.Container = "main"
	witness.NodeUID = "40000000-0000-4000-8000-000000000001"
	if _, err := kubernetesDirectChild(owner, foreign); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("caller node UID overrode the operator attestation", err)
	}
}
