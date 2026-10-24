package prompt

import (
	"strings"
	"testing"
)

// TestAutomationTaskPurposeUsesOnePassWithoutDuplicate checks literal template syntax inside task
// values stays inert and repeated purpose preparation includes the task once even when the
// original insertion was in a skipped branch.
// TestAutomationTaskPurposeUsesOnePassWithoutDuplicate проверяет неисполнение буквального
// синтаксиса шаблона в задаче и однократное включение задачи при повторной подготовке purpose даже
// при исходной вставке в пропущенной ветви.
func TestAutomationTaskPurposeUsesOnePassWithoutDuplicate(t *testing.T) {
	task := "Literal {{.user.name}}"
	snapshot := Snapshot{Variables: map[string]string{"task": task}}
	for _, text := range []string{"Coordinate the process.", "Coordinate {{ .task }}", "{{if false}}{{.task}}{{end}}Coordinate."} {
		purpose, err := AutomationTaskPurpose(text, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		again, err := AutomationTaskPurpose(purpose, snapshot)
		if err != nil || again != purpose {
			t.Fatalf("task purpose is not idempotent: %v", err)
		}
		parsed, err := parseTemplate(purpose)
		if err != nil {
			t.Fatal(err)
		}
		data := canonicalTemplateData(nil)
		setNestedTemplateValue(data, "task", task)
		rendered, err := executeTemplate(parsed, data)
		if err != nil || strings.Count(rendered, task) != 1 {
			t.Fatalf("task omitted, duplicated or evaluated again: %v", err)
		}
	}
}
