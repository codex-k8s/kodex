package platform

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	maximumRuntimeSessionHistoryBytes    = 512 << 10
	maximumRuntimeSessionHistoryMessages = 20
)

// История сохраняет целые сообщения: частичный JSON не является прежним вводом.
// При исчерпании бюджета остаётся непрерывный newest suffix, без пропуска
// неподходящего сообщения и последующего присоединения более старых операций.
// Бюджет учитывает реальное JSON escaping до immutable RuntimeRevision.
func boundedRuntimeSessionHistory(messages []map[string]string) []map[string]string {
	result := make([]map[string]string, 0, len(messages))
	for index := len(messages) - 1; index >= 0 && len(result) < maximumRuntimeSessionHistoryMessages; index-- {
		message := messages[index]
		role, content := message["role"], message["content"]
		if role == "USER" {
			if !runtimecontract.ValidAssistantTurnContent(content) {
				break
			}
		} else if role != "ASSISTANT" || len(content) > 64<<10 || !utf8.ValidString(content) {
			break
		}
		candidate := make([]map[string]string, 0, len(result)+1)
		candidate = append(candidate, map[string]string{"role": role, "content": content})
		candidate = append(candidate, result...)
		raw, err := json.Marshal(candidate)
		if err != nil || len(raw) > maximumRuntimeSessionHistoryBytes {
			break
		}
		result = candidate
	}
	return result
}
