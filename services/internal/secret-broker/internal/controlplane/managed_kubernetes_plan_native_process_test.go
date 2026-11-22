package controlplane

import (
	"os"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/protobuf/proto"
)

// TestKubernetesNativeOwnerPlanWire consumes actual PostgreSQL owner output after the production
// CP mapper in an independent process. It proves the closed wire decoder, not mTLS or policy108.
// TestKubernetesNativeOwnerPlanWire принимает actual PostgreSQL owner output после production CP
// mapper в independent process. Доказывает closed wire decoder, не mTLS либо policy108.
func TestKubernetesNativeOwnerPlanWire(t *testing.T) {
	path := os.Getenv("KODEX_KUBERNETES_NATIVE_PLAN_WIRE")
	if path == "" {
		t.Skip("native owner wire fixture is not configured")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() < 1 || info.Size() > 1<<20 {
		t.Fatal("invalid private native plan fixture")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read native plan fixture")
	}
	var response cp.PrepareManagedKubernetesChildrenResponse
	if proto.Unmarshal(raw, &response) != nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(response.Children) == 0 {
		t.Fatal("invalid native owner plan response")
	}
	for _, child := range response.Children {
		parsed, ok := kubernetesOperationChild(child, value.KubernetesTarget{})
		metadata := integrationpackage.KubernetesMetadataRead(parsed.Operation)
		if !ok || (parsed.Observation == nil) != metadata || parsed.Ref != child.ChildRef || parsed.State != child.State || parsed.InputDigest != child.InputDigest {
			t.Fatal("actual native owner child was rejected by broker decoder")
		}
	}
}
