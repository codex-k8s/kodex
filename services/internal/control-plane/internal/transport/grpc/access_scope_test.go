package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
	"reflect"
	"testing"
)

func TestPublicAccessScopeRPCPreservesQueryShape(t *testing.T) {
	for _, tc := range []struct {
		name   string
		wire   *cp.AccessScope
		domain entity.AccessScope
	}{
		{"organization", &cp.AccessScope{Kind: cp.AccessScopeKind_ACCESS_SCOPE_KIND_ORGANIZATION}, entity.AccessScope{Kind: "ORGANIZATION"}},
		{"project", &cp.AccessScope{Kind: cp.AccessScopeKind_ACCESS_SCOPE_KIND_PROJECT, ProjectRef: "prj_fixture"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: "prj_fixture"}},
		{"resource kind", &cp.AccessScope{Kind: cp.AccessScopeKind_ACCESS_SCOPE_KIND_RESOURCE_KIND, ProjectRef: "prj_fixture", ResourceKind: cp.AccessResourceKind_ACCESS_RESOURCE_KIND_ROLE_IMAGE}, entity.AccessScope{Kind: "RESOURCE_KIND", ProjectRef: "prj_fixture", ResourceKind: "ROLE_IMAGE"}},
		{"resource instance", &cp.AccessScope{Kind: cp.AccessScopeKind_ACCESS_SCOPE_KIND_RESOURCE_INSTANCE, ProjectRef: "prj_fixture", ResourceKind: cp.AccessResourceKind_ACCESS_RESOURCE_KIND_PROJECT, ResourceRef: "prj_fixture"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: "prj_fixture", ResourceKind: "PROJECT", ResourceRef: "prj_fixture"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(domainAccessScope(tc.wire), tc.domain) {
				t.Fatal("query scope changed at domain input")
			}
			if !proto.Equal(castAccessScope(tc.domain), tc.wire) {
				t.Fatal("public response target changed at RPC output")
			}
		})
	}
}
