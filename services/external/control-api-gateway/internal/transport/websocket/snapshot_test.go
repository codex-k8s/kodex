package websockettransport

import (
	"encoding/json"
	"testing"

	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/websocket/generated"
)

func TestSnapshotItemsMarshalExactlyOneProjection(t *testing.T) {
	raw, err := json.Marshal(generated.SnapshotItems{Resources: make([]generated.AnonymousSchema_15, 0)})
	if err != nil || string(raw) != `{"resources":[]}` {
		t.Fatalf("empty authoritative resource snapshot is ambiguous: %s, %v", raw, err)
	}
	if _, err := json.Marshal(generated.SnapshotItems{
		Resources: make([]generated.AnonymousSchema_15, 0),
		Incidents: make([]generated.AnonymousSchema_182, 0),
	}); err == nil {
		t.Fatal("multi-projection snapshot was accepted")
	}
}
