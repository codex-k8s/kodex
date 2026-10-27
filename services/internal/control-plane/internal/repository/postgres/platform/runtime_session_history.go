package platform

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// maximumRuntimeSessionHistoryBytes limits the JSON-encoded retained history suffix to512 KiB
	// including escaping and envelope overhead. It is an output serialization budget, not permission
	// to truncate an individual message.
	// maximumRuntimeSessionHistoryBytes ограничивает JSON-кодирование сохраняемого суффикса истории512
	// КиБ с учётом escaping и оболочки. Это бюджет сериализации результата, а не разрешение обрезать
	// отдельное сообщение.
	maximumRuntimeSessionHistoryBytes = 512 << 10
	// maximumRuntimeSessionHistoryMessages caps the retained contiguous newest history suffix at20
	// messages. Invalid or oversized entries stop traversal rather than allowing older messages to
	// bypass the gap.
	// maximumRuntimeSessionHistoryMessages ограничивает непрерывный новейший суффикс истории20
	// сообщениями. Неверная либо слишком большая запись останавливает обход, не позволяя старым
	// сообщениям обойти разрыв.
	maximumRuntimeSessionHistoryMessages = 20
)

// boundedRuntimeSessionHistory walks backward and returns fresh role/content maps for at most20
// whole valid USER/ASSISTANT messages in original order. It stops at the first invalid message or
// JSON budget overflow, retaining a contiguous newest suffix; USER uses the shared turn contract
// and ASSISTANT requires UTF-8 and at most64 KiB. It does not mutate input, clip content or assign
// task/authority from recency.
// boundedRuntimeSessionHistory обходит историю назад и возвращает новые role/content карты
// максимум20 полных допустимых USER/ASSISTANT сообщений в исходном порядке. Останавливается на
// первой неверной записи либо превышении JSON-бюджета, сохраняя непрерывный новейший суффикс; USER
// проходит общий turn contract, ASSISTANT требует UTF-8 и максимум64 КиБ. Не меняет вход, не
// обрезает содержимое и не назначает задачу либо права по новизне.
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
