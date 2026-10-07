package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestIntegrationDefinitionBindingHTTPPresenceAndPins(t *testing.T) {
	for _, test := range []struct {
		name    string
		binding *cp.IntegrationDefinitionConfigurationBinding
		want    string
	}{
		{"omitted", nil, ""},
		{"absent", &cp.IntegrationDefinitionConfigurationBinding{State: cp.IntegrationDefinitionConfigurationBinding_STATE_ABSENT}, `{"state":"ABSENT"}`},
		{"match", &cp.IntegrationDefinitionConfigurationBinding{State: cp.IntegrationDefinitionConfigurationBinding_STATE_MATCH, ConfigurationRef: "mconf_bindingfixture", RevisionRef: "mrev_bindingfixture", BindingVersion: 7}, `{"bindingVersion":7,"configurationRef":"mconf_bindingfixture","revisionRef":"mrev_bindingfixture","state":"MATCH"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			writeMessage(writer, http.StatusOK, &cp.GetIntegrationConnectionResponse{Connection: &cp.IntegrationConnection{Ref: "icon_bindingfixture", Version: 1, DefinitionConfigurationBinding: test.binding}}, "connection", "")
			if writer.Code != http.StatusOK {
				t.Fatalf("unexpected HTTP status: %d", writer.Code)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if got := string(body["definitionConfigurationBinding"]); got != test.want {
				t.Fatalf("binding = %s, want %s", got, test.want)
			}
		})
	}
}

func TestIntegrationDefinitionBindingHTTPRejectsInvalidUnion(t *testing.T) {
	for _, binding := range []*cp.IntegrationDefinitionConfigurationBinding{
		{},
		{State: cp.IntegrationDefinitionConfigurationBinding_State(99)},
		{State: cp.IntegrationDefinitionConfigurationBinding_STATE_ABSENT, BindingVersion: 1},
		{State: cp.IntegrationDefinitionConfigurationBinding_STATE_ABSENT, ConfigurationRef: "mconf_hiddenfixture"},
		{State: cp.IntegrationDefinitionConfigurationBinding_STATE_MATCH},
		{State: cp.IntegrationDefinitionConfigurationBinding_STATE_MATCH, ConfigurationRef: "mconf_fixture", RevisionRef: "mrev_fixture", BindingVersion: -1},
		{State: cp.IntegrationDefinitionConfigurationBinding_STATE_MATCH, ConfigurationRef: "mconf_fixture", RevisionRef: "mrev_fixture", BindingVersion: maximumSafeJSONInteger + 1},
	} {
		writer := httptest.NewRecorder()
		writeMessage(writer, http.StatusOK, &cp.GetIntegrationConnectionResponse{Connection: &cp.IntegrationConnection{DefinitionConfigurationBinding: binding}}, "connection", "")
		if writer.Code != http.StatusInternalServerError && writer.Code != http.StatusBadGateway {
			t.Fatalf("invalid binding accepted: status=%d", writer.Code)
		}
	}
}
