// Package systemassistant предоставляет неизменяемую поставляемую платформой
// часть инструкций системного помощника.
package systemassistant

import _ "embed"

// CorePromptRevision identifies the shipped immutable core prompt revision; it does not select
// actor permissions or mutable owner additions.
// CorePromptRevision обозначает поставляемую immutable ревизию core prompt; полномочия actor либо
// изменяемые дополнения владельца не выбирает.
const CorePromptRevision = "system-assistant-core-v54"

// corePrompt stores the shipped v54 instructions as embedded bytes without reading owner settings.
// corePrompt хранит поставляемые v54 инструкции как встроенный текст без чтения настроек владельца.
//
//go:embed prompts/system-assistant-core-v54.md
var corePrompt string

// CorePrompt returns the embedded core instructions unchanged; owner additions remain separate and
// cannot replace the platform-owned core.
// CorePrompt возвращает встроенные core instructions без изменений; дополнения владельца остаются
// отдельно и не заменяют поставляемую платформой основу.
func CorePrompt() string {
	return corePrompt
}
