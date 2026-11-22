package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	ownergrpc "github.com/codex-k8s/kodex/services/internal/control-plane/internal/transport/grpc"
	"google.golang.org/protobuf/proto"
)

// assertKubernetesOwnerPlanWire passes the actual owner plan through the production CP mapper and
// an obligatory independent broker decoder binary. It never substitutes a synthetic response.
// assertKubernetesOwnerPlanWire передаёт actual owner plan через production CP mapper и обязательный
// independent broker decoder binary. Synthetic response вместо actual output не используется.
func assertKubernetesOwnerPlanWire(t *testing.T, ctx context.Context, plan entity.ManagedKubernetesPlan) {
	t.Helper()
	response := ownergrpc.ManagedKubernetesPlanToProto(plan)
	if response == nil {
		t.Fatal("actual owner plan failed the RPC mapper")
	}
	raw, err := proto.Marshal(response)
	if err != nil {
		t.Fatal("encode actual owner plan response")
	}
	binary := os.Getenv("KODEX_KUBERNETES_NATIVE_PLAN_CONSUMER_BINARY")
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatal("native plan consumer binary is required")
	}
	directory := t.TempDir()
	if os.Chmod(directory, 0o700) != nil {
		t.Fatal("protect native owner plan fixture")
	}
	path := filepath.Join(directory, "plan.pb")
	if os.WriteFile(path, raw, 0o600) != nil {
		t.Fatal("write private native owner plan fixture")
	}
	childContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	consumer := exec.CommandContext(childContext, binary, "-test.run=^TestKubernetesNativeOwnerPlanWire$", "-test.count=1", "-test.timeout=10s")
	consumer.Env = []string{"KODEX_KUBERNETES_NATIVE_PLAN_WIRE=" + path, "TMPDIR=" + directory}
	if consumer.Run() != nil {
		t.Fatal("actual owner plan failed independent broker wire consumer")
	}
}
