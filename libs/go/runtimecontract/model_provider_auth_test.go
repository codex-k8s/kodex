package runtimecontract_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestGatewayCredentialIsBoundToDefinition closes gateway-to-gateway key substitution.
// TestGatewayCredentialIsBoundToDefinition закрывает подмену key между gateway definitions.
func TestGatewayCredentialIsBoundToDefinition(t *testing.T) {
	key := []byte("synthetic-test-key")
	raw, err := runtimecontract.EncodeGatewayProviderAuthentication("opencode-test-gateway", key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "OPENAI_API_KEY") || !strings.Contains(string(raw), "gateway-api-key") {
		t.Fatal("gateway credential can be confused with legacy Codex authentication")
	}
	decoded, err := runtimecontract.DecodeGatewayProviderAuthentication(raw, "opencode-test-gateway")
	if err != nil || !bytes.Equal(decoded, key) {
		t.Fatal("bound gateway credential rejected")
	}
	if _, err := runtimecontract.DecodeGatewayProviderAuthentication(raw, "opencode-other-gateway"); !errors.Is(err, runtimecontract.ErrGatewayProviderAuthentication) {
		t.Fatal("gateway credential was accepted for another definition")
	}
}

// TestGatewayCredentialRejectsLegacyAndAmbiguousPayloads prevents implicit auth-mode fallback.
// TestGatewayCredentialRejectsLegacyAndAmbiguousPayloads запрещает неявный auth-mode fallback.
func TestGatewayCredentialRejectsLegacyAndAmbiguousPayloads(t *testing.T) {
	for _, raw := range []string{
		`{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic-test-key"}`,
		`{"auth_mode":"gateway-api-key","provider_definition_key":"opencode-test-gateway","API_KEY":"synthetic-test-key","tokens":{}}`,
		`{"auth_mode":"gateway-api-key","provider_definition_key":"opencode-test-gateway","API_KEY":"synthetic-test-key","API_KEY":"different-test-key"}`,
		`{"auth_mode":"gateway-api-key","provider_definition_key":"opencode-test-gateway","API_KEY":"synthetic test key"}`,
	} {
		if _, err := runtimecontract.DecodeGatewayProviderAuthentication([]byte(raw), "opencode-test-gateway"); !errors.Is(err, runtimecontract.ErrGatewayProviderAuthentication) {
			t.Fatal("unsupported or ambiguous secret payload accepted")
		}
	}
}
