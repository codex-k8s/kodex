package platform

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
)

func continuationProducerFixture(t *testing.T) (map[string]any, promptservice.Materialization) {
	t.Helper()
	components := map[string][]promptservice.RuntimeDescriptor{}
	for _, key := range []string{"INSTRUCTIONS", "MODEL", "REASONING", "IMAGE", "ENVIRONMENT", "FILES", "SKILLS", "MEMORY", "TOOLS", "MCP", "INTEGRATIONS", "CAPABILITIES", "POLICY"} {
		components[key] = nil
	}
	diff, err := promptservice.CompareRuntimeContexts(components, components, promptservice.RuntimeDiff{PreviousRevisionRef: "rrev_previous", CurrentRevisionRef: "rrev_current", SessionRef: "ses_current", TurnRef: "trn_current", Attempt: 2})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(diff)
	snapshot := workflowPromptFixture()
	snapshot.TargetKind, snapshot.TargetRef = promptservice.TargetSessionContinuation, "ses_current"
	snapshot.UserCapabilities, snapshot.AgentCapabilities = []string{"read"}, []string{"read"}
	snapshot.Variables = map[string]string{"task": strings.Repeat("a", 40000)}
	snapshot.SessionContinuation = string(raw)
	materialized, err := promptservice.Materialize(defaultContinuationTemplate, promptservice.FromSnapshot(snapshot))
	if err != nil || !materialized.Complete || runtimecontract.ValidAssistantTurnContent(materialized.Prompt) || len(materialized.Prompt) > runtimecontract.MaximumContinuationNoticeBytes {
		t.Fatalf("invalid producer fixture: %v", err)
	}
	return map[string]any{"runtimeRevisionRef": "rrev_current", "sessionRef": "ses_current", "turnRef": "trn_current", "attempt": int32(2), "capabilities": []string{"read"}, "promptServiceTemplateRevision": materialized.ServiceTemplateRevision,
		"sessionContext": []map[string]string{{"role": "USER", "content": "Exact original input"}, {"role": "ASSISTANT", "content": "Exact prior output"}}}, materialized
}

func TestContinuationProducerAndSharedConsumerPreserveFullNotice(t *testing.T) {
	snapshot, materialized := continuationProducerFixture(t)
	previous := snapshot["sessionContext"].([]map[string]string)
	if err := appendRuntimeContinuationContext(snapshot, materialized); err != nil {
		t.Fatal(err)
	}
	messages := snapshot["sessionContext"].([]map[string]string)
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, RuntimeRevisionRef: "rrev_current", SessionRef: "ses_current", TurnRef: "trn_current", Attempt: 2, Capabilities: []string{"read"}, PromptServiceTemplateRevision: materialized.ServiceTemplateRevision, SessionContext: runtimeRevisionSessionContext(messages)}
	if notice, err := runtimecontract.ValidateRuntimeSessionContext(input); err != nil || !notice || len(messages) != 3 || messages[2]["content"] != materialized.Prompt || !reflect.DeepEqual(previous, messages[:2]) {
		t.Fatal("producer/consumer diverged or exact history changed")
	}
}

func TestContinuationProducerRejectsInvalidInputWithoutPartialAppend(t *testing.T) {
	for _, name := range []string{"revision", "session", "turn", "attempt", "caps", "service", "history", "size", "shape", "count"} {
		t.Run(name, func(t *testing.T) {
			snapshot, materialized := continuationProducerFixture(t)
			switch name {
			case "revision":
				snapshot["runtimeRevisionRef"] = "rrev_other"
			case "session":
				snapshot["sessionRef"] = "ses_other"
			case "turn":
				snapshot["turnRef"] = "trn_other"
			case "attempt":
				snapshot["attempt"] = int32(3)
			case "caps":
				snapshot["capabilities"] = []string{"admin"}
			case "service":
				snapshot["promptServiceTemplateRevision"] = "prompt-service-v4"
			case "history":
				snapshot["sessionContext"].([]map[string]string)[0]["content"] = strings.Repeat("a", runtimecontract.MaximumAssistantTurnCodepoints+1)
			case "size":
				materialized.Prompt += strings.Repeat(" ", runtimecontract.MaximumContinuationNoticeBytes-len(materialized.Prompt)+1)
			case "shape":
				snapshot["sessionContext"] = []string{"invalid"}
			case "count":
				snapshot["sessionContext"] = make([]map[string]string, 128)
			}
			before, _ := json.Marshal(snapshot)
			if appendRuntimeContinuationContext(snapshot, materialized) == nil {
				t.Fatal("invalid producer input accepted")
			}
			after, _ := json.Marshal(snapshot)
			if string(before) != string(after) {
				t.Fatal("failed append mutated immutable candidate")
			}
		})
	}
}
