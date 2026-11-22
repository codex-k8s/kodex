package integrationpackage

import (
	"encoding/json"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestCredentialDeliveryRejectsMixedOrUnregisteredPurposes checks the package boundary that
// separates generic token/password material from metadata-only purpose-specific broker identities.
// TestCredentialDeliveryRejectsMixedOrUnregisteredPurposes проверяет package boundary между
// generic token/password material и metadata-only purpose-specific broker identities.
func TestCredentialDeliveryRejectsMixedOrUnregisteredPurposes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, adapter string
		credential    *Credential
		kind          CredentialDeliveryKind
		purpose       string
		valid         bool
	}{
		{"none", "GITHUB", nil, CredentialDeliveryNone, "", true},
		{"token", "GITHUB", &Credential{Kind: "TOKEN", SecretKey: "token"}, CredentialDeliveryToken, "", true},
		{"password", "SYNTHETIC_HTTP", &Credential{Kind: "PASSWORD", SecretKey: "password"}, CredentialDeliveryToken, "", true},
		{"kubernetes", "KUBERNETES", &Credential{Kind: "BROKER_IDENTITY", BrokerPurpose: "KUBERNETES_TARGET"}, CredentialDeliveryBrokerIdentity, "KUBERNETES_TARGET", true},
		{"ssh", "SSH_TARGET", &Credential{Kind: "BROKER_IDENTITY", BrokerPurpose: "SSH_TARGET"}, CredentialDeliveryBrokerIdentity, "SSH_TARGET", true},
		{"kubernetes-nil", "KUBERNETES", nil, "", "", false},
		{"kubernetes-token", "KUBERNETES", &Credential{Kind: "TOKEN", SecretKey: "token"}, "", "", false},
		{"kubernetes-password", "KUBERNETES", &Credential{Kind: "PASSWORD", SecretKey: "password"}, "", "", false},
		{"ssh-nil", "SSH_TARGET", nil, "", "", false},
		{"ssh-token", "SSH_TARGET", &Credential{Kind: "TOKEN", SecretKey: "token"}, "", "", false},
		{"ssh-password", "SSH_TARGET", &Credential{Kind: "PASSWORD", SecretKey: "password"}, "", "", false},
		{"unknown-kind", "GITHUB", &Credential{Kind: "UNKNOWN", SecretKey: "token"}, "", "", false},
		{"missing-kind", "GITHUB", &Credential{SecretKey: "token"}, "", "", false},
		{"token-without-key", "GITHUB", &Credential{Kind: "TOKEN"}, "", "", false},
		{"token-with-purpose", "GITHUB", &Credential{Kind: "TOKEN", SecretKey: "token", BrokerPurpose: "SSH_TARGET"}, "", "", false},
		{"broker-with-key", "KUBERNETES", &Credential{Kind: "BROKER_IDENTITY", SecretKey: "not_a_broker_credential", BrokerPurpose: "KUBERNETES_TARGET"}, "", "", false},
		{"broker-missing-purpose", "KUBERNETES", &Credential{Kind: "BROKER_IDENTITY"}, "", "", false},
		{"unknown-purpose", "KUBERNETES", &Credential{Kind: "BROKER_IDENTITY", BrokerPurpose: "GENERIC_SECRET"}, "", "", false},
		{"cross-purpose", "KUBERNETES", &Credential{Kind: "BROKER_IDENTITY", BrokerPurpose: "SSH_TARGET"}, "", "", false},
		{"generic-adapter", "GITHUB", &Credential{Kind: "BROKER_IDENTITY", BrokerPurpose: "KUBERNETES_TARGET"}, "", "", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			definition := Package{Spec: Spec{Adapter: test.adapter, Credential: test.credential}}
			delivery, ok := definition.ConnectionCredentialDelivery()
			if ok != test.valid || delivery.Kind != test.kind || delivery.BrokerPurpose != test.purpose {
				t.Fatal("credential representation crossed a package boundary")
			}
		})
	}
}

// TestShippedCredentialDeliveryRetainsExistingTokenMeanings checks every published package,
// preserving credential-free owner routes and refusing broker metadata on current token adapters.
// TestShippedCredentialDeliveryRetainsExistingTokenMeanings проверяет каждый published package,
// сохраняя credential-free owner routes и запрещая broker metadata у текущих token adapters.
func TestShippedCredentialDeliveryRetainsExistingTokenMeanings(t *testing.T) {
	t.Parallel()
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		delivery, ok := definition.ConnectionCredentialDelivery()
		if definition.Metadata.Key == SSHManagedDefinitionKey || definition.Metadata.Key == SSHExecuteDefinitionKey {
			if !ok || delivery.Kind != CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != "SSH_TARGET" || !definition.RequiresConnectionCredential() || definition.Spec.Credential.SecretKey != "" {
				t.Fatal("SSH purpose acquired generic credential representation")
			}
			continue
		}
		if definition.Metadata.Key == KubernetesDefinitionKey {
			if !ok || definition.Spec.Adapter != string(AdapterKubernetes) || delivery.Kind != CredentialDeliveryBrokerIdentity ||
				delivery.BrokerPurpose != "KUBERNETES_TARGET" || !definition.RequiresConnectionCredential() || definition.Spec.Credential.SecretKey != "" {
				t.Fatal("Kubernetes package changed its private broker credential representation")
			}
			continue
		}
		if !ok || definition.RequiresConnectionCredential() && delivery.Kind != CredentialDeliveryToken || !definition.RequiresConnectionCredential() && delivery.Kind != CredentialDeliveryNone || delivery.BrokerPurpose != "" {
			t.Fatal("existing package changed its credential representation")
		}
	}
	definition := definitions["github"]
	definition.Spec.Credential = &Credential{Kind: "BROKER_IDENTITY", BrokerPurpose: "KUBERNETES_TARGET"}
	if err := validate(&definition); err == nil {
		t.Fatal("generic executable adapter accepted a broker identity")
	}
}

// TestCredentialDeliveryWirePresence rejects an explicitly present empty/null alternate branch,
// duplicate names and case aliases before they can disappear during canonical JSON serialization.
// TestCredentialDeliveryWirePresence отвергает явно переданную empty/null alternate branch,
// duplicate names и case aliases до их исчезновения при canonical JSON serialization.
func TestCredentialDeliveryWirePresence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		raw     string
		adapter string
		valid   bool
	}{
		{`{"kind":"TOKEN","secretKey":"token"}`, "GITHUB", true},
		{`{"kind":"BROKER_IDENTITY","brokerPurpose":"KUBERNETES_TARGET"}`, "KUBERNETES", true},
		{`{"kind":"TOKEN","secretKey":"token","brokerPurpose":""}`, "GITHUB", false},
		{`{"kind":"TOKEN","secretKey":"token","brokerPurpose":null}`, "GITHUB", false},
		{`{"kind":"BROKER_IDENTITY","secretKey":"","brokerPurpose":"KUBERNETES_TARGET"}`, "KUBERNETES", false},
		{`{"kind":"BROKER_IDENTITY","secretKey":null,"brokerPurpose":"KUBERNETES_TARGET"}`, "KUBERNETES", false},
		{`{"kind":"TOKEN","kind":"TOKEN","secretKey":"token"}`, "GITHUB", false},
		{`{"Kind":"TOKEN","secretKey":"token"}`, "GITHUB", false},
		{`{"kind":"TOKEN","secretKey":"token","value":"synthetic-private-value"}`, "GITHUB", false},
	} {
		var credential Credential
		err := json.Unmarshal([]byte(test.raw), &credential)
		if valid := err == nil && validConnectionCredential(test.adapter, &credential); valid != test.valid {
			t.Fatal("JSON credential representation lost exact presence/type semantics")
		}
		if test.valid {
			canonical, err := json.Marshal(credential)
			if err != nil || string(canonical) == "{}" {
				t.Fatal("valid credential metadata did not retain a canonical representation")
			}
		}
	}
	for _, raw := range []string{
		"kind: TOKEN\nsecretKey: token\nbrokerPurpose: ''\n",
		"kind: TOKEN\nsecretKey: token\nbrokerPurpose: null\n",
		"kind: BROKER_IDENTITY\nbrokerPurpose: KUBERNETES_TARGET\nsecretKey: ''\n",
		"kind: BROKER_IDENTITY\nbrokerPurpose: KUBERNETES_TARGET\nsecretKey: null\n",
		"kind: TOKEN\nkind: TOKEN\nsecretKey: token\n",
		"kind: TOKEN\nsecretKey: 12345\n",
		"Kind: TOKEN\nsecretKey: token\n",
	} {
		var credential Credential
		err := yaml.Unmarshal([]byte(raw), &credential)
		if err == nil && (validConnectionCredential("GITHUB", &credential) || validConnectionCredential("KUBERNETES", &credential)) {
			t.Fatal("YAML credential representation lost exact presence/type semantics")
		}
	}
}
