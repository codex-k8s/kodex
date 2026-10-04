package codex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClosedAccountReadFailurePinnedCategories(t *testing.T) {
	for _, row := range []struct{ message, reason string }{
		{"failed to load workspace requirements", "REQUIREMENTS_LOAD"},
		{"workspace routing requires a ChatGPT account id", "MISSING_ACCOUNT_ID"},
		{"workspace routing discovery cancelled", "DISCOVERY_CANCELLED"},
		{"account changed during workspace routing discovery", "ACCOUNT_CHANGED"},
		{"workspace routing discovery unauthorized (401)", "DISCOVERY_UNAUTHORIZED"},
		{"workspace routing discovery failed", "DISCOVERY_FAILED"},
		{"selected workspace missing from routing discovery", "MISSING_WORKSPACE"},
		{"duplicate workspace in routing discovery", "DUPLICATE_WORKSPACE"},
		{"failed to reload workspace requirements", "REQUIREMENTS_RELOAD"},
		{"configuration changed during workspace routing discovery; retry account/read", "CONFIGURATION_CHANGED"},
		{"workspace routing discovery cancelled during shutdown", "SHUTDOWN"},
		{"workspace routing discovery timed out", "DISCOVERY_TIMEOUT"},
		{"workspace routing discovery missing backend origin", "MISSING_BACKEND_ORIGIN"},
		{"workspace routing discovery has invalid account routing override", "INVALID_ROUTING_OVERRIDE"},
		{"workspace routing discovery must return an origin", "BACKEND_IS_NOT_ORIGIN"},
		{"required ChatGPT backend conflicts with workspace routing", "BACKEND_CONFLICT"},
		{"invalid workspace backend URL", "INVALID_BACKEND_URL"},
		{"workspace backend must use an HTTPS origin without credentials", "INVALID_BACKEND_ORIGIN"},
	} {
		t.Run(row.reason, func(t *testing.T) {
			raw := accountReadFailureFixture(t, row.message, nil)
			if got := closedAccountReadFailure("account/read", -32603, raw); got != row.reason {
				t.Fatalf("closed reason = %s, want %s", got, row.reason)
			}
			if got := safeAccountReadFailure(row.reason); got != row.reason {
				t.Fatal("known logging reason was rejected")
			}
			for _, near := range []string{" " + row.message, row.message + " ", row.message + ": private-sentinel", strings.ToUpper(row.message)} {
				if got := closedAccountReadFailure("account/read", -32603, accountReadFailureFixture(t, near, nil)); got != "UNKNOWN" {
					t.Fatalf("near-match reason = %s, want UNKNOWN", got)
				}
			}
		})
	}
}

func TestClosedAccountReadFailureRejectsOtherMethodAndCode(t *testing.T) {
	raw := accountReadFailureFixture(t, "workspace routing discovery failed", nil)
	for _, row := range []struct {
		method string
		code   int64
	}{
		{"account/read", -32600}, {"account/read", -32602}, {"account/read", 0},
		{"account/read ", -32603}, {"account/Read", -32603}, {"account/login/start", -32603}, {"", -32603},
	} {
		if got := closedAccountReadFailure(row.method, row.code, raw); got != "NONE" {
			t.Fatalf("unrelated RPC classified as %s", got)
		}
	}
}

func TestClosedAccountReadFailureRejectsInvalidAndSessionOnlyPayloads(t *testing.T) {
	for _, raw := range []json.RawMessage{
		nil, json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`{}`),
		json.RawMessage(`{"code":-32603,"message":"workspace routing discovery failed"} {}`),
		json.RawMessage(`{"code":-32600,"message":"workspace routing discovery failed"}`),
		json.RawMessage(`{"code":null,"message":"workspace routing discovery failed"}`),
		json.RawMessage(`{"code":"-32603","message":"workspace routing discovery failed"}`),
		json.RawMessage(`{"code":-32603,"message":null}`),
		json.RawMessage(`{"code":-32603,"message":42}`),
		json.RawMessage(`{"code":-32603,"message":""}`),
		json.RawMessage(`{"code":-32603,"code":-32603,"message":"workspace routing discovery failed"}`),
		json.RawMessage(`{"code":-32603,"message":"workspace routing discovery failed","message":"private-sentinel"}`),
		json.RawMessage(`{"code":-32603,"message":"workspace routing discovery failed","private-sentinel":true}`),
		accountReadFailureFixture(t, strings.Repeat("x", maximumDiagnosticBytes+1), nil),
		accountReadFailureFixture(t, "workspace routing discovery failed", strings.Repeat("x", maximumDiagnosticBytes)),
		accountReadFailureFixture(t, "invalid model provider URL", nil),
		accountReadFailureFixture(t, "invalid ChatGPT backend URL", nil),
		accountReadFailureFixture(t, "invalid session ChatGPT backend URL", nil),
		accountReadFailureFixture(t, "ChatGPT backend changed; start a new thread before sending more content", nil),
		append(accountReadFailureFixture(t, "workspace routing discovery failed", nil), 0xff),
	} {
		if got := closedAccountReadFailure("account/read", -32603, raw); got != "UNKNOWN" {
			t.Fatalf("invalid or session-only payload classified as %s", got)
		}
	}
}

func TestClosedAccountReadFailureNeverExposesPrivatePayload(t *testing.T) {
	const private = "private-sentinel-credential https://private.invalid/account?token=private-sentinel"
	for _, row := range []struct{ message, want string }{
		{private, "UNKNOWN"},
		{"workspace routing discovery failed " + private, "UNKNOWN"},
		{"workspace routing discovery failed", "DISCOVERY_FAILED"},
	} {
		raw := accountReadFailureFixture(t, row.message, map[string]any{"credential": private, "nested": map[string]string{"account": private}})
		got := closedAccountReadFailure("account/read", -32603, raw)
		if got != row.want || strings.Contains(got, "private") {
			t.Fatal("classifier returned a non-closed diagnostic")
		}
	}
	for _, reason := range []string{private, "DISCOVERY_FAILED " + private, "discovery_failed", "", "DISCOVERY_FAILED\n"} {
		if got := safeAccountReadFailure(reason); got != "UNKNOWN" {
			t.Fatal("logging classifier accepted a non-closed reason")
		}
	}
	for _, reason := range []string{"NONE", "UNKNOWN"} {
		if safeAccountReadFailure(reason) != reason {
			t.Fatal("logging classifier rejected a closed fallback")
		}
	}
}

func accountReadFailureFixture(t *testing.T, message string, data any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"code": -32603, "message": message, "data": data})
	if err != nil {
		t.Fatal("encode account read error fixture")
	}
	return raw
}
