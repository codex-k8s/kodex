package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

// Только мост test profile: старый producer передаёт свою исходную owner map,
// typed поля восстанавливаются без изменения значений перед production caster.
func TestLegacyEmailClaimCasterFixture(t *testing.T) {
	if os.Getenv("KODEX_EMAIL_LEGACY_CLAIM") != "1" {
		t.Skip("legacy CP producer entrypoint required")
	}
	binary := os.Getenv("KODEX_EMAIL_GATEWAY_TEST_BINARY")
	if binary == "" {
		t.Fatal("gateway fixture binary required")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 256<<10))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	var item map[string]any
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &item) != nil {
		t.Fatal("owner map invalid")
	}
	var generation int64
	if json.Unmarshal(fields["generation"], &generation) != nil {
		t.Fatal("generation invalid")
	}
	item["generation"] = generation
	var expires time.Time
	var credential entity.IntegrationCredentialRevision
	var definition []byte
	if json.Unmarshal(fields["expiresAt"], &expires) != nil || json.Unmarshal(fields["credential"], &credential) != nil || json.Unmarshal(fields["definitionPackage"], &definition) != nil || credential.Ref == "" {
		t.Fatal("legacy owner fields invalid")
	}
	item["expiresAt"], item["credential"], item["definitionPackage"] = expires, credential, definition
	var claim proto.Message
	if os.Getenv("KODEX_EMAIL_CLAIM_KIND") == "health" {
		claim = CastIntegrationConnectionTestClaim(item)
	} else {
		var scope map[string]string
		if json.Unmarshal(fields["resourceScope"], &scope) != nil {
			t.Fatal("scope invalid")
		}
		item["resourceScope"] = scope
		claim = CastIntegrationInvocationClaim(item)
	}
	encoded, err := proto.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestCPProducedEmailClaim$", "-test.timeout=12s")
	child.Stdin = bytes.NewReader(encoded)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("gateway rejected old owner claim: %v\n%s", err, output)
	}
}
