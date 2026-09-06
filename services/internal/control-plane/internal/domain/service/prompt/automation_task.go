package prompt

import (
	"encoding/json"
	"strings"
)

// AutomationTaskPurpose сохраняет пользовательский шаблон и добавляет task
// только когда его точного значения нет в результате одного прохода. Значение
// task остаётся данными, в том числе при наличии внутри него {{...}}.
func AutomationTaskPurpose(text string, snapshot Snapshot) (string, error) {
	task := snapshot.Variables["task"]
	if strings.TrimSpace(task) == "" || len(task) > 65536 || len(Validate(text, Catalog())) != 0 {
		return "", ErrInvalid
	}
	parsed, err := parseTemplate(text)
	if err != nil {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(snapshot.StructuredVariables)
	var structured map[string]any
	if err != nil || json.Unmarshal(raw, &structured) != nil {
		return "", ErrInvalid
	}
	data := canonicalTemplateData(structured)
	for name, value := range snapshot.Variables {
		setNestedTemplateValue(data, name, value)
	}
	rendered, err := executeTemplate(parsed, data)
	if err != nil {
		return "", ErrInvalid
	}
	if strings.Contains(rendered, task) {
		return text, nil
	}
	text += "\n\n{{.task}}"
	if len(text) > 100000 {
		return "", ErrInvalid
	}
	return text, nil
}
