package canonical

import (
	"encoding/json"
	"testing"
)

func TestHashBindsAuthorityAndCanonicalArguments(t *testing.T) {
	t.Parallel()
	base := Request{
		DefinitionID: "payments", DefinitionVersion: 3,
		ConnectionID: "connection-1", ConnectionRevision: 7, ConnectionGeneration: 11,
		Capability: "payment", ToolName: "send-payment", ToolVersion: 2,
		TenantID: "tenant-1", ProjectID: "project-1", ProcessID: "process-1",
		SessionID: "session-1", SessionVersion: 2, ThreadID: "thread-1",
		TurnID: "turn-1", TurnVersion: 3, Attempt: 1, InputDigest: "input-digest",
		RuntimeRevisionID: "runtime-1", RuntimeRevisionVersion: 5,
		RuntimeRevisionDigest: "runtime-digest", RuntimeManifestDigest: "manifest-digest",
		RoleID: "role-1", RoleVersion: 7, GrantID: "grant-1", GrantGeneration: 13,
		Method: "POST", Path: "/v1/payments", Arguments: json.RawMessage(`{"amount":10,"currency":"EUR"}`),
	}
	first, _, err := Hash(base)
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.Arguments = json.RawMessage(`{ "currency": "EUR", "amount": 10 }`)
	second, _, err := Hash(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("canonical arguments changed hash: %s != %s", first, second)
	}
	changed := base
	changed.GrantGeneration++
	third, _, err := Hash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("authority generation is not bound to canonical hash")
	}
	changedDigest := base
	changedDigest.RuntimeManifestDigest = "other-manifest-digest"
	fourth, _, err := Hash(changedDigest)
	if err != nil {
		t.Fatal(err)
	}
	if first == fourth {
		t.Fatal("runtime manifest digest is not bound to canonical hash")
	}
}

func TestPreviewRedactsClosedFields(t *testing.T) {
	t.Parallel()
	preview, err := Preview(json.RawMessage(`{"amount":10,"token":"value","recipient":{"account":"DE00"}}`), []string{"/recipient/account"})
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"amount":10,"recipient":{"account":"[REDACTED]"},"token":"[REDACTED]"}`
	if string(preview) != expected {
		t.Fatalf("unexpected preview: %s", preview)
	}
}

func TestPreviewRedactsArrayPointer(t *testing.T) {
	t.Parallel()
	preview, err := Preview(
		json.RawMessage(`{"recipients":[{"account":"DE00"},{"account":"DE01"}]}`),
		[]string{"/recipients/1/account"},
	)
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"recipients":[{"account":"DE00"},{"account":"[REDACTED]"}]}`
	if string(preview) != expected {
		t.Fatalf("unexpected preview: %s", preview)
	}
}

func TestNormalizeRejectsTrailingData(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{"value":1}{"value":2}`, `{"value":1} broken`} {
		if _, err := Normalize([]byte(input), 1024); err == nil {
			t.Fatalf("trailing data was accepted: %q", input)
		}
	}
}
