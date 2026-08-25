package exactkubernetessecret

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAggregateRejectsDigestMismatchAndGenerationRollback(t *testing.T) {
	t.Parallel()
	document := NewAggregate()
	document.Records["registered"] = Record{
		Version: 1, Status: RecordActive, Value: []byte("credential"),
		ContentSHA256: ValueSHA256([]byte("credential")),
	}
	raw, err := EncodeAggregate(document, 4)
	if err != nil {
		t.Fatal(err)
	}
	var tampered Aggregate
	if err := json.Unmarshal(raw, &tampered); err != nil {
		t.Fatal(err)
	}
	record := tampered.Records["registered"]
	record.Value = []byte("changed")
	tampered.Records["registered"] = record
	raw, err = json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAggregate(raw, 4); err == nil {
		t.Fatal("aggregate digest mismatch was accepted")
	}
	next := CloneAggregate(document)
	if err := ValidateForwardTransition(document, next); err == nil {
		t.Fatal("aggregate generation rollback was accepted")
	}
	next.Generation++
	if err := ValidateForwardTransition(document, next); err != nil {
		t.Fatalf("forward generation rejected: %v", err)
	}
}

func TestExactClientConfigAllowsOnlyBoundResourceShape(t *testing.T) {
	t.Parallel()
	if !validConfig(Config{ResourceName: "integration-gateway-provider-credentials", DataKey: "state.json", Timeout: 5 * time.Second}) {
		t.Fatal("exact aggregate resource configuration was rejected")
	}
	if validConfig(Config{ResourceName: "../secrets", DataKey: "state.json", Timeout: 5 * time.Second}) ||
		validConfig(Config{ResourceName: "registered", DataKey: "../token", Timeout: 5 * time.Second}) {
		t.Fatal("alternate resource or data key was accepted")
	}
}

func TestValidResourceNameUsesKubernetesDNSSubdomainBoundary(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{
		"internal-rpc-authority-automation-scheduler-issuer-readback-possession",
		"authority.snapshot",
		strings.Repeat("a", 253),
	} {
		if !validResourceName(valid) {
			t.Fatalf("validResourceName(%q) = false; want true", valid)
		}
	}
	for _, invalid := range []string{
		"",
		"authority..snapshot",
		"Authority.snapshot",
		"authority-.snapshot",
		strings.Repeat("a", 254),
	} {
		if validResourceName(invalid) {
			t.Fatalf("validResourceName(%q) = true; want false", invalid)
		}
	}
}
