package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

type proofRequestWriter struct {
	bytes.Buffer
	beforeWrite func()
	failure     error
}

func (writer *proofRequestWriter) Write(raw []byte) (int, error) {
	writer.beforeWrite()
	if writer.failure != nil {
		return 0, writer.failure
	}
	return writer.Buffer.Write(raw)
}
func (*proofRequestWriter) Close() error { return nil }

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

func TestProviderInputProofActualAcknowledgedPayloadAndPrivacy(t *testing.T) {
	input, prompt := proofFixture(t)
	var logs bytes.Buffer
	writer := &proofRequestWriter{beforeWrite: func() {
		if logs.Len() != 0 {
			t.Fatal("proof emitted before provider ACK")
		}
	}}
	messages := make(chan streamEvent, 1)
	messages <- streamEvent{message: wireMessage{kind: messageResponse, id: json.RawMessage(`1`), payload: json.RawMessage(`{"turn":{"id":"` + testTurnID + `","items":[],"status":"inProgress"}}`)}}
	server := &appServer{stdin: writer, messages: messages}
	state := newProtocolState("")
	state.threadID = testThreadID
	observer := providerInputProofLogger(slog.New(slog.NewJSONHandler(&logs, nil)))
	if err := server.startTurnWithInputProof(t.Context(), state, input, prompt, observer); err != nil {
		t.Fatal(err)
	}
	var request struct {
		Method string `json:"method"`
		Params struct {
			Model  string `json:"model"`
			Effort string `json:"effort"`
			Input  []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"input"`
		} `json:"params"`
	}
	var record struct {
		Event string             `json:"event"`
		Proof providerInputProof `json:"proof"`
	}
	if json.Unmarshal(writer.Bytes(), &request) != nil || json.Unmarshal(logs.Bytes(), &record) != nil {
		t.Fatal("invalid fixture wire/proof")
	}
	proof := record.Proof
	if request.Method != "turn/start" || len(request.Params.Input) < 1 || record.Event != providerInputProofEvent ||
		proof.ProviderPromptSHA256 != providerProofSHA([]byte(request.Params.Input[0].Text)) || proof.ProviderPromptBytes != len(prompt) ||
		proof.Model != request.Params.Model || proof.ReasoningEffort != request.Params.Effort || !proof.TaskInPrompt ||
		proof.TaskSHA256 != providerProofSHA([]byte(input.Task)) || proof.InstructionsSHA256 != providerProofSHA([]byte(input.Instructions)) ||
		proof.InstructionsFileComparison != proofEqual || proof.InboxPromptComparison != proofEqual ||
		proof.InstructionsFileSHA256 != proof.InstructionsSHA256 || proof.InboxPromptSHA256 != proof.ProviderPromptSHA256 ||
		proof.RunRef != input.RunRef || proof.TurnRef != input.TurnRef || proof.Attempt != 7 || proof.RuntimeRevisionVersion != 9 || proof.EnvironmentVersion != 11 ||
		len(proof.Tools) != 1 || proof.Tools[0].Command != "git" || len(proof.Grants) != 1 || proof.Grants[0].Version != 3 {
		t.Fatal("proof does not match actual acknowledged TURN or byte-equal files")
	}
	for _, private := range []string{"private-", "run_stale_warm", "QA_INPUT_MARKER", "PRIVATE_VARIABLE", "Материализованный", "Русские instructions"} {
		if bytes.Contains(logs.Bytes(), []byte(private)) {
			t.Fatal("private input reached proof log")
		}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(logs.Bytes(), &fields) != nil || len(fields) != 5 || fields[providerInputProofKey] == nil {
		t.Fatal("unexpected log envelope")
	}
}

func TestProviderInputProofNoReceiptBeforeSuccessfulProviderAck(t *testing.T) {
	for _, failure := range []string{"write", "rejected", "invalid ACK", "missing ACK"} {
		t.Run(failure, func(t *testing.T) {
			input, prompt := proofFixture(t)
			var logs bytes.Buffer
			writer := &proofRequestWriter{beforeWrite: func() {
				if logs.Len() != 0 {
					t.Fatal("early proof")
				}
			}}
			messages := make(chan streamEvent, 1)
			switch failure {
			case "write":
				writer.failure = errors.New("private-write-error")
			case "rejected":
				messages <- streamEvent{message: wireMessage{kind: messageError, id: json.RawMessage(`1`), payload: json.RawMessage(`{"code":-32000,"message":"private-provider-error"}`)}}
			case "invalid ACK":
				messages <- streamEvent{message: wireMessage{kind: messageResponse, id: json.RawMessage(`1`), payload: json.RawMessage(`{"turn":{"id":"` + testTurnID + `","items":[],"status":"completed"}}`)}}
			case "missing ACK":
				close(messages)
			}
			state := newProtocolState("")
			state.threadID = testThreadID
			server := &appServer{stdin: writer, messages: messages}
			if server.startTurnWithInputProof(t.Context(), state, input, prompt, providerInputProofLogger(slog.New(slog.NewJSONHandler(&logs, nil)))) == nil || logs.Len() != 0 {
				t.Fatal("failed/unacknowledged provider input produced a receipt")
			}
		})
	}
}

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
	if _, err := readProviderProofFile(input.WorkspaceRoot, ".kodex/state/codex-home/auth.json"); err == nil {
		t.Fatal("arbitrary credential path accepted")
	}
}
