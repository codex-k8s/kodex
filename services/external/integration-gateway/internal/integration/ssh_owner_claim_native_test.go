package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// TestSSHActualOwnerClaimConsumer consumes real PostgreSQL owner output after its production mapper and runs the actual gateway purpose preparation in a separate bounded process; it proves this producer/consumer seam, not protected transport or policy109.
// TestSSHActualOwnerClaimConsumer принимает real PostgreSQL owner output после production mapper и запускает actual gateway purpose preparation в отдельном bounded process; доказывается producer/consumer seam, не protected transport либо policy109.
func TestSSHActualOwnerClaimConsumer(t *testing.T) {
	instance := os.Getenv("KODEX_SSH_NATIVE_CLAIM_INSTANCE")
	if instance == "" {
		t.Skip("actual SSH owner claim fixture is not configured")
	}
	if !managedssh.OwnerReference(instance) {
		t.Fatal("actual SSH owner instance is invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, (512<<10)+1))
	if err != nil || len(raw) == 0 || len(raw) > 512<<10 {
		t.Fatal("actual SSH owner claim frame is invalid")
	}
	defer clear(raw)
	claim := &cp.IntegrationInvocationClaim{}
	if grpcserver.StrictProtoCodec().Unmarshal(raw, claim) != nil {
		t.Fatal("actual SSH owner claim wire is invalid")
	}
	adapter, err := New(Config{CredentialDirectory: t.TempDir(), ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080",
		OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080", Timeout: time.Second})
	if err != nil {
		t.Fatal("create actual SSH gateway adapter")
	}
	prepared, err := adapter.PrepareSSHInvocation(claim, instance)
	if err != nil || prepared == nil {
		t.Fatal("actual owner claim was not admitted by the SSH purpose consumer")
	}
	expected := managedssh.Binding{WorkloadInstance: instance, InvocationRef: claim.InvocationRef, LeaseRef: claim.Lease.Ref,
		Fence: claim.Lease.Fence, Generation: claim.Lease.Generation, InputDigest: claim.InputDigest}
	if prepared.Binding() != expected || !prepared.ExpiresAt().Equal(claim.Lease.ExpiresAt.AsTime()) {
		t.Fatal("SSH gateway changed the actual owner binding")
	}
	receipt := os.NewFile(4, "native-claim-receipt")
	if receipt == nil {
		t.Fatal("native SSH claim receipt descriptor is missing")
	}
	digest := sha256.Sum256(raw)
	if _, err := receipt.WriteString("SSH_CLAIM_VALIDATED " + hex.EncodeToString(digest[:]) + "\n"); err != nil {
		_ = receipt.Close()
		t.Fatal("native SSH claim receipt write failed")
	}
	if receipt.Close() != nil {
		t.Fatal("native SSH claim receipt close failed")
	}
}
