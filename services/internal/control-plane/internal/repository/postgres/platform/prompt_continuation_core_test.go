package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/systemassistant"
)

// continuationCoreProducerFixture materializes the full current protected core and owner overlay
// with exact platform/runtime pins and prior history, returning a matching previous snapshot.
// continuationCoreProducerFixture материализует полное текущее защищённое ядро и overlay владельца
// с точными привязками платформы и runtime и прежней историей, возвращая совпадающий предыдущий
// снимок.
func continuationCoreProducerFixture(t *testing.T) (entity.PromptMaterializationSnapshot, map[string]any, []byte) {
	t.Helper()
	source := workflowPromptFixture()
	source.TargetKind, source.TargetRef = promptservice.TargetAgent, "agt_current"
	source.ProjectRef, source.RunRef, source.SessionRef = "prj_current", "run_current", "ses_current"
	source.ContextPin.AgentRef = "agt_current"
	source.Variables["node.ref"], source.Variables["turn.ref"], source.Variables["task"] = "nod_current", "trn_current", "Synthetic second"
	source.TemplateContent = "OWNER_OVERLAY_MARKER"
	digest := sha256.Sum256([]byte(systemassistant.CorePrompt()))
	source.AssistantCore = &entity.PromptAssistantCore{Scope: "PROJECT", Ref: "ins_core_fixture", Revision: systemassistant.CorePromptRevision,
		Digest: hex.EncodeToString(digest[:]), Content: systemassistant.CorePrompt()}
	if bindPlatformInstructions(&source) != nil {
		t.Fatal("bind fresh platform instructions")
	}
	main, err := promptservice.Materialize(source.TemplateContent, promptservice.FromSnapshot(source))
	if err != nil || !main.Complete {
		t.Fatal("materialize full initial core")
	}
	digest = sha256.Sum256([]byte(main.Prompt))
	current := map[string]any{"promptSnapshot": source, "instructions": main.Prompt, "instructionDigest": hex.EncodeToString(digest[:]),
		"promptMaterializationDigest": main.Digest, "promptServiceTemplateRevision": main.ServiceTemplateRevision, "capabilities": main.EffectiveCapabilities,
		"runtimeRevisionRef": "rrev_current", "agentRef": "agt_current", "projectRef": "prj_current", "runRef": "run_current", "nodeRef": "nod_current",
		"sessionRef": "ses_current", "turnRef": "trn_current", "attempt": int32(2), "task": source.Variables["task"],
		"sessionContext": []map[string]string{{"role": "USER", "content": "Exact original USER"}, {"role": "ASSISTANT", "content": "Exact previous result"}}}
	previous, _ := json.Marshal(map[string]any{"promptSnapshot": source, "capabilities": main.EffectiveCapabilities})
	return source, current, previous
}

// TestContinuationCoreProducerPinsFullCurrentInstructionsWithoutDuplicateBody checks a compact
// bounded continuation references the full current core/instructions exactly, preserves
// source/history, rejoins the durable notice and passes the shared runner consumer without
// duplicating core bodies.
// TestContinuationCoreProducerPinsFullCurrentInstructionsWithoutDuplicateBody проверяет точную
// ссылку компактного ограниченного продолжения на полное текущее ядро и инструкции, сохранение
// источника и истории, восстановление устойчивого уведомления и принятие общим runner consumer без
// повторения тел ядра.
func TestContinuationCoreProducerPinsFullCurrentInstructionsWithoutDuplicateBody(t *testing.T) {
	source, current, previous := continuationCoreProducerFixture(t)
	beforeSource, _ := json.Marshal(source)
	beforeInstructions := current["instructions"]
	beforeHistory, _ := json.Marshal(current["sessionContext"])
	var repository Repository
	notice, err := repository.prepareRuntimeContinuationNotice(t.Context(), continuationTransitionTx{previous: previous}, scope{}, current, source)
	if err != nil || notice == nil {
		t.Fatalf("fresh full core cannot continue: %s", runtimeEligibilityReason(err))
	}
	if len(notice.Materialization.Prompt) > runtimecontract.MaximumContinuationNoticeBytes ||
		strings.Contains(notice.Materialization.Prompt, "get_configuration_catalog") ||
		!strings.Contains(notice.Materialization.Prompt, promptservice.AssistantCoreContinuationProjectionRevision) ||
		!strings.Contains(stringMap(current, "instructions"), "get_configuration_catalog") ||
		current["instructions"] != beforeInstructions || !reflect.DeepEqual(notice.Snapshot.AssistantCore, source.AssistantCore) {
		t.Fatal("compact notice altered full current instructions, core pins, or unchanged byte bound")
	}
	runtime := notice.Snapshot.StructuredVariables["runtime"].(map[string]any)
	projectionRaw, _ := json.Marshal(runtime[promptservice.AssistantCoreContinuationProjectionKey])
	var projection promptservice.AssistantCoreContinuationProjection
	if json.Unmarshal(projectionRaw, &projection) != nil {
		t.Fatal("decode exact core reference")
	}
	if projection.CoreRef != source.AssistantCore.Ref || projection.CoreRevision != source.AssistantCore.Revision || projection.CoreDigest != source.AssistantCore.Digest ||
		projection.InstructionsSHA256 != stringMap(current, "instructionDigest") || projection.InstructionsBytes != len(stringMap(current, "instructions")) ||
		projection.PromptMaterializationDigest != stringMap(current, "promptMaterializationDigest") || projection.RuntimeRevisionRef != stringMap(current, "runtimeRevisionRef") {
		t.Fatal("reference lost exact current source equality or core identity")
	}
	afterSource, _ := json.Marshal(source)
	currentSource, _ := json.Marshal(current["promptSnapshot"])
	messages := current["sessionContext"].([]map[string]string)
	afterHistory, _ := json.Marshal(messages[:len(messages)-1])
	if string(beforeSource) != string(afterSource) || string(currentSource) != string(beforeSource) || string(beforeHistory) != string(afterHistory) {
		t.Fatal("projection mutated original snapshot or prior history")
	}
	raw, _ := json.Marshal(notice.Snapshot)
	var stored entity.PromptMaterializationSnapshot
	if json.Unmarshal(raw, &stored) != nil {
		t.Fatal("durable compact notice snapshot")
	}
	rejoined, err := promptservice.Materialize(stored.TemplateContent, promptservice.FromSnapshot(stored))
	if err != nil || rejoined.Prompt != notice.Materialization.Prompt || rejoined.Digest != notice.Materialization.Digest ||
		rejoined.VariableSnapshotDigest != notice.Materialization.VariableSnapshotDigest {
		t.Fatal("canonical rejoin changed compact notice")
	}
	if valid, err := runtimecontract.ValidateRuntimeSessionContext(runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn,
		RuntimeRevisionRef: "rrev_current", SessionRef: "ses_current", TurnRef: "trn_current", Attempt: 2,
		PromptServiceTemplateRevision: promptservice.ServiceTemplateRevision, Capabilities: runtimeRevisionStringSlice(current["capabilities"]),
		SessionContext: runtimeRevisionSessionContext(current["sessionContext"])}); err != nil || !valid {
		t.Fatal("shared runner consumer rejected compact owner-sealed notice")
	}
	t.Logf("main_instructions_bytes=%d continuation_notice_bytes=%d limit_bytes=%d", len(stringMap(current, "instructions")), len(notice.Materialization.Prompt), runtimecontract.MaximumContinuationNoticeBytes)
}

// TestContinuationCoreProjectionRejectsSourceMismatchWithoutMutation rejects altered
// instruction/core/execution/target pins or a preexisting projection marker and proves failed
// binding leaves the notice unchanged.
// TestContinuationCoreProjectionRejectsSourceMismatchWithoutMutation отклоняет изменённые привязки
// инструкции, ядра, исполнения и цели и уже существующий маркер проекции и доказывает неизменность
// уведомления после неудачной привязки.
func TestContinuationCoreProjectionRejectsSourceMismatchWithoutMutation(t *testing.T) {
	for _, mutation := range []string{"instructions", "instructions-digest", "main-digest", "source-snapshot", "source-core", "agent", "project", "run", "session", "node", "turn", "attempt", "notice-core", "notice-target", "existing-marker"} {
		t.Run(mutation, func(t *testing.T) {
			source, current, _ := continuationCoreProducerFixture(t)
			raw, _ := json.Marshal(source)
			var notice entity.PromptMaterializationSnapshot
			if json.Unmarshal(raw, &notice) != nil {
				t.Fatal("notice fixture")
			}
			notice.TargetKind, notice.TargetRef = promptservice.TargetSessionContinuation, source.SessionRef
			switch mutation {
			case "instructions":
				current["instructions"] = "Different full instructions"
			case "instructions-digest":
				current["instructionDigest"] = strings.Repeat("f", 64)
			case "main-digest":
				current["promptMaterializationDigest"] = strings.Repeat("f", 64)
			case "source-snapshot":
				current["promptSnapshot"] = map[string]any{"forged": true}
			case "source-core":
				source.AssistantCore.Content += "Changed source"
			case "agent":
				current["agentRef"] = "agt_other"
			case "project":
				current["projectRef"] = "prj_other"
			case "run":
				current["runRef"] = "run_other"
			case "session":
				current["sessionRef"] = "ses_other"
			case "node":
				current["nodeRef"] = "nod_other"
			case "turn":
				current["turnRef"] = "trn_other"
			case "attempt":
				current["attempt"] = int32(0)
			case "notice-core":
				notice.AssistantCore.Ref = "ins_other"
			case "notice-target":
				notice.TargetKind = promptservice.TargetAgent
			case "existing-marker":
				notice.StructuredVariables["runtime"] = map[string]any{promptservice.AssistantCoreContinuationProjectionKey: true}
			}
			before, _ := json.Marshal(notice)
			if !errors.Is(bindRuntimeContinuationCoreProjection(&notice, source, current), errs.ErrConflict) {
				t.Fatal("source mismatch accepted for compact core")
			}
			after, _ := json.Marshal(notice)
			if string(before) != string(after) {
				t.Fatal("rejected projection mutated notice")
			}
		})
	}
}
