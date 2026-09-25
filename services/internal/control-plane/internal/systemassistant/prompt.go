// Package systemassistant предоставляет неизменяемую поставляемую платформой
// часть инструкций системного помощника.
package systemassistant

import _ "embed"

const CorePromptRevision = "system-assistant-core-v28"

//go:embed prompts/system-assistant-core-v21.md
var corePrompt string

//go:embed prompts/system-assistant-core-v22-addendum.md
var corePromptV22Addendum string

//go:embed prompts/system-assistant-core-v23-addendum.md
var corePromptV23Addendum string

//go:embed prompts/system-assistant-core-v25-addendum.md
var corePromptV25Addendum string

//go:embed prompts/system-assistant-core-v26-addendum.md
var corePromptV26Addendum string

//go:embed prompts/system-assistant-core-v27-addendum.md
var corePromptV27Addendum string

//go:embed prompts/system-assistant-core-v28-addendum.md
var corePromptV28Addendum string

// CorePrompt возвращает versioned core prompt с контекстными границами шаблонов.
// Дополнение владельца хранится отдельно и не может заменить эту часть.
func CorePrompt() string {
	return corePrompt + "\n\n" + corePromptV22Addendum + "\n\n" + corePromptV23Addendum + "\n\n" + corePromptV25Addendum + "\n\n" + corePromptV26Addendum + "\n\n" + corePromptV27Addendum + "\n\n" + corePromptV28Addendum
}
