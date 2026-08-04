package eventconsumer

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/google/uuid"
)

func TestDecodeRejectsUnknownFieldAndAcceptsExactContract(t *testing.T) {
	resourceID := uuid.NewString()
	data, _ := json.Marshal(changeReference{ProjectID: uuid.NewString(), ResourceID: resourceID,
		ResourceKind: "RUNTIME_REVISION", ResourceState: "ACTIVE", ResourceVersion: 2})
	envelope := eventing.Envelope{EventID: uuid.NewString(), EventName: eventName, EventVersion: 1, SchemaVersion: 1,
		OccurredAt: time.Now().UTC().Truncate(time.Microsecond), AggregateType: "RUNTIME_REVISION",
		AggregateID: resourceID, AggregateVersion: 2, EventSequence: 2, CorrelationID: uuid.NewString(),
		OrganizationID: uuid.NewString(), Data: data}
	raw, _ := json.Marshal(envelope)
	if _, _, err := decode(raw); err != nil {
		t.Fatalf("exact event rejected: %v", err)
	}
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	object["unexpected"] = true
	raw, _ = json.Marshal(object)
	if _, _, err := decode(raw); err == nil {
		t.Fatal("unknown field was accepted")
	}
	exact, _ := json.Marshal(envelope)
	if _, _, err := decode(append(exact, []byte("trailing")...)); err == nil {
		t.Fatal("trailing invalid JSON was accepted")
	}
	envelope.Data = append(data, []byte("trailing")...)
	withTrailingData, _ := json.Marshal(envelope)
	if _, _, err := decode(withTrailingData); err == nil {
		t.Fatal("trailing invalid event data was accepted")
	}
	invalidState, _ := json.Marshal(changeReference{ProjectID: uuid.NewString(), ResourceID: resourceID,
		ResourceKind: "RUNTIME_REVISION", ResourceState: "UNKNOWN", ResourceVersion: 2})
	envelope.Data = invalidState
	withInvalidState, _ := json.Marshal(envelope)
	if _, _, err := decode(withInvalidState); err == nil {
		t.Fatal("unknown lifecycle state was accepted")
	}
}
