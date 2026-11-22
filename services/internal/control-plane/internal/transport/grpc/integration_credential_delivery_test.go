package grpc

import (
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// personalBrokerTransportFixture creates metadata-only owner data with no private key or Secret locator.
// personalBrokerTransportFixture создаёт metadata-only owner data без private key либо Secret locator.
func personalBrokerTransportFixture(now time.Time) entity.PersonalIntegrationCredential {
	binding := personalCredentialTransportFixture(now)
	binding.Kind, binding.BrokerPurpose, binding.Credential = entity.IntegrationCredentialBrokerIdentity, entity.IntegrationBrokerIdentityKubernetes, nil
	binding.BrokerIdentity = &entity.IntegrationBrokerIdentityRevision{
		Ref: "bir_fixture01", TargetRef: "ktg_private_fixture", Revision: 1,
		Purpose: entity.IntegrationBrokerIdentityKubernetes, CreatedAt: now,
	}
	return binding
}

// TestPersonalBrokerRPCProjectionExcludesPrivateTransport proves the actual protobuf response
// carries only a self-owned metadata ref/revision and cannot be confused with a token descriptor.
// TestPersonalBrokerRPCProjectionExcludesPrivateTransport доказывает, что actual protobuf response
// содержит только self-owned metadata ref/revision без смешения с token descriptor.
func TestPersonalBrokerRPCProjectionExcludesPrivateTransport(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	binding := personalBrokerTransportFixture(now)
	view, err := castPersonalIntegrationCredential(binding, now)
	if err != nil || !view.GetCredentialConfigured() || view.GetCredentialDeliveryKind() != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY ||
		view.GetBrokerIdentityPurpose() != cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET ||
		view.GetBrokerIdentityRef() != binding.BrokerIdentity.Ref || view.GetBrokerIdentityRevision() != binding.BrokerIdentity.Revision || view.GetBindingGeneration() != binding.Generation {
		t.Fatalf("metadata-only personal projection failed: %v", err)
	}
	raw, err := protojson.Marshal(view)
	if err != nil {
		t.Fatal("encode synthetic broker metadata")
	}
	for _, forbidden := range []string{binding.OrganizationID, binding.SubjectID, binding.BrokerIdentity.TargetRef, "Secret", "secretRef", "privateKey", "kubeconfig"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("public broker metadata exposed a private transport coordinate")
		}
	}
	binding.State, binding.BrokerIdentity = "NOT_CONFIGURED", nil
	view, err = castPersonalIntegrationCredential(binding, now)
	if err != nil || view.GetCredentialConfigured() || view.GetBrokerIdentityRef() != "" || view.GetCredentialDeliveryKind() != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY {
		t.Fatal("missing identity was replaced by a fake token descriptor")
	}
	for _, change := range []func(*entity.PersonalIntegrationCredential){
		func(value *entity.PersonalIntegrationCredential) { value.Kind = "UNKNOWN" },
		func(value *entity.PersonalIntegrationCredential) { value.BrokerPurpose = "UNKNOWN" },
		func(value *entity.PersonalIntegrationCredential) {
			value.BrokerIdentity.Purpose = entity.IntegrationBrokerIdentitySSH
		},
		func(value *entity.PersonalIntegrationCredential) {
			value.Credential = personalCredentialTransportFixture(now).Credential
		},
		func(value *entity.PersonalIntegrationCredential) { value.Generation = 0 },
		func(value *entity.PersonalIntegrationCredential) { value.BrokerIdentity.Revision = 0 },
	} {
		invalid := personalBrokerTransportFixture(now)
		change(&invalid)
		if _, err := castPersonalIntegrationCredential(invalid, now); status.Code(err) != codes.Unavailable {
			t.Fatal("mixed or unknown broker metadata reached a public response")
		}
	}
}

// TestConnectionBrokerProjectionDoesNotSelectRetainedSharedIdentity checks explicit shared
// metadata, hides it under personal policy and withholds all descriptors for unknown/mixed kinds.
// TestConnectionBrokerProjectionDoesNotSelectRetainedSharedIdentity проверяет explicit shared
// metadata, скрывает её в personal policy и не выдаёт descriptors для unknown/mixed kinds.
func TestConnectionBrokerProjectionDoesNotSelectRetainedSharedIdentity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	binding := personalBrokerTransportFixture(now)
	connection := entity.IntegrationConnection{CredentialPolicy: entity.IntegrationCredentialProjectShared, BrokerIdentity: binding.BrokerIdentity,
		CredentialDelivery: integrationpackage.CredentialDelivery{Kind: integrationpackage.CredentialDeliveryBrokerIdentity, BrokerPurpose: string(binding.BrokerPurpose)}}
	view := castConnection(connection)
	if view.GetCredentialRevision() != nil || view.GetBrokerIdentityRef() != binding.BrokerIdentity.Ref || view.GetBrokerIdentityRevision() != 1 {
		t.Fatal("shared broker identity was serialized as a generic descriptor")
	}
	connection.CredentialPolicy = entity.IntegrationCredentialInitiatorRequired
	view = castConnection(connection)
	if view.GetCredentialRevision() != nil || view.GetBrokerIdentityRef() != "" || view.GetBrokerIdentityRevision() != 0 || view.GetCredentialsConfigured() {
		t.Fatal("personal policy disclosed or selected a retained shared identity")
	}
	connection.CredentialPolicy = entity.IntegrationCredentialProjectShared
	connection.CredentialRevision = personalCredentialTransportFixture(now).Credential
	view = castConnection(connection)
	if view.GetCredentialRevision() != nil || view.GetBrokerIdentityRef() != "" || view.GetCredentialDeliveryKind() != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_UNSPECIFIED {
		t.Fatal("mixed shared representations returned credential metadata")
	}
	connection.CredentialDelivery, connection.BrokerIdentity = integrationpackage.CredentialDelivery{}, nil
	view = castConnection(connection)
	if view.GetCredentialRevision() != nil || view.GetCredentialDeliveryKind() != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_UNSPECIFIED {
		t.Fatal("unknown delivery kind fell back to a generic descriptor")
	}
}

// TestRetainedReceiptWithoutKindRemainsUnknownMetadata preserves its prior safe view without
// rewriting stored receipt bytes or manufacturing current execution/setup authority.
// TestRetainedReceiptWithoutKindRemainsUnknownMetadata сохраняет прежнее safe view без
// переписывания stored receipt bytes либо создания current execution/setup authority.
func TestRetainedReceiptWithoutKindRemainsUnknownMetadata(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	retained := personalCredentialTransportFixture(now)
	retained.Kind = ""
	view, err := castPersonalIntegrationCredential(retained, now)
	if err != nil || !view.GetCredentialConfigured() || view.GetCredentialDeliveryKind() != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_UNSPECIFIED ||
		view.GetBindingGeneration() != 0 || view.GetBrokerIdentityRef() != "" || view.GetBrokerIdentityRevision() != 0 || retained.Kind != "" {
		t.Fatal("retained metadata was rewritten or given an inferred token kind")
	}
}
