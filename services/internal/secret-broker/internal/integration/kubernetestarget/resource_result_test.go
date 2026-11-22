package kubernetestarget

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// resultPreparationFixture supplies an owned compiled schema and exact observed object pins.
// resultPreparationFixture даёт owned compiled schema и exact observed object pins.
func resultPreparationFixture(t *testing.T, operation string) *PreparedResourceOperation {
	t.Helper()
	registered, err := compileResourceSchemaFixture(resourceSchemaFixture(t))
	if err != nil {
		t.Fatal("compile owned result fixture")
	}
	classification, _ := integrationpackage.KubernetesOperationByKey(operation)
	address := ResourceAddress{Version: "v1", Resource: "configmaps", Namespace: "example", Namespaced: true, Name: "selected", UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "10"}
	return &PreparedResourceOperation{schema: registered, operation: classification, address: address, observation: managedkubernetes.ResourceObservation{State: managedkubernetes.ObservationPresent, Resource: address, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: registered.digest}}
}

// resourceDeepProjectionFixture fits provider/schema bounds but exceeds the strict owner depth.
// resourceDeepProjectionFixture укладывается в provider/schema bounds, превышая strict owner depth.
func resourceDeepProjectionFixture() map[string]any {
	object := map[string]any{"private-business": "synthetic-deep-value"}
	for range 24 {
		object = map[string]any{"next": object}
	}
	return object
}

// TestResourceHTTPSDeepCRDProjectionCannotBecomeSuccess proves actual discovery/GET accepts
// the source privately, while the real owner result validator blocks incompatible emitted bytes.
// TestResourceHTTPSDeepCRDProjectionCannotBecomeSuccess доказывает actual discovery/GET private
// admission source и отказ real owner result validator для несовместимых outgoing bytes.
func TestResourceHTTPSDeepCRDProjectionCannotBecomeSuccess(t *testing.T) {
	state := &resourceWireFixture{deepCRD: true}
	credential := resourceTLSFixture(t, state)
	input := managedkubernetes.OperationInput{Group: "data.example", Version: "v1", Resource: "widgets", Namespace: "example", Name: "selected"}
	address := ResourceAddress{Group: input.Group, Version: input.Version, Resource: input.Resource, Namespace: input.Namespace, Name: input.Name, Namespaced: true}
	prepared, err := credential.PrepareResourceOperation(context.Background(), "kubernetes.get", input, address, subjectPin(t), nil)
	if err != nil {
		t.Fatalf("deep source fixture failed before actual projection: %v", err)
	}
	defer prepared.Close()
	result, err := prepared.Execute(context.Background(), time.Now().Add(5*time.Second))
	if err == nil || result.Outcome != "FAILED" || result.ReasonCode != managedkubernetes.ReasonResponseInvalid || len(result.SafeJSON) != 0 || !result.Attempted || state.reads.Load() != 2 {
		t.Fatal("deep source became SUCCEEDED before an incompatible owner Complete")
	}
}

// TestResourceInvalidActualProjectionPreservesOutcomeAndClearsOwnedBytes covers both READ and
// already-attempted write at the shared confirmation boundary, with no raw result fallback.
// TestResourceInvalidActualProjectionPreservesOutcomeAndClearsOwnedBytes покрывает READ и
// already-attempted write на shared confirmation boundary без raw result fallback.
func TestResourceInvalidActualProjectionPreservesOutcomeAndClearsOwnedBytes(t *testing.T) {
	for _, outcome := range []string{"FAILED", "UNKNOWN"} {
		safe, _ := json.Marshal(resourceDeepProjectionFixture())
		result, err := confirmResourceProjection(value.ResourceOperationResult{Outcome: outcome, Attempted: true}, safe, "", "")
		if err == nil || result.Outcome != outcome || result.ReasonCode != managedkubernetes.ReasonResponseInvalid || len(result.SafeJSON) != 0 || strings.Trim(string(safe), "\x00") != "" {
			t.Fatal("invalid projection lost uncertainty, retained owned bytes or became success")
		}
	}
}

// resultObjectFixture removes only the separately tested synthetic status extension.
// resultObjectFixture удаляет лишь отдельно проверяемое synthetic status extension.
func resultObjectFixture(revision string) map[string]any {
	object, _ := decodeResourceJSON([]byte(resourceWireObject(revision)), resourceResponseBytes, 64)
	delete(object, "status")
	return object
}

// TestResourceReadUsesCurrentRVWithoutInventingWriteCAS distinguishes a latest same-UID READ
// from an explicit caller RV pin and rejects replacement objects even without an explicit pin.
// TestResourceReadUsesCurrentRVWithoutInventingWriteCAS отличает latest same-UID READ от
// explicit caller RV pin и отклоняет replacement object даже без explicit pin.
func TestResourceReadUsesCurrentRVWithoutInventingWriteCAS(t *testing.T) {
	prepared := resultPreparationFixture(t, "kubernetes.get")
	raw, _ := json.Marshal(resultObjectFixture("11"))
	if _, uid, rv, err := prepared.projectResourceResponse(raw); err != nil || uid != prepared.address.UID || rv != "11" {
		t.Fatal("ordinary current READ inherited a stale write CAS")
	}
	prepared.input.ResourceVersion = "10"
	if _, _, _, err := prepared.projectResourceResponse(raw); err == nil {
		t.Fatal("explicit READ version pin silently widened to current")
	}
	prepared.input.ResourceVersion = ""
	raw = []byte(strings.ReplaceAll(string(raw), prepared.address.UID, "22222222-2222-4222-8222-222222222222"))
	if _, _, _, err := prepared.projectResourceResponse(raw); err == nil {
		t.Fatal("named READ returned a replacement UID")
	}
}

// TestResourceListFiltersProtectedScopesAndRejectsMalformedMetadata preserves continuation
// while excluding private items; untyped metadata cannot become returned safe JSON.
// TestResourceListFiltersProtectedScopesAndRejectsMalformedMetadata сохраняет continuation,
// исключая private items; untyped metadata не становятся выдаваемым safe JSON.
func TestResourceListFiltersProtectedScopesAndRejectsMalformedMetadata(t *testing.T) {
	prepared := resultPreparationFixture(t, "kubernetes.list.all-namespaces")
	prepared.address.Name, prepared.address.UID, prepared.address.ResourceVersion, prepared.address.Namespace, prepared.address.AllNamespaces = "", "", "", "", true
	prepared.protected = []string{"private"}
	first, private := resultObjectFixture("11"), resultObjectFixture("12")
	private["metadata"].(map[string]any)["namespace"] = "private"
	private["data"].(map[string]any)["private-business"] = "private-namespace-output"
	object := map[string]any{"apiVersion": "v1", "kind": "ConfigMapList", "metadata": map[string]any{"resourceVersion": "12", "continue": "next"}, "items": []any{first, private}}
	raw, _ := json.Marshal(object)
	safe, _, _, err := prepared.projectResourceResponse(raw)
	if err != nil || strings.Contains(string(safe), "private-namespace-output") || !strings.Contains(string(safe), `"continue":"next"`) {
		t.Fatal("list lost continuation or leaked a protected namespace")
	}
	object["metadata"].(map[string]any)["resourceVersion"] = map[string]any{"unexpected": "value"}
	raw, _ = json.Marshal(object)
	if safe, _, _, err := prepared.projectResourceResponse(raw); err == nil || len(safe) != 0 {
		t.Fatal("malformed list metadata escaped result validation")
	}
}
