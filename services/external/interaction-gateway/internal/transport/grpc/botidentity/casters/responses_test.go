package casters

import (
	"bytes"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestIdentityViewRedactsProviderAndCredentialEvidence(t *testing.T) {
	t.Parallel()
	identity := entity.AgentMattermostBotIdentity{
		IdentityRef: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Selector: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ProviderBotID: "raw-provider-bot", ProviderUserID: "raw-provider-user", ProviderTeamID: "raw-provider-team",
		ProviderTokenID: "raw-provider-token", CredentialBindingID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		CredentialSecretRef: "vault://private/path", CredentialSHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Username: "agent-bot", DisplayName: "Agent bot", Status: enum.AgentBotIdentityAvailable,
		ProviderVersion: 2, ProviderGeneration: 3,
		ProviderSnapshotSHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	raw, err := protojson.Marshal(IdentityView(identity))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte("raw-provider"), []byte("vault://"), []byte(identity.CredentialSHA256),
		[]byte(identity.CredentialBindingID)} {
		if bytes.Contains(raw, forbidden) {
			t.Fatalf("safe identity view contains private evidence: %s", raw)
		}
	}
}

func TestOperationViewNormalizesUnknownFailure(t *testing.T) {
	t.Parallel()
	view := OperationView(entity.AgentMattermostBotOperation{FailureCode: "provider body: private"})
	if view.GetFailureCode() != "SAFE_FAILURE" {
		t.Fatalf("raw provider failure escaped: %q", view.GetFailureCode())
	}
}
