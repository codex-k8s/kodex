// Package systemassistant предоставляет неизменяемую поставляемую платформой
// часть инструкций системного помощника.
package systemassistant

import _ "embed"

const CorePromptRevision = "system-assistant-core-v19"

//go:embed prompts/system-assistant-core-v19.md
var corePrompt string

// CorePrompt возвращает versioned core prompt с контекстными границами шаблонов.
// Дополнение владельца хранится отдельно и не может заменить эту часть.
func CorePrompt() string { return corePrompt }
