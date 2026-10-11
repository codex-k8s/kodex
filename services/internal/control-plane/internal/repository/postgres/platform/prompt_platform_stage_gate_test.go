package platform

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
)

func TestPlatformStageGateV3SeparatesCodeFromAuthAndDeployment(t *testing.T) {
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			binding, err := platformInstructionBinding(locale)
			if err != nil || binding.Revision != "platform-source-conformity-v3" || binding.Ref != "pinst_source_conformity_v3_"+locale || len(binding.Content) > maximumPlatformInstructionBytes {
				t.Fatal("fresh stage guidance is not exactly versioned and bounded")
			}
			required := []string{"CODE_ONLY", "authentication execution and deployment", "prohibited action remains STOP", "Unknown future deployment", "not by itself a blanket code-only blocker", "required current-gate proof", "UNKNOWN", "PASS/READY", "trusted deployment provenance", "Pod, VM or license", "no permission, exemption or compliance verdict"}
			if locale == "ru" {
				required = []string{"CODE_ONLY", "исполнение авторизации и deployment", "запрещённое действие остаётся STOP", "Неизвестный будущий deployment", "не запрещает code-only целиком", "обязательное доказательство текущего gate", "UNKNOWN", "PASS/READY", "доверенное происхождение deployment", "Pod, VM или лицензии", "не выдаёт прав, исключений или verdict соответствия"}
			}
			for _, guard := range required {
				if !strings.Contains(binding.Content, guard) {
					t.Fatalf("stage boundary missing: %q", guard)
				}
			}
			for _, forbidden := range []string{"1796", "k3d-kodex", "current deployment is permitted", "current compliance PASS", "skip auth policy", "ignore policy", "MODE=LOCAL"} {
				if strings.Contains(binding.Content, forbidden) {
					t.Fatal("generic instruction declared a deployment verdict or exemption")
				}
			}
		})
	}
}

func TestPlatformStageGateV3KeepsPublishedV2BytePins(t *testing.T) {
	for locale, expected := range map[string]string{"en": "c840a732d1d1a31379545262d93c55a78e55bbcc615f623b8bbf8a559b912094", "ru": "853b0a340ae4ad97c3c116f0fc1d0164ed668c8c064468730429108423e9cdd5"} {
		binding, err := platformInstructionBindingForRevision("platform-source-conformity-v2", locale)
		if err != nil || binding.Digest != expected || binding.Ref != "pinst_source_conformity_v2_"+locale || strings.Contains(binding.Content, "CODE_ONLY") {
			t.Fatal("published v2 instruction bytes changed")
		}
	}
}

func TestPlatformStageGateV3ContinuationChangesOnlyInstructionDependency(t *testing.T) {
	snapshot := workflowPromptFixture()
	oldBinding, err := platformInstructionBindingForRevision("platform-source-conformity-v2", snapshot.Locale)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.PlatformInstructions = &oldBinding
	snapshot.SemanticValues = map[string]string{"CONSTRAINTS": platformInstructionProjection(oldBinding), "INPUT": "EXACT_OWNER_INPUT"}
	oldRaw, _ := json.Marshal(snapshot)
	before, err := continuationComponents(map[string]any{"promptSnapshot": snapshot})
	if err != nil {
		t.Fatal(err)
	}
	currentBinding, err := platformInstructionBinding(snapshot.Locale)
	if err != nil || currentBinding.Revision != "platform-source-conformity-v3" {
		t.Fatal("fresh v3 binding missing")
	}
	snapshot.PlatformInstructions = &currentBinding
	snapshot.SemanticValues = map[string]string{"CONSTRAINTS": platformInstructionProjection(currentBinding), "INPUT": "EXACT_OWNER_INPUT"}
	after, err := continuationComponents(map[string]any{"promptSnapshot": snapshot})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := promptservice.CompareRuntimeContexts(before, after, promptservice.RuntimeDiff{PreviousRevisionRef: "rrev_old", CurrentRevisionRef: "rrev_new", SessionRef: "ses_fixture", TurnRef: "trn_fixture", Attempt: 2})
	if err != nil || len(diff.Changes) != 1 || diff.Changes[0].Component != "INSTRUCTIONS" {
		t.Fatal("stage guidance changed a non-instruction dependency")
	}
	for _, component := range []string{"MODEL", "ENVIRONMENT", "CAPABILITIES", "POLICY", "TOOLS"} {
		if !reflect.DeepEqual(before[component], after[component]) {
			t.Fatal("stage guidance changed execution authority")
		}
	}
	descriptor, err := platformInstructionDependency(oldRaw)
	if err != nil || descriptor == nil || descriptor.Ref != oldBinding.Ref || descriptor.Digest != oldBinding.Digest {
		t.Fatal("historical dependency was rewritten")
	}
}
