package grpc

import (
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

func TestEmailReceiptProjection(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	for _, outcome := range []string{"UNKNOWN_OUTCOME", "EFFECT_CONFIRMED", "NO_EFFECT_CONFIRMED"} {
		t.Run(outcome, func(t *testing.T) {
			source := entity.EmailEffectReceipt{Ref: "emrc_fixture", Version: 2, InvocationRef: "inv_fixture", ExternalReceiptRef: strings.Repeat("a", 32),
				ExternalReceiptDigest: strings.Repeat("b", 64), SemanticInputDigest: strings.Repeat("c", 64), EffectKey: "opaque:key/with spaces",
				Outcome: outcome, MailboxRef: "mailbox", ConfigurationRevision: 3, ConnectionRef: "connection", ProjectRef: "project", CreatedAt: now, UpdatedAt: now}
			wire := castEmailReceipt(source)
			if emailOutcomeFromProto(wire.Outcome) != outcome || wire.Outcome == cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNSPECIFIED ||
				wire.ProjectRef != source.ProjectRef || wire.ExternalReceiptDigest != source.ExternalReceiptDigest || wire.EffectKey != source.EffectKey {
				t.Fatal("email receipt projection lost exact identity or outcome")
			}
			data, err := proto.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			var decoded cp.EmailEffectReceipt
			if err := proto.Unmarshal(data, &decoded); err != nil || !proto.Equal(wire, &decoded) {
				t.Fatalf("email receipt wire roundtrip: %v", err)
			}
		})
	}
	if castEmailDecision(nil) != nil || emailOutcomeFromProto(cp.EmailEffectOutcome(99)) != "" {
		t.Fatal("absent decision or unknown outcome projected as valid")
	}
}
