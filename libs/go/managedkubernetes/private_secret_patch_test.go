package managedkubernetes

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProtectedSecretPatchValidationDoesNotOpenPublicInput accepts owner-local private content
// while proving that the exact same data path remains forbidden in durable public input.
// TestProtectedSecretPatchValidationDoesNotOpenPublicInput принимает owner-local private content
// и доказывает, что тот же data path остаётся запрещён в durable public input.
func TestProtectedSecretPatchValidationDoesNotOpenPublicInput(t *testing.T) {
	private := []byte(`[{"op":"add","path":"/data/known","value":"a25vd24="}]`)
	if ValidateProtectedSecretPatchBody(private, "JSON") != nil ||
		ValidateProtectedSecretPatchBody([]byte(`{"data":{"known":"a25vd24="}}`), "MERGE") != nil ||
		ValidateProtectedSecretPatchBody([]byte(`{"stringData":{"known":"fixture"}}`), "STRATEGIC") != nil ||
		ValidateProtectedSecretPatchBody([]byte(`[]`), "JSON") != nil ||
		ValidateProtectedSecretPatchBody([]byte(`{}`), "MERGE") != nil {
		t.Fatal("owned protected patch shape was rejected")
	}
	input := OperationInput{Version: "v1", Resource: "secrets", Namespace: "selected", Name: "selected",
		UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "37", Body: private,
		Options: json.RawMessage(`{"patchType":"JSON"}`)}
	raw, _ := json.Marshal(input)
	if _, canonical, err := DecodeOperationInput("kubernetes.patch", raw); err == nil || canonical != nil {
		t.Fatal("private data path entered canonical public invocation input")
	}
}

// TestProtectedSecretPatchUsesCommonClosedShape rejects ambiguous paths/options without creating
// another JSON Patch dialect; target and readonly-field authorization are deliberately separate.
// TestProtectedSecretPatchUsesCommonClosedShape отклоняет ambiguous paths/options без нового
// JSON Patch dialect; target/readonly-field authorization намеренно проверяется отдельно.
func TestProtectedSecretPatchUsesCommonClosedShape(t *testing.T) {
	for _, candidate := range []struct{ raw, kind string }{
		{`{"data":{}}`, "JSON"},
		{`[{"op":"add","path":"/data/key","value":"known"}]`, "MERGE"},
		{`[{"op":"add","op":"remove","path":"/data/key","value":"known"}]`, "JSON"},
		{`[{"Op":"add","path":"/data/key","value":"known"}]`, "JSON"},
		{`[{"op":"add","path":"/data/~2","value":"known"}]`, "JSON"},
		{`[{"op":"copy","path":"/data/key"}]`, "JSON"},
		{`[{"op":"add","path":"/data/key","value":"known","flags":"unsafe"}]`, "JSON"},
		{`{"data":{}} {}`, "MERGE"}, {`{"data":{}}`, "UNKNOWN"},
		{"{\"data\":\"\xff\"}", "MERGE"},
		{`{"data":"` + strings.Repeat("a", MaximumProtectedSecretPatchBytes) + `"}`, "MERGE"},
	} {
		if ValidateProtectedSecretPatchBody([]byte(candidate.raw), candidate.kind) == nil {
			t.Fatal("ambiguous protected patch was admitted")
		}
	}
}
