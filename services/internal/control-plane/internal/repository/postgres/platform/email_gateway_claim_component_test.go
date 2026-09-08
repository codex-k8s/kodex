package platform

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	transport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/transport/grpc"
	"google.golang.org/protobuf/proto"
)

// Штатная disposable suite передаёт настоящий owner claim через production
// caster и protobuf в заранее собранный gateway test binary. Lease не продлевается.
func testEmailGatewayClaim(t *testing.T, item map[string]any, kind string) {
	t.Helper()
	if item["credential"] != nil {
		t.Fatal("managed email claim contains generic credential")
	}
	binary := os.Getenv("KODEX_EMAIL_GATEWAY_TEST_BINARY")
	if binary == "" {
		return
	}
	var message proto.Message
	if kind == "health" {
		message = transport.CastIntegrationConnectionTestClaim(item)
	} else {
		message = transport.CastIntegrationInvocationClaim(item)
	}
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestCPProducedEmailClaim$", "-test.timeout=15s")
	child.Env = append(os.Environ(), "KODEX_EMAIL_CLAIM_KIND="+kind)
	child.Stdin = bytes.NewReader(raw)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("gateway CP claim rejected: %v\n%s", err, output)
	}
}
