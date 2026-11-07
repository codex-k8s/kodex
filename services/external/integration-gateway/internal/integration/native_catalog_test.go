package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestAllNativeCapabilitiesUseSelectedTransport enumerates the complete native catalog,
// including catalog helpers, uploads and Server/DC variants, and forbids core-client fallback.
// Mock provider replies prove route selection and unchanged receipts, not live external permissions.
// TestAllNativeCapabilitiesUseSelectedTransport перебирает весь native catalog, включая
// catalog helpers, uploads и Server/DC, запрещая core-client fallback. Mock ответы доказывают
// выбор маршрута и сохранность receipts, а не живые внешние полномочия.
func TestAllNativeCapabilitiesUseSelectedTransport(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"gitlab", "jira", "confluence", jiraServerDefinition, confluenceServerDefinition} {
		definition := definitions[key]
		if key == jiraServerDefinition || key == confluenceServerDefinition {
			definition = serverDefinitionFixture(t, key)
		}
		for _, capability := range definition.Spec.Capabilities {
			t.Run(key+"/"+capability.Key, func(t *testing.T) {
				adapter := testAdapter(t)
				token := "test-token"
				server := key == jiraServerDefinition || key == confluenceServerDefinition
				if server {
					token = "synthetic-server-token"
				}
				credential := testCredential(t, adapter, token)
				var request Request
				if server {
					request = atlassianServerInvocation(t, adapter, key, capability.Key, atlassianServerInput(capability.Key), credential)
				} else {
					raw, ok := catalogInputs()[capability.Operation]
					if !ok {
						t.Fatal("native capability lacks input fixture")
					}
					var input map[string]any
					if json.Unmarshal([]byte(raw), &input) != nil {
						t.Fatal("invalid native input fixture")
					}
					request = invocationRequest(t, definition, capability.Key, input, credential)
				}
				calls := 0
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Error("native request fell back to core provider transport")
					return nil, errors.New("fixture core route forbidden")
				})}
				adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Header.Get("Authorization") != "Bearer "+token {
						t.Fatal("native request changed credential")
					}
					if server {
						return atlassianServerResponse(t, request, r), nil
					}
					if r.URL.Host != key+".example.test" {
						t.Fatal("native request escaped configured origin")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(catalogResponse(t, key, capability.Operation, r))), Request: r}, nil
				})}
				result, err := adapter.Execute(t.Context(), request)
				if err != nil || calls == 0 || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest {
					t.Fatalf("native transport operation failed: %v calls=%d", err, calls)
				}
			})
		}
	}
}

// TestNativePersonalCredentialProbeUsesTheSameSelectedPeer keeps health and subsequent
// scope observations on the native client without substituting a shared key or transport.
// TestNativePersonalCredentialProbeUsesTheSameSelectedPeer сохраняет health и последующие
// scope observations на native client без подстановки общего ключа либо другого transport.
func TestNativePersonalCredentialProbeUsesTheSameSelectedPeer(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-gitlab-scope-key")
	definition := adapter.definitions["gitlab"]
	request := invocationRequest(t, definition, "gitlab.project.metadata.read", map[string]any{}, credential)
	request.CapabilityKey, request.Operation, request.GrantRef, request.GrantVersion = "", "", "", 0
	request.PersonalObservationProfiles, request.PersonalDeniedWriteCapabilityKeys = integrationpackage.CredentialObservationRequests(definition)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("personal native probe fell back to core transport")
		return nil, errors.New("fixture core route forbidden")
	})}
	adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer synthetic-gitlab-scope-key" || r.Method != http.MethodGet {
			t.Fatal("personal native probe changed credential or method")
		}
		if calls == 1 {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(catalogResponse(t, "gitlab", "gitlab.project.metadata.read", r))), Request: r}, nil
		}
		fixture := request
		fixture.Operation = "gitlab.issue.create"
		return gitLabPermissionResponse(t, fixture, r, "allow"), nil
	})}
	result, err := adapter.VerifyPersonalCredential(t.Context(), request)
	if err != nil || calls < 2 || len(result.ConfirmedCapabilityKeys) != 1 || result.ConfirmedCapabilityKeys[0] != "gitlab.project.metadata.read" {
		t.Fatal("personal credential native route failed", err)
	}
}
