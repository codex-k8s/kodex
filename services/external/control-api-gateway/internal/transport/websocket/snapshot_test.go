package websockettransport

import (
	"encoding/json"
	"testing"

	httpgenerated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
)

func TestSnapshotItemsMarshalExactlyOneProjection(t *testing.T) {
	raw, err := json.Marshal(SnapshotItems{Resources: make([]httpgenerated.Resource, 0)})
	if err != nil || string(raw) != `{"resources":[]}` {
		t.Fatalf("empty authoritative resource snapshot is ambiguous: %s, %v", raw, err)
	}
	if _, err := json.Marshal(SnapshotItems{
		Resources: make([]httpgenerated.Resource, 0),
		Incidents: make([]httpgenerated.IncidentView, 0),
	}); err == nil {
		t.Fatal("multi-projection snapshot was accepted")
	}
}

func TestSnapshotItemsRejectStaleOwnerProjectionVersion(t *testing.T) {
	if _, err := json.Marshal(SnapshotItems{Runs: []httpgenerated.RunView{{
		State:   httpgenerated.LifecycleStateACTIVE,
		Version: 0,
	}}}); err == nil {
		t.Fatal("stale Run projection version was accepted")
	}
	if _, err := json.Marshal(SnapshotItems{Runs: []httpgenerated.RunView{{
		State:       httpgenerated.LifecycleStateACTIVE,
		Version:     1,
		NextActions: []httpgenerated.RunNextAction{httpgenerated.RunNextAction("FUTURE_ACTION")},
	}}}); err == nil {
		t.Fatal("unknown authoritative Run action was accepted")
	}
}
