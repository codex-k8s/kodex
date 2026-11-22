package kubernetestarget

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// observedPodFixture supplies a credential-free Pod with two ordinary containers and exact placement.
// observedPodFixture создаёт Pod без credentials, с двумя обычными containers и exact placement.
func observedPodFixture() string {
	return `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"selected","namespace":"example","uid":"11111111-1111-4111-8111-111111111111","resourceVersion":"10"},"spec":{"nodeName":"node-one","automountServiceAccountToken":false,"containers":[{"name":"first","image":"example.invalid/image:fixture"},{"name":"second","image":"example.invalid/image:fixture"}]}}`
}

// TestObservePodStreamUsesPrivateNativeRead verifies TLS discovery, exact Pod policy and no node/effect I/O.
// The result is a proposal with an empty node UID, never a final stream grant.
// TestObservePodStreamUsesPrivateNativeRead проверяет TLS discovery, exact Pod policy и отсутствие node/effect I/O.
// Результат содержит пустой UID узла и является proposal, не окончательным stream grant.
func TestObservePodStreamUsesPrivateNativeRead(t *testing.T) {
	for _, variant := range []string{"default", "explicit", "annotation", "stale annotation", "attach", "port-forward", "missing container", "terminating", "host access", "service token", "unscheduled", "duplicate container", "denied", "changed UID", "changed version", "missing pins", "wrong kind", "unknown operation", "unknown option"} {
		t.Run(variant, func(t *testing.T) {
			body := observedPodFixture()
			input := managedkubernetes.OperationInput{Version: "v1", Resource: "pods", Namespace: "example", Name: "selected", UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "10", Options: json.RawMessage(`{"command":["true"],"timeoutSeconds":30}`)}
			selected := "first"
			operation := "kubernetes.exec"
			success := variant == "default" || variant == "explicit" || variant == "annotation" || variant == "stale annotation" || variant == "attach" || variant == "port-forward"
			switch variant {
			case "attach":
				operation, input.Options = "kubernetes.attach", json.RawMessage(`{"timeoutSeconds":30}`)
			case "port-forward":
				operation, input.Options = "kubernetes.port-forward", json.RawMessage(`{"ports":[{"port":8080,"targetPort":8080}],"timeoutSeconds":30}`)
			case "explicit":
				input.Options = json.RawMessage(`{"command":["true"],"timeoutSeconds":30,"container":"second"}`)
				selected = "second"
			case "annotation":
				body = strings.Replace(body, `"resourceVersion":"10"`, `"resourceVersion":"10","annotations":{"kubectl.kubernetes.io/default-container":"second"}`, 1)
				selected = "second"
			case "stale annotation":
				body = strings.Replace(body, `"resourceVersion":"10"`, `"resourceVersion":"10","annotations":{"kubectl.kubernetes.io/default-container":"missing"}`, 1)
			case "missing container":
				input.Options = json.RawMessage(`{"command":["true"],"timeoutSeconds":30,"container":"missing"}`)
			case "terminating":
				body = strings.Replace(body, `"resourceVersion":"10"`, `"resourceVersion":"10","deletionTimestamp":"2026-01-01T00:00:00Z"`, 1)
			case "host access":
				body = strings.Replace(body, `"nodeName":`, `"hostNetwork":true,"nodeName":`, 1)
			case "service token":
				body = strings.Replace(body, `"automountServiceAccountToken":false`, `"automountServiceAccountToken":true`, 1)
			case "unscheduled":
				body = strings.Replace(body, `"node-one"`, `""`, 1)
			case "duplicate container":
				body = strings.Replace(body, `"name":"second"`, `"name":"first"`, 1)
			case "changed UID":
				input.UID = "21111111-1111-4111-8111-111111111111"
			case "changed version":
				input.ResourceVersion = "11"
			case "missing pins":
				input.UID, input.ResourceVersion = "", ""
			case "wrong kind":
				input.Resource = "deployments"
			case "unknown operation":
				operation = "kubernetes.arbitrary"
			case "unknown option":
				input.Options = json.RawMessage(`{"command":["true"],"timeoutSeconds":30,"url":"https://example.invalid"}`)
			}
			wire := &resourceWireFixture{podBody: body}
			wire.deny.Store(variant == "denied")
			credential := resourceTLSFixture(t, wire)
			candidate, err := credential.ObservePodStream(t.Context(), operation, input, subjectPin(t), nil)
			if wire.effects.Load() != 0 {
				t.Fatal("observation started a remote effect")
			}
			if !success {
				if err == nil || candidate.PodWitness != nil {
					t.Fatal("invalid or unauthorized Pod produced a proposal")
				}
				if (variant == "missing pins" || variant == "wrong kind" || variant == "unknown operation" || variant == "unknown option") && wire.reads.Load() != 0 {
					t.Fatal("invalid input reached a resource read")
				}
				return
			}
			if err != nil || wire.reads.Load() != 1 || candidate.ValidateProposal() != nil || candidate.Validate() == nil ||
				candidate.PodWitness.NodeUID != "" || candidate.PodWitness.NodeName != "node-one" || candidate.PodWitness.Container != selected {
				t.Fatal("private Pod observation lost its exact proposal boundary")
			}
		})
	}
}
