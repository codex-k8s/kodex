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
			binding, err := platformInstructionBindingForRevision("platform-source-conformity-v4", locale)
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

func TestPlatformInstructionsV5ReadScopeAndCompletion(t *testing.T) {
	for _, locale := range []string{"en", "ru"} {
		binding, err := platformInstructionBinding(locale)
		if err != nil || binding.Revision != "platform-source-conformity-v5" || binding.Ref != "pinst_source_conformity_v5_"+locale || len(binding.Content) > maximumPlatformInstructionBytes {
			t.Fatal("fresh scoped read instruction is not bounded v5")
		}
		clauses := []string{"FULL gate origin", "explicit owner requirement", "concrete mandatory fact of this stage", "complete actual business diff", "generated file", "native pages to EOF", "full-byte provenance", "applicable declarations/schema", "scoped reading is not FULL", "checkpoint is progress, not stage completion", "available mandatory work", "explicit whole-source/native EOF", "denied/unknown effects"}
		if locale == "ru" {
			clauses = []string{"происхождение FULL gate", "явное требование владельца", "конкретный обязательный факт текущего этапа", "полный actual business diff", "generated файл", "native страниц до EOF", "full-byte provenance", "применимых declarations/schema", "scoped чтение не равно FULL", "checkpoint — прогресс, не завершение этапа", "доступная обязательная работа", "явные whole-source/native EOF", "denied/unknown effects"}
		}
		for _, clause := range clauses {
			if !strings.Contains(binding.Content, clause) {
				t.Fatalf("scope/completion guard missing: %q", clause)
			}
		}
	}
}

func TestPlatformHistoricalInstructionPins(t *testing.T) {
	protected := map[string]string{
		"platform-source-conformity-v1/en": "fa572d1632e72e967d8e3855ad4922505e55b68c2ea3807bab53a6fdd44b3fa0",
		"platform-source-conformity-v1/ru": "6d0134419a5a5c2543cf1a10244faa93c25e0d4cb3aece94084ed0906481e9ea",
		"platform-source-conformity-v2/en": "c840a732d1d1a31379545262d93c55a78e55bbcc615f623b8bbf8a559b912094",
		"platform-source-conformity-v2/ru": "853b0a340ae4ad97c3c116f0fc1d0164ed668c8c064468730429108423e9cdd5",
		"platform-source-conformity-v3/en": "58db829606aba2667bcd43423b0fa5935ec9d7f5a18f85abf75be4772638dc48",
		"platform-source-conformity-v3/ru": "fbe4937c81ddca7a9158b300bbb597bca8f018dc388d6d0be8e42dbfba18a654",
		"platform-source-conformity-v4/en": "0734f55eb1105dfd2aa688b9b63ea77724a28a31aa3d92ba0475d91973f7fea6",
		"platform-source-conformity-v4/ru": "1b78c95ec6d117cbf2dea043e86a6af6681e5b7a6f4da6dac796a043f358c5a1",
	}
	for _, revision := range []string{"platform-source-conformity-v1", "platform-source-conformity-v2", "platform-source-conformity-v3", "platform-source-conformity-v4"} {
		for _, locale := range []string{"en", "ru"} {
			binding, err := platformInstructionBindingForRevision(revision, locale)
			if err != nil {
				t.Fatal(err)
			}
			if binding.Digest != protected[revision+"/"+locale] {
				t.Fatal("published v1-v4 bytes changed")
			}
			t.Logf("revision=%s locale=%s bytes=%d sha256=%s", revision, locale, len(binding.Content), binding.Digest)
		}
	}
}

func TestPlatformInstructionsPublishedRegistryRejoinsExactAllRevisions(t *testing.T) {
	for _, revision := range []string{"platform-source-conformity-v1", "platform-source-conformity-v2", "platform-source-conformity-v3", "platform-source-conformity-v4", "platform-source-conformity-v5"} {
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
	for _, revision := range []string{"platform-source-conformity-v1", "platform-source-conformity-v2", "platform-source-conformity-v3", "platform-source-conformity-v4", "platform-source-conformity-v5"} {
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
	for _, revision := range []string{"", "platform-source-conformity-v0", "platform-source-conformity-v02", "platform-source-conformity-v6"} {
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
