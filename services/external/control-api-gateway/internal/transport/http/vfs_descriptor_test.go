package httptransport

import (
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func TestVFSForwardsOwnerFiltersWithoutLocalFiltering(t *testing.T) {
	client := &catalogRPCRecorder{response: &cp.ListVFSNodesResponse{Total: 43}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/vfs/nodes?path=/projects&query=literal&lifecycleState=DELETED&kinds=INPUT&kinds=SKILL", nil))
	q, ok := client.request.(*cp.ListVFSNodesRequest)
	if w.Code != 200 || !ok || q.Query != "literal" || q.LifecycleState != "DELETED" || q.Path != "/projects" || len(q.Kinds) != 2 || q.Kinds[1] != cp.VFSNodeKind_VFS_NODE_KIND_SKILL {
		t.Fatalf("filters lost: %d %+v", w.Code, q)
	}
	for _, suffix := range []string{"?kinds=UNKNOWN", "?kinds=INPUT&kinds=INPUT", "?lifecycleState=UNKNOWN", "?path=/../private"} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/vfs/nodes"+suffix, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("invalid filter reached owner: %s %d", suffix, w.Code)
		}
	}
}

func TestVFSDescriptorRejectsUnknownAndContradictoryAuthority(t *testing.T) {
	fixture := func() *cp.VFSNode {
		return &cp.VFSNode{Version: 4, Revision: 2, RevisionRef: "arv_fixture01", LifecycleState: "DELETED", ScanState: "CLEAN", ResourceKind: "ARTIFACT", Selectable: true, SelectionReason: "AVAILABLE", NextActions: []string{"RESTORE", "PURGE"}}
	}
	var output generated.VFSNode
	if !vfsDescriptor(fixture(), &output) || output.Version != 4 || output.Revision != 2 || !output.Selectable || len(output.NextActions) != 2 {
		t.Fatal("owner descriptor lost")
	}
	for name, mutate := range map[string]func(*cp.VFSNode){
		"unknown lifecycle":       func(v *cp.VFSNode) { v.LifecycleState = "UNKNOWN" },
		"unknown scan":            func(v *cp.VFSNode) { v.ScanState = "UNKNOWN" },
		"unknown kind":            func(v *cp.VFSNode) { v.ResourceKind = "UNKNOWN" },
		"unknown reason":          func(v *cp.VFSNode) { v.SelectionReason = "UNKNOWN" },
		"unknown action":          func(v *cp.VFSNode) { v.NextActions = []string{"EXECUTE"} },
		"duplicate action":        func(v *cp.VFSNode) { v.NextActions = []string{"PURGE", "PURGE"} },
		"contradictory selection": func(v *cp.VFSNode) { v.Selectable = false },
		"unsafe version":          func(v *cp.VFSNode) { v.Version = maximumSafeJSONInteger + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			v := fixture()
			mutate(v)
			if vfsDescriptor(v, &output) {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
}
