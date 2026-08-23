// Package systemassistant предоставляет неизменяемую поставляемую платформой
// часть инструкций системного помощника.
package systemassistant

import _ "embed"

const CorePromptRevision = "system-assistant-core-v1"

//go:embed prompts/system-assistant-core-v1.md
var corePrompt string

// CorePrompt возвращает versioned core prompt. Дополнение владельца хранится
// отдельно и не может заменить эту часть.
func CorePrompt() string { return corePrompt }
