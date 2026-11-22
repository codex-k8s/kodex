package httptransport

import (
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// TestBrokerCredentialPublicProjectionUsesClosedMetadata exercises the real protobuf mapper;
// no Secret descriptor is manufactured from the broker reference and no private locator is emitted.
// TestBrokerCredentialPublicProjectionUsesClosedMetadata проверяет настоящий protobuf mapper;
// Secret descriptor из broker ref не создаётся, private locator не выдаётся.
func TestBrokerCredentialPublicProjectionUsesClosedMetadata(t *testing.T) {
	t.Parallel()
	view := personalCredentialHTTPFixture()
	view.CredentialDeliveryKind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY
	view.BrokerIdentityPurpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET
	view.BrokerIdentityRef, view.BrokerIdentityRevision = "bir_fixture01", 2
	view.CredentialConfigured = true
	view.State = cp.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_CHECK_REQUIRED
	body, err := ProtoMap(view)
	if err != nil {
		t.Fatal("metadata-only public projection failed", err)
	}
	delivery, ok := body["credentialDelivery"].(map[string]any)
	identity, identityOK := body["brokerIdentity"].(map[string]any)
	if !ok || !identityOK || len(delivery) != 2 || delivery["kind"] != "BROKER_IDENTITY" || delivery["brokerPurpose"] != "KUBERNETES_TARGET" ||
		len(identity) != 2 || identity["ref"] != "bir_fixture01" || identity["revision"] != float64(2) || body["bindingGeneration"] != float64(1) {
		t.Fatal("metadata-only public union lost purpose or revision")
	}
	for _, forbidden := range []string{"credentialRevision", "credentialDeliveryKind", "brokerIdentityPurpose", "brokerIdentityRef", "brokerIdentityRevision", "secretRef", "secretUid", "secretResourceVersion", "contentSha256"} {
		if _, exists := body[forbidden]; exists {
			t.Fatal("private or unnormalized broker field reached public output")
		}
	}
}

// TestBrokerCredentialPublicProjectionRejectsMixedAndPartialMetadata checks malformed wire
// metadata is rejected as a whole instead of choosing a token/shared fallback or guessing a ref.
// TestBrokerCredentialPublicProjectionRejectsMixedAndPartialMetadata проверяет полный отказ
// malformed wire metadata вместо выбора token/shared fallback либо угадывания ref.
func TestBrokerCredentialPublicProjectionRejectsMixedAndPartialMetadata(t *testing.T) {
	t.Parallel()
	for _, change := range []func(*cp.PersonalIntegrationCredentialView){
		func(value *cp.PersonalIntegrationCredentialView) {
			value.CredentialDeliveryKind = cp.IntegrationCredentialDeliveryKind(999)
		},
		func(value *cp.PersonalIntegrationCredentialView) {
			value.BrokerIdentityPurpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET
		},
		func(value *cp.PersonalIntegrationCredentialView) {
			value.BrokerIdentityRef = "bir_wrong_kind"
			value.BrokerIdentityRevision = 1
		},
		func(value *cp.PersonalIntegrationCredentialView) {
			value.CredentialDeliveryKind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY
		},
		func(value *cp.PersonalIntegrationCredentialView) { value.BindingGeneration = 0 },
	} {
		view := personalCredentialHTTPFixture()
		change(view)
		if _, err := ProtoMap(view); err == nil {
			t.Fatal("mixed or partial credential metadata reached public output")
		}
	}
	for _, connection := range []*cp.IntegrationConnection{
		{CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED, CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
			BrokerIdentityPurpose: cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET, CredentialsConfigured: true},
		{CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED, CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
			BrokerIdentityPurpose: cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET, BrokerIdentityRef: "bir_retained_shared", BrokerIdentityRevision: 1},
		{CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED, CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
			BrokerIdentityPurpose: cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET, BrokerIdentityRef: "bir_partial"},
		{CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED, CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
			BrokerIdentityPurpose: cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET, BrokerIdentityRef: "bir_fixture01", BrokerIdentityRevision: 1, CredentialRevision: &cp.IntegrationCredentialRevision{Ref: "icr_wrong_kind"}},
	} {
		if _, err := ProtoMap(connection); err == nil {
			t.Fatal("shared fallback or generic descriptor crossed broker metadata projection")
		}
	}
}

// TestUnknownCredentialDeliveryRemainsAbsent proves old/unknown metadata never means NONE or TOKEN.
// TestUnknownCredentialDeliveryRemainsAbsent доказывает, что old/unknown metadata не означает NONE либо TOKEN.
func TestUnknownCredentialDeliveryRemainsAbsent(t *testing.T) {
	t.Parallel()
	body, err := ProtoMap(&cp.IntegrationDefinition{Key: "github", Name: "fixture", Description: "fixture", Category: "development", BuiltIn: true, Available: true,
		Origin: cp.IntegrationDefinitionOrigin_INTEGRATION_DEFINITION_ORIGIN_SHIPPED, Digest: strings.Repeat("a", 64), Adapter: "GITHUB", AdapterOwner: "integration-gateway",
		ExecutionRoute: "MANAGED_MCP", AdapterReadiness: "READY", Version: 1, DefinitionVersion: "6.5.0", SchemaVersion: "integrations.kodex.io/v1"})
	if err != nil {
		t.Fatal("unknown metadata projection failed", err)
	}
	if _, exists := body["credentialDelivery"]; exists {
		t.Fatal("unknown metadata was converted into a credential representation")
	}
}

// TestSSHCredentialPublicProjectionRetainsOnlyPurposeMetadata checks the actual SSH enum spelling and shared reference projection without adding a generic credential descriptor or physical locator.
// TestSSHCredentialPublicProjectionRetainsOnlyPurposeMetadata проверяет actual SSH enum spelling и shared reference projection без generic credential descriptor либо physical locator.
func TestSSHCredentialPublicProjectionRetainsOnlyPurposeMetadata(t *testing.T) {
	value := &cp.IntegrationConnection{Ref: "int_fixture01", DefinitionKey: "ssh-managed", CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED, CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY, BrokerIdentityPurpose: cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET, BrokerIdentityRef: "bki_fixture01", BrokerIdentityRevision: 2, CredentialsConfigured: true}
	body, err := ProtoMap(value)
	if err != nil {
		t.Fatal("SSH metadata projection rejected", err)
	}
	delivery, ok := body["credentialDelivery"].(map[string]any)
	identity, identityOK := body["brokerIdentity"].(map[string]any)
	if !ok || !identityOK || len(delivery) != 2 || delivery["kind"] != "BROKER_IDENTITY" || delivery["brokerPurpose"] != "SSH_TARGET" || len(identity) != 2 || identity["ref"] != "bki_fixture01" || identity["revision"] != float64(2) {
		t.Fatal("SSH metadata crossed its closed purpose boundary")
	}
	for _, key := range []string{"credentialRevision", "secretRef", "secretUid", "secretResourceVersion", "contentSha256", "privateKey"} {
		if _, found := body[key]; found {
			t.Fatal("private locator reached public SSH metadata")
		}
	}
}
