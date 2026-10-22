package codex

import (
	"encoding/json"
	"testing"
)

// TestFinancialUnavailableRequiresExactOutcome checks the one allowed financial-unavailable envelope and rejects wrong method/code/message, data even when null, whitespace suffixes, duplicate keys and trailing JSON.
// TestFinancialUnavailableRequiresExactOutcome проверяет единственный допустимый financial-unavailable envelope и отклоняет wrong method/code/message, data даже при null, whitespace suffixes, duplicate keys и trailing JSON.
func TestFinancialUnavailableRequiresExactOutcome(t *testing.T) {
	for _, row := range []struct {
		method string
		raw    string
		want   bool
	}{
		{"account/rateLimits/read", `{"code":-32603,"message":"failed to fetch codex rate limits: no snapshots returned"}`, true},
		{"account/read", `{"code":-32603,"message":"failed to fetch codex rate limits: no snapshots returned"}`, false},
		{"account/rateLimits/read", `{"code":-32601,"message":"failed to fetch codex rate limits: no snapshots returned"}`, false},
		{"account/rateLimits/read", `{"code":-32603,"message":"failed to fetch codex rate limits: unauthorized"}`, false},
		{"account/rateLimits/read", `{"code":-32603,"message":"failed to fetch codex rate limits: policy denied"}`, false},
		{"account/rateLimits/read", `{"code":-32603,"message":"failed to fetch codex rate limits: no snapshots returned","data":null}`, false},
		{"account/rateLimits/read", `{"code":-32603,"message":"failed to fetch codex rate limits: no snapshots returned "}`, false},
		{"account/rateLimits/read", `{"code":-32603,"code":-32603,"message":"failed to fetch codex rate limits: no snapshots returned"}`, false},
		{"account/rateLimits/read", `{"code":-32603,"message":"failed to fetch codex rate limits: no snapshots returned"} {}`, false},
	} {
		if got := financialSnapshotsUnavailable(row.method, json.RawMessage(row.raw)); got != row.want {
			t.Fatal("unexpected financial outcome classification")
		}
	}
}
