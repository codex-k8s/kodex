package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// proofRequestWriter captures a synthetic request after a required pre-write hook, optionally
// failing the write to test that no acknowledgement projection appears early.
// proofRequestWriter сохраняет synthetic request после обязательного pre-write hook, при необходимости
// имитируя отказ записи для проверки отсутствия преждевременной acknowledgement projection.
type proofRequestWriter struct {
	bytes.Buffer
	beforeWrite func()
	failure     error
}

// Write invokes the nonnil hook before either returning the injected failure or buffering all bytes.
// Write вызывает ненулевой hook перед возвратом подставленного отказа либо буферизацией всех байтов.
func (writer *proofRequestWriter) Write(raw []byte) (int, error) {
	writer.beforeWrite()
	if writer.failure != nil {
		return 0, writer.failure
	}
	return writer.Buffer.Write(raw)
}

// Close satisfies the request-stream interface without owning a descriptor or clearing captured bytes.
// Close реализует request-stream interface, не владея descriptor и не очищая сохранённые байты.
func (*proofRequestWriter) Close() error { return nil }

// proofFixture creates current synthetic TURN metadata, private sentinels and matching guide/prompt
// files alongside deliberately stale WARM state; it contains no real credential or provider execution.
// proofFixture создаёт текущие synthetic TURN metadata, private sentinels и совпадающие guide/prompt
// files рядом с намеренно устаревшим WARM state; реальных credential и provider execution здесь нет.
func proofFixture(t *testing.T) (model.Input, []byte) {
	t.Helper()
	root := t.TempDir()
	input := model.Input{Mode: runtimecontract.RunnerModeTurn, OrganizationRef: "org_abcdefgh", ProjectRef: "prj_abcdefgh", AgentRef: "agt_abcdefgh",
		RunRef: "run_current0", NodeRef: "node_current0", SessionRef: "ses_current0", TurnRef: "trn_current0", Attempt: 7,
		LeaseRef: "lease_current0", LeaseGeneration: 8, InputDigest: strings.Repeat("a", 64),
		RuntimeRevisionRef: "rrev_current0", RuntimeRevisionVersion: 9, RuntimeRevisionDigest: strings.Repeat("b", 64),
		RuntimeEnvironmentRef: "env_current0", RuntimeEnvironmentVersion: 11, RuntimeEnvironmentDigest: strings.Repeat("c", 64),
		Provider: "openai", Model: "gpt-6-astra", ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high",
		ConfigOverlay: "model_reasoning_effort = \"high\"\n", Instructions: "Русские instructions private-instructions-sentinel",
		Task: "QA_INPUT_MARKER private-task-sentinel", WorkspaceRoot: root,
		EnvironmentTools: []runtimecontract.RuntimeEnvironmentTool{{Name: "Git", Command: "git", Description: "private-tool-description"}},
		IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{{Ref: "igr_current0", GrantVersion: 3, ConnectionRef: "icn_current0", ConnectionVersion: 4,
			CapabilityKey: "context7.docs.query", ApprovalPolicy: "NONE", InputSchema: "private-schema-sentinel", ConnectionName: "private-connection-name", CapabilityDescription: "private-grant-description"}},
		Capabilities: []string{"context7.docs.query"},
		LeaseFence:   "private-fence-sentinel", ProviderCredentialRef: "private-credential-ref", ProviderCredentialSHA256: "private-credential-digest",
		CallbackURL: "https://private-callback.invalid", ExecutionTicketFile: "/private-ticket-path",
		EnvironmentValues: []runtimecontract.RuntimeEnvironmentValue{{Name: "PRIVATE_VARIABLE", Value: "private-env-value"}},
		BoundedInput:      map[string]any{"secret": "private-bounded-input"},
		SessionContext:    []runtimecontract.RunnerSessionMessage{{Role: "ASSISTANT", Content: "private-history-reasoning"}},
	}
	snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef}
	snapshot.Digest, _ = snapshot.ComputeDigest()
	input.ContextSnapshot = &snapshot
	prompt := []byte("Материализованный prompt\n" + input.Task + "\nprivate-context-sentinel")
	if os.MkdirAll(filepath.Join(root, ".kodex/inbox"), 0o700) != nil ||
		os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(input.Instructions), 0o600) != nil ||
		os.WriteFile(filepath.Join(root, ".kodex/inbox/prompt.md"), prompt, 0o600) != nil {
		t.Fatal("prepare proof fixture")
	}
	// Статический WARM input намеренно устарел. Proof не имеет доступа к нему.
	if os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"run_ref":"run_stale_warm","attempt":0}`), 0o600) != nil {
		t.Fatal("prepare warm fixture")
	}
	return input, prompt
}

// TestProviderInputProofClosedComparisonsAndPayloadShape distinguishes changed/unavailable files,
// rejects a leaf alias, arbitrary credential path and representative invalid first-item/model/effort shapes.
// TestProviderInputProofClosedComparisonsAndPayloadShape отличает changed/unavailable files,
// отвергает leaf alias, произвольный credential path и неверные first-item/model/effort shapes.
func TestProviderInputProofClosedComparisonsAndPayloadShape(t *testing.T) {
	input, prompt := proofFixture(t)
	params := map[string]any{"model": input.Model, "effort": input.EffectiveReasoningEffort, "input": []map[string]any{{"type": "text", "text": string(prompt)}}}
	if os.WriteFile(filepath.Join(input.WorkspaceRoot, "AGENTS.md"), []byte("different"), 0o600) != nil {
		t.Fatal("change fixture")
	}
	proof, err := newProviderInputProof(input, params)
	if err != nil || proof.InstructionsFileComparison != proofDifferent {
		t.Fatal("mismatch reported as equal")
	}
	if os.Remove(filepath.Join(input.WorkspaceRoot, "AGENTS.md")) != nil || os.Symlink(filepath.Join(input.WorkspaceRoot, "runtime.json"), filepath.Join(input.WorkspaceRoot, "AGENTS.md")) != nil {
		t.Fatal("prepare alias")
	}
	proof, err = newProviderInputProof(input, params)
	if err != nil || proof.InstructionsFileComparison != proofUnavailable || proof.InstructionsFileSHA256 != "" {
		t.Fatal("alias was followed")
	}
	for _, invalid := range []map[string]any{
		{"model": input.Model, "input": []map[string]any{}},
		{"model": input.Model, "input": []map[string]any{{"type": "image", "text": "private"}}},
		{"model": "other", "effort": input.EffectiveReasoningEffort, "input": params["input"]},
		{"model": input.Model, "effort": "other", "input": params["input"]},
	} {
		if _, err := newProviderInputProof(input, invalid); err == nil {
			t.Fatal("invalid provider payload accepted")
		}
	}
	if _, err := readProviderProofFile(input.WorkspaceRoot, ".kodex/state/opencode/auth.json"); err == nil {
		t.Fatal("arbitrary credential path accepted")
	}
}
