package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestAgentRequiredBooleanHTTPProjection(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled bool
		system  bool
		state   cp.AgentState
	}{
		{"ordinary", true, false, cp.AgentState_AGENT_STATE_READY},
		{"system", true, true, cp.AgentState_AGENT_STATE_READY},
		{"disabled", false, false, cp.AgentState_AGENT_STATE_DISABLED},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := &cp.Agent{Ref: "agt_fixture01", ProjectRef: "prj_fixture01", Version: 1,
				Enabled: test.enabled, System: test.system, State: test.state}
			writer := httptest.NewRecorder()
			// Тот же generated response → protojson → normalize → JSON путь, что GetAgent.
			writeMessage(writer, http.StatusOK, &cp.GetAgentResponse{Agent: agent}, "agent", "")
			if writer.Code != http.StatusOK {
				t.Fatalf("unexpected HTTP status: %d", writer.Code)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			for field, expected := range map[string]bool{"enabled": test.enabled, "system": test.system} {
				literal := "false"
				if expected {
					literal = "true"
				}
				if string(body[field]) != literal {
					t.Fatalf("required Agent boolean lost or changed: %s", field)
				}
			}
			if string(body["ref"]) != `"agt_fixture01"` || string(body["projectRef"]) != `"prj_fixture01"` {
				t.Fatal("Agent owner identity changed")
			}
			if _, exists := body["currentRunRef"]; exists {
				t.Fatal("optional current run reference fabricated")
			}
		})
	}
}
