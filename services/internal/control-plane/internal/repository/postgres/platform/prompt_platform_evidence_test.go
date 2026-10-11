package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestPlatformEvidenceGuidanceIsClaimScopedAndNotAuthority(t *testing.T) {
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			binding, err := platformInstructionBinding(locale)
			if err != nil || binding.Revision != "platform-source-conformity-v4" || binding.Ref != "pinst_source_conformity_v4_"+locale || len(binding.Content) > 8<<10 {
				t.Fatal("fresh source evidence guidance is not exactly bounded and versioned")
			}
			required := []string{"claim -> mandatory fact -> sufficient evidence class", "exact source/version/digest/EOF", "residual UNKNOWN", "approved requirement, a proposed design decision and future acceptance", "not hidden implementation, runtime routing, delivery/order, cancellation or observed effects", "which mandatory fact needs full implementation source", "does not by itself make every transitive source a full-EOF gate", "do not discard a mandatory missing fact or declare its source unnecessary", "UNKNOWN/BLOCKED and its gate closed", "EXTRACT is not FULL", "not transitively complete unless proved", "do not call design or unverified runtime behavior PASS/READY", "only within explicitly owner-delegated decisions", "do not invent business rules or assume delegated authority", "never replay a denied URL through another transport or identity"}
			if locale == "ru" {
				required = []string{"утверждение → обязательный факт → достаточный класс evidence", "точные источник/version/digest/EOF", "остаточный UNKNOWN", "утверждённое требование, предлагаемый дизайн и будущую приёмку", "не скрытую реализацию, runtime routing, доставку/порядок, отмену или наблюдаемые эффекты", "какой обязательный факт требует полного source реализации", "сам по себе не превращает все транзитивные источники в full-EOF gate", "Не отбрасывай обязательный недостающий факт и не объявляй его источник ненужным", "UNKNOWN/BLOCKED этого утверждения и закрытый gate", "EXTRACT не равен FULL", "не является транзитивно полным без доказательства", "непроверенное runtime-поведение не называй PASS/READY", "только в явно делегированных владельцем решениях", "не выдумывай бизнес-правила и не предполагай делегированные полномочия", "запрещённый URL не повторяй через другой transport или identity"}
			}
			for _, text := range required {
				if !strings.Contains(binding.Content, text) {
					t.Fatalf("claim-scoped evidence guard missing: %q", text)
				}
			}
			for _, forbidden := range []string{"outgoing_message.rs", "1796", "a956", "skip required", "ignore mandatory"} {
				if strings.Contains(binding.Content, forbidden) {
					t.Fatal("generic instruction prescribed business evidence or a gate bypass")
				}
			}
		})
	}
}

func TestPlatformInstructionsPublishedRegistryRejoinsExactAllRevisions(t *testing.T) {
	for _, revision := range []string{"platform-source-conformity-v1", "platform-source-conformity-v2", "platform-source-conformity-v3", "platform-source-conformity-v4"} {
		for _, locale := range []string{"en", "ru"} {
			t.Run(revision+"/"+locale, func(t *testing.T) {
				binding, err := platformInstructionBindingForRevision(revision, locale)
				if err != nil {
					t.Fatal(err)
				}
				snapshot := workflowPromptFixture()
				snapshot.TargetKind, snapshot.TargetRef = promptservice.TargetAgent, "agt_fixture"
				snapshot.Locale, snapshot.PlatformInstructions = locale, &binding
				snapshot.SemanticValues = map[string]string{"CONSTRAINTS": "OWNER_UNCHANGED\n\n" + platformInstructionProjection(binding), "INPUT": "OWNER_INPUT_UNCHANGED"}
				before, err := promptservice.Materialize("Owner", promptservice.FromSnapshot(snapshot))
				if err != nil || validatePlatformInstructions(snapshot) != nil {
					t.Fatal("published binding cannot materialize")
				}
				raw, _ := json.Marshal(snapshot)
				var stored entity.PromptMaterializationSnapshot
				if json.Unmarshal(raw, &stored) != nil || validatePlatformInstructions(stored) != nil {
					t.Fatal("published binding cannot rejoin")
				}
				descriptor, err := platformInstructionDependency(raw)
				if err != nil || descriptor == nil || descriptor.Ref != binding.Ref || descriptor.Value != binding.Revision || descriptor.Digest != binding.Digest {
					t.Fatal("historical instruction dependency lost exact pins")
				}
				after, err := promptservice.Materialize("Owner", promptservice.FromSnapshot(stored))
				if err != nil || before.Prompt != after.Prompt || before.Digest != after.Digest || before.VariableSnapshotDigest != after.VariableSnapshotDigest || !reflect.DeepEqual(before.EffectiveCapabilities, after.EffectiveCapabilities) {
					t.Fatal("published snapshot changed bytes, digests or capabilities")
				}
				storedBefore, _ := json.Marshal(stored)
				err = bindPlatformInstructions(&stored)
				storedAfter, _ := json.Marshal(stored)
				if !errors.Is(err, errs.ErrConflict) || string(storedBefore) != string(storedAfter) {
					t.Fatal("fresh producer rewrote an existing immutable binding")
				}
			})
		}
	}
}

func TestPlatformInstructionsRegistryRejectsCrossRevisionAndLocalePins(t *testing.T) {
	for _, revision := range []string{"platform-source-conformity-v1", "platform-source-conformity-v2", "platform-source-conformity-v3", "platform-source-conformity-v4"} {
		for _, field := range []string{"revision", "ref", "content-digest", "locale"} {
			t.Run(revision+"/"+field, func(t *testing.T) {
				binding, _ := platformInstructionBindingForRevision(revision, "en")
				otherRevision := "platform-source-conformity-v1"
				if revision == otherRevision {
					otherRevision = "platform-source-conformity-v2"
				}
				other, _ := platformInstructionBindingForRevision(otherRevision, "en")
				snapshot := workflowPromptFixture()
				snapshot.Locale, snapshot.PlatformInstructions = "en", &binding
				switch field {
				case "revision":
					binding.Revision = other.Revision
				case "ref":
					binding.Ref = other.Ref
				case "content-digest":
					binding.Content, binding.Digest = other.Content, other.Digest
				case "locale":
					snapshot.Locale = "ru"
				}
				snapshot.SemanticValues = map[string]string{"CONSTRAINTS": platformInstructionProjection(binding)}
				raw, _ := json.Marshal(snapshot)
				if !errors.Is(validatePlatformInstructions(snapshot), errs.ErrConflict) {
					t.Fatal("cross-revision or cross-locale exact pins accepted")
				}
				if _, err := platformInstructionDependency(raw); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("mixed instruction descriptor accepted")
				}
			})
		}
	}
	for _, revision := range []string{"", "platform-source-conformity-v0", "platform-source-conformity-v02", "platform-source-conformity-v5"} {
		if _, err := platformInstructionBindingForRevision(revision, "en"); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("unknown instruction revision accepted")
		}
	}
}

func TestPlatformInstructionsV1ToV2ChangesOnlyInstructionDependency(t *testing.T) {
	snapshot := workflowPromptFixture()
	oldBinding, _ := platformInstructionBindingForRevision("platform-source-conformity-v1", snapshot.Locale)
	snapshot.PlatformInstructions = &oldBinding
	snapshot.SemanticValues = map[string]string{"CONSTRAINTS": platformInstructionProjection(oldBinding), "INPUT": "EXACT_OWNER_INPUT"}
	old := map[string]any{"promptSnapshot": snapshot}
	newBinding, _ := platformInstructionBindingForRevision("platform-source-conformity-v2", snapshot.Locale)
	snapshot.PlatformInstructions = &newBinding
	snapshot.SemanticValues = map[string]string{"CONSTRAINTS": platformInstructionProjection(newBinding), "INPUT": "EXACT_OWNER_INPUT"}
	current := map[string]any{"promptSnapshot": snapshot}
	before, err := continuationComponents(old)
	if err != nil {
		t.Fatal(err)
	}
	after, err := continuationComponents(current)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := promptservice.CompareRuntimeContexts(before, after, promptservice.RuntimeDiff{PreviousRevisionRef: "rrev_old", CurrentRevisionRef: "rrev_new", SessionRef: "ses_fixture", TurnRef: "trn_fixture", Attempt: 2})
	if err != nil || len(diff.Changes) != 1 || diff.Changes[0].Component != "INSTRUCTIONS" {
		t.Fatal("new instruction dependency is not an exact continuation change")
	}
	for _, component := range []string{"MODEL", "ENVIRONMENT", "CAPABILITIES", "POLICY", "TOOLS"} {
		if !reflect.DeepEqual(before[component], after[component]) {
			t.Fatal("evidence guidance changed execution authority")
		}
	}
}
