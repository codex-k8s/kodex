package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// TestWorkspaceMountProofPinsOnlyCurrentBoundInode checks actual codec and each local commitment.
// It proves no owner BIND, PVC ownership, process join or filesystem read-only status by itself.
// TestWorkspaceMountProofPinsOnlyCurrentBoundInode проверяет codec и каждую local commitment.
// Сам тест не доказывает owner BIND, PVC ownership, process join или read-only файловую систему.
func TestWorkspaceMountProofPinsOnlyCurrentBoundInode(t *testing.T) {
	_, input, binding := workspaceQuiescenceFixture()
	input.WorkspaceStorage = &binding
	proof, err := NewWorkspaceMountProof(input, 42, 73)
	if err != nil {
		t.Fatal("valid current input was rejected")
	}
	raw, err := EncodeWorkspaceMountProof(proof)
	if err != nil || bytes.Contains(raw, []byte(input.LeaseFence)) || bytes.Contains(raw, []byte("session-archive/")) {
		t.Fatal("local proof contains private storage location or secret fence")
	}
	decoded, err := DecodeWorkspaceMountProof(raw)
	if err != nil || !decoded.Matches(input, 42, 73) || decoded.Matches(input, 43, 73) || decoded.Matches(input, 42, 74) {
		t.Fatal("local proof did not bind the observed exact inode")
	}
	binding.Generation++
	if decoded.Matches(input, 42, 73) {
		t.Fatal("old generation proof authorized the new materialization")
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"device":42`), []byte(`"device":0,"device":42`), 1),
		append(append([]byte(nil), raw...), '\n'),
		append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"authority":true}`)...),
		bytes.Repeat([]byte("x"), MaximumWorkspaceMountProofBytes+1),
	} {
		if _, err := DecodeWorkspaceMountProof(invalid); err == nil {
			t.Fatal("ambiguous or unknown local proof became valid")
		}
	}
}

// TestWorkspaceCallbacksPreserveLegacyBindingBytes checks the historical closed method set verbatim.
// Only the opt-in profile adds one exact POST; MCP stays bound to its historical single method.
// TestWorkspaceCallbacksPreserveLegacyBindingBytes проверяет прежний закрытый набор методов побайтно.
// Только opt-in profile добавляет один exact POST; у MCP сохраняется прежний единственный метод.
func TestWorkspaceCallbacksPreserveLegacyBindingBytes(t *testing.T) {
	_, input, binding := workspaceQuiescenceFixture()
	input.WorkloadInstance = "controller-synthetic"
	input.ExecutionBindingDigest, input.MCPBindingDigest = "", ""
	execution, mcp, err := RuntimeExecutionBindingDigests(input)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(struct {
		Materialization RunnerInput `json:"materialization"`
		Methods         []string    `json:"methods"`
	}{Materialization: input, Methods: []string{"GET artifact", "POST complete", "POST mcp", "POST native-tool-call", "POST progress", "POST provider-credential-refresh"}})
	if err != nil || bytes.Contains(legacy, []byte(`"workspace_storage"`)) {
		t.Fatal("legacy binding grew a new optional field")
	}
	sum := sha256.Sum256(legacy)
	if execution != hex.EncodeToString(sum[:]) {
		t.Fatal("legacy execution binding bytes changed")
	}
	legacyMCP, err := json.Marshal(struct {
		Materialization RunnerInput `json:"materialization"`
		Method          string      `json:"method"`
	}{Materialization: input, Method: "POST mcp"})
	sum = sha256.Sum256(legacyMCP)
	if err != nil || mcp != hex.EncodeToString(sum[:]) {
		t.Fatal("legacy MCP binding bytes changed")
	}
	input.WorkspaceStorage = &binding
	fresh, _, err := RuntimeExecutionBindingDigests(input)
	if err != nil || fresh == execution {
		t.Fatal("new storage profile did not get a distinct method binding")
	}
	bound, err := json.Marshal(struct {
		Materialization RunnerInput `json:"materialization"`
		Methods         []string    `json:"methods"`
	}{Materialization: input, Methods: []string{"GET artifact", "POST complete", "POST mcp", "POST native-tool-call", "POST progress", "POST provider-credential-refresh", "POST workspace"}})
	sum = sha256.Sum256(bound)
	if err != nil || fresh != hex.EncodeToString(sum[:]) {
		t.Fatal("opt-in callback catalog differs from its exact allowed method set")
	}
}
