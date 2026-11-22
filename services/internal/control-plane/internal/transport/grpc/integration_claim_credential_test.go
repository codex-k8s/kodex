package grpc

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// TestInvocationClaimCredentialDeliveryPreservesOwnerMetadata exercises actual Proto roundtrip for the existing claim, without creating another selector or a generic descriptor for BI.
// TestInvocationClaimCredentialDeliveryPreservesOwnerMetadata проверяет actual Proto roundtrip existing claim без второго selector либо generic descriptor для BI.
func TestInvocationClaimCredentialDeliveryPreservesOwnerMetadata(t *testing.T) {
	for _, source := range []struct{ kind, purpose string }{{"NONE", ""}, {"TOKEN", ""}, {"BROKER_IDENTITY", "SSH_TARGET"}, {"BROKER_IDENTITY", "KUBERNETES_TARGET"}} {
		t.Run(source.kind+source.purpose, func(t *testing.T) {
			values := map[string]any{"credentialDeliveryKind": source.kind, "invocationRef": "inv_original", "connectionRef": "int_original", "inputDigest": "original-owner-input-digest"}
			if source.purpose != "" {
				values["brokerIdentityPurpose"], values["brokerIdentityRef"], values["brokerIdentityRevision"] = source.purpose, "bir_original", int64(7)
			}
			claim := CastIntegrationInvocationClaim(values)
			raw, err := proto.Marshal(claim)
			if err != nil {
				t.Fatal(err)
			}
			var received cp.IntegrationInvocationClaim
			if proto.Unmarshal(raw, &received) != nil || !proto.Equal(claim, &received) || received.CredentialDeliveryKind.String() != "INTEGRATION_CREDENTIAL_DELIVERY_KIND_"+source.kind || received.InvocationRef != "inv_original" || received.ConnectionRef != "int_original" || received.InputDigest != "original-owner-input-digest" || received.CredentialRevision != nil {
				t.Fatal("existing owner claim changed identity or credential representation on the wire")
			}
			if source.purpose != "" && (received.BrokerIdentityPurpose.String() != "INTEGRATION_BROKER_IDENTITY_PURPOSE_"+source.purpose || received.BrokerIdentityRef != "bir_original" || received.BrokerIdentityRevision != 7) {
				t.Fatal("BI claim lost immutable owner pins")
			}
		})
	}
}

// TestClaimUnknownPurposeCannotBecomeOrdinaryCredential retains invalid future or malformed purpose values even alongside TOKEN/NONE, so consumers cannot lose a mixed representation during projection.
// TestClaimUnknownPurposeCannotBecomeOrdinaryCredential сохраняет invalid future либо malformed purpose даже рядом с TOKEN/NONE; consumer не теряет mixed representation при проекции.
func TestClaimUnknownPurposeCannotBecomeOrdinaryCredential(t *testing.T) {
	for _, kind := range []string{"NONE", "TOKEN", "BROKER_IDENTITY"} {
		for _, invalid := range []any{"FUTURE_PURPOSE", "UNSPECIFIED", int64(1), nil} {
			values := map[string]any{"credentialDeliveryKind": kind, "brokerIdentityPurpose": invalid}
			claims := []proto.Message{CastIntegrationConnectionTestClaim(values), CastIntegrationInvocationClaim(values)}
			for _, message := range claims {
				raw, err := proto.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				copy := message.ProtoReflect().New().Interface()
				if proto.Unmarshal(raw, copy) != nil || !proto.Equal(message, copy) {
					t.Fatal("unknown enum was normalized on the wire")
				}
				field := copy.ProtoReflect().Descriptor().Fields().ByName("broker_identity_purpose")
				if field == nil || copy.ProtoReflect().Get(field).Enum() != -1 {
					t.Fatal("malformed purpose disappeared into ordinary credential delivery")
				}
			}
		}
	}
	for _, values := range []map[string]any{nil, {"brokerIdentityPurpose": ""}} {
		if castIntegrationClaimBrokerPurpose(values) != 0 {
			t.Fatal("absent purpose acquired a transport")
		}
	}
}
