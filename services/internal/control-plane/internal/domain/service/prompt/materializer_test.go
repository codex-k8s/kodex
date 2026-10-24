package prompt

import (
	"errors"
	"strings"
	"testing"
)

// TestMaterializeUsesOnePinnedSnapshotAndMandatoryBlocks checks repeatable legacy rendering and
// digest from one snapshot, mandatory service blocks, effective authority intersection and
// redacted contextual values.
// TestMaterializeUsesOnePinnedSnapshotAndMandatoryBlocks проверяет повторяемые историческое
// построение и дайджест одного снимка, обязательные service-блоки, пересечение фактических
// полномочий и скрытие контекстных значений.
func TestMaterializeUsesOnePinnedSnapshotAndMandatoryBlocks(t *testing.T) {
	snapshot := Snapshot{
		TargetKind: TargetSessionContinuation, TargetRef: "agt_example", ProjectRef: "prj_example",
		RunRef: "run_example", SessionRef: "ses_example", TemplateRef: "ins_example",
		TemplateDigest: strings.Repeat("a", 64), Variables: map[string]string{"user.name": "Анна"},
		UserCapabilities: []string{"read", "write"}, AgentCapabilities: []string{"read"},
		ConnectionCapabilities: []string{"external"},
		SessionContinuation:    "Продолжить с immutable history.",
	}
	first, err := Materialize("Проект {{  .project.ref  }}, пользователь {{ .user.name }}.", snapshot)
	if err != nil {
		t.Fatalf("materialize prompt: %v", err)
	}
	second, err := Materialize("Проект {{  .project.ref  }}, пользователь {{ .user.name }}.", snapshot)
	if err != nil || first.Prompt != second.Prompt || first.Digest != second.Digest {
		t.Fatal("preview and runtime materialization diverged")
	}
	for _, block := range []string{"<workflow-stage used=\"false\">", "<automation used=\"false\">", "<session-continuation used=\"true\">"} {
		if !strings.Contains(first.Prompt, block) {
			t.Fatalf("mandatory service block %q is absent", block)
		}
	}
	if got := first.EffectiveCapabilities; len(got) != 1 || got[0] != "read" {
		t.Fatalf("effective capabilities = %#v", got)
	}
	if strings.Contains(first.SafePrompt, "Анна") || strings.Contains(first.SafePrompt, "immutable history") ||
		!strings.Contains(first.SafePrompt, "[user.name]") || !strings.Contains(first.SafePrompt, "configured") {
		t.Fatalf("safe prompt leaked contextual values: %q", first.SafePrompt)
	}
	if !strings.Contains(first.Prompt, "Проект prj_example") || strings.Contains(first.Prompt, "{{") {
		t.Fatalf("prompt contains an unresolved variable: %q", first.Prompt)
	}
}

// TestExplicitEmptyAuthorityLayerFailsClosed checks an explicit empty layer denies every
// capability and trusted grant union remains sorted and duplicate-free.
// TestExplicitEmptyAuthorityLayerFailsClosed проверяет запрет всех capabilities явно пустым слоем
// и отсортированное объединение доверенных grants без повторов.
func TestExplicitEmptyAuthorityLayerFailsClosed(t *testing.T) {
	if got := Intersection([]string{}, []string{"read"}); len(got) != 0 {
		t.Fatalf("empty authority layer expanded permissions: %#v", got)
	}
	if got := Union([]string{"write", "read"}, []string{"read", "external"}); strings.Join(got, ",") != "external,read,write" {
		t.Fatalf("capability union = %#v", got)
	}
}

// TestApplicableEmptyHumanGateRemovesIntegrationGrant checks an applicable empty Human Gate
// removes an otherwise granted integration capability from both effective grants and legacy prompt
// output.
// TestApplicableEmptyHumanGateRemovesIntegrationGrant проверяет удаление ранее разрешённой
// capability интеграции применимым пустым Human Gate из effective grants и исторического prompt.
func TestApplicableEmptyHumanGateRemovesIntegrationGrant(t *testing.T) {
	result, err := Materialize("Agent {{ .target.ref }}", Snapshot{
		TargetKind: TargetAgent, TargetRef: "agt_example", TemplateRef: "ins_example",
		TemplateDigest: strings.Repeat("e", 64), UserCapabilities: []string{"integration.write"},
		AgentCapabilities: []string{}, ConnectionCapabilities: []string{"integration.write"},
		HumanGateCapabilities: []string{},
	})
	if err != nil {
		t.Fatalf("materialize gated prompt: %v", err)
	}
	if len(result.EffectiveCapabilities) != 0 || !strings.Contains(result.Prompt, "<effective-capabilities used=\"false\">") {
		t.Fatalf("empty applicable Human Gate expanded integration grant: %#v", result.EffectiveCapabilities)
	}
}

// TestMaterializeRejectsUnknownAndUnclosedVariables checks unknown fields, unclosed syntax,
// forbidden index and unrestricted dot output return ErrInvalid with diagnostics.
// TestMaterializeRejectsUnknownAndUnclosedVariables проверяет возврат ErrInvalid с диагностикой
// для неизвестных полей, незакрытого синтаксиса, запрещённого index и неограниченного вывода dot.
func TestMaterializeRejectsUnknownAndUnclosedVariables(t *testing.T) {
	snapshot := Snapshot{TargetKind: TargetAgent, TargetRef: "agt_example", TemplateRef: "ins_example", TemplateDigest: strings.Repeat("b", 64)}
	for _, template := range []string{
		"{{ .unknown.value }}", "{{ .project.ref", "{{ index . \"unknown\" }}", "{{ printf \"%v\" . }}",
	} {
		result, err := Materialize(template, snapshot)
		if !errors.Is(err, ErrInvalid) || len(result.Diagnostics) == 0 {
			t.Fatalf("invalid template accepted: %q, result=%#v, err=%v", template, result, err)
		}
	}
}

// TestValidateLocatesUnknownVariableWithoutEchoingTemplate checks an unknown variable is
// identified on the second Unicode source line without echoing template text or the field into its
// safe message.
// TestValidateLocatesUnknownVariableWithoutEchoingTemplate проверяет определение неизвестной
// переменной на второй Unicode-строке без возврата текста шаблона либо поля в безопасном
// сообщении.
func TestValidateLocatesUnknownVariableWithoutEchoingTemplate(t *testing.T) {
	diagnostics := Validate("первая строка\n{{ .unknown.value }}", Catalog())
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Code != "PROMPT_TEMPLATE_VARIABLE_UNKNOWN" || diagnostic.VariableName != "unknown.value" || diagnostic.Line != 2 || diagnostic.Column < 1 {
		t.Fatalf("unknown variable diagnostic = %#v", diagnostic)
	}
	if strings.Contains(diagnostic.Message, "первая строка") || strings.Contains(diagnostic.Message, "unknown.value") {
		t.Fatal("diagnostic echoed template input")
	}
}

// TestValidateLocatesVariableWithWrongExecutionType checks an invalid range over an agent-name
// string reports that field and its source line through safe execution diagnostics.
// TestValidateLocatesVariableWithWrongExecutionType проверяет указание поля имени агента и строки
// исходника в безопасной диагностике неверной итерации range по строковому значению.
func TestValidateLocatesVariableWithWrongExecutionType(t *testing.T) {
	diagnostics := Validate("{{.project.name}}\n{{range .agent.name}}value{{end}}", Catalog())
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Code != "PROMPT_TEMPLATE_EXECUTION_INVALID" || diagnostic.VariableName != "agent.name" || diagnostic.Line != 2 || diagnostic.Column < 1 {
		t.Fatalf("execution diagnostic = %#v", diagnostic)
	}
}

// TestIntersectionCannotEscalateAuthority checks the five-layer intersection returns only the
// capability shared by every authority layer.
// TestIntersectionCannotEscalateAuthority проверяет возврат только общей capability при
// пересечении пяти слоёв полномочий.
func TestIntersectionCannotEscalateAuthority(t *testing.T) {
	got := Intersection([]string{"read", "write", "admin"}, []string{"read", "write"}, []string{"read"}, []string{"read", "external"}, []string{"read"})
	if len(got) != 1 || got[0] != "read" {
		t.Fatalf("intersection = %#v", got)
	}
}

// TestMaterializationDigestPinsEveryAuthorityLayer checks adding an explicitly empty Human Gate
// changes the historical materialization digest even when the other pinned inputs are unchanged.
// TestMaterializationDigestPinsEveryAuthorityLayer проверяет изменение исторического дайджеста
// материализации при добавлении явно пустого Human Gate без изменения остальных закреплённых
// входов.
func TestMaterializationDigestPinsEveryAuthorityLayer(t *testing.T) {
	base := Snapshot{TargetKind: TargetAgent, TargetRef: "agt_example", TemplateRef: "ins_example",
		TemplateDigest: strings.Repeat("c", 64), UserCapabilities: []string{"read"}, AgentCapabilities: []string{"read"}}
	first, err := Materialize("Agent {{ .target.ref }}", base)
	if err != nil {
		t.Fatalf("materialize baseline: %v", err)
	}
	base.HumanGateCapabilities = []string{}
	second, err := Materialize("Agent {{ .target.ref }}", base)
	if err != nil {
		t.Fatalf("materialize explicit gate restriction: %v", err)
	}
	if first.Digest == second.Digest {
		t.Fatal("materialization digest did not pin an explicit Human Gate layer")
	}
}

// TestMaterializeTypedFileAndToolCollections checks typed file/tool rendering and exact descriptor
// values while safe legacy output hides values and retains only one placeholder item per
// collection.
// TestMaterializeTypedFileAndToolCollections проверяет типизированное построение файлов и
// инструментов и точные значения дескрипторов со скрытием значений и одним placeholder-элементом
// коллекции в безопасном историческом выводе.
func TestMaterializeTypedFileAndToolCollections(t *testing.T) {
	snapshot := Snapshot{
		TargetKind: TargetAgent, TargetRef: "agt_example", TemplateRef: "ins_example",
		TemplateDigest: strings.Repeat("d", 64),
		StructuredVariables: map[string]any{
			"input": map[string]any{
				"files": []any{
					map[string]any{"name": "plan.txt", "path": "/workspace/input/set_example/files/0001-plan.txt", "size": int64(837)},
					map[string]any{"name": "brief.txt", "path": "/workspace/input/set_example/files/0002-brief.txt", "size": int64(419)},
				},
				"files_count": 2,
			},
			"runtime": map[string]any{"environment": map[string]any{
				"tools": []any{map[string]any{"name": "gh", "description": "GitHub CLI"}},
			}},
		},
	}
	result, err := Materialize("{{ .input.files_count }}:{{ range .input.files }}{{ .name }}={{ .path }}#{{ .size }};{{ end }} {{ range .runtime.environment.tools }}{{ .name }}{{ end }}", snapshot)
	if err != nil {
		t.Fatalf("materialize typed collections: %v", err)
	}
	if !strings.Contains(result.Prompt, "2:plan.txt=/workspace/input/set_example/files/0001-plan.txt#837;brief.txt=/workspace/input/set_example/files/0002-brief.txt#419; gh") {
		t.Fatalf("typed collections were not rendered: %q", result.Prompt)
	}
	if strings.Contains(result.SafePrompt, "plan.txt") || strings.Contains(result.SafePrompt, "brief.txt") ||
		strings.Contains(result.SafePrompt, "/workspace/input") || strings.Contains(result.SafePrompt, "GitHub CLI") ||
		strings.Contains(result.SafePrompt, "837") || strings.Contains(result.SafePrompt, "419") ||
		strings.Count(result.SafePrompt, "[input.files.item.name]") != 1 {
		t.Fatalf("safe prompt leaked a typed descriptor: %q", result.SafePrompt)
	}
}
