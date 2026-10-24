package prompt

import (
	"encoding/json"
	"strings"
)

// AutomationTaskPurpose validates and renders the owner template once to detect the exact captured
// task, appending one task insertion only if absent and keeping template syntax inside task values
// inert.
// AutomationTaskPurpose проверяет и один раз строит шаблон владельца для поиска точной захваченной
// задачи, добавляя одну вставку task только при её отсутствии и не исполняя синтаксис шаблона
// внутри значения задачи.
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
