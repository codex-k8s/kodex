package grpc

import (
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestRuntimeQueueViewPreservesOptionalPresence checks the actual Proto JSON boundary including EmitUnpopulated consumers.
// Absent, terminal and unknown reasons never serialize an empty or unsupported enum value into the HTTP/WebSocket Run view.
// TestRuntimeQueueViewPreservesOptionalPresence проверяет настоящую Proto JSON границу, включая EmitUnpopulated consumers.
// Отсутствующая, terminal и неизвестная причина не сериализуются в пустой/неподдержанный enum HTTP/WebSocket Run.
func TestRuntimeQueueViewPreservesOptionalPresence(t *testing.T) {
	for _, test := range []struct {
		state, reason string
		present       bool
	}{
		{"QUEUED", "WAITING_RESOURCES", true},
		{"QUEUED", "REQUEST_EXCEEDS_CAPACITY", true},
		{"RUNNING", "", false},
		{"CANCELLED", "WAITING_RESOURCES", false},
		{"SUCCEEDED", "REQUEST_EXCEEDS_CAPACITY", false},
		{"QUEUED", "UNKNOWN_PRIVATE_DIAGNOSTIC", false},
	} {
		raw, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(castRun(entity.Run{State: test.state, QueueReason: test.reason}))
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if json.Unmarshal(raw, &value) != nil {
			t.Fatal("invalid Run JSON")
		}
		reason, present := value["queueReason"]
		if present != test.present || present && reason != test.reason {
			t.Fatal("queue reason presence violated public optional enum contract")
		}
	}
}
