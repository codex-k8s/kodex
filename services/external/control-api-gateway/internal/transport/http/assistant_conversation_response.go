package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// assistantConversationStateResponse restricts state-result projection to the two specialized archive/restore owner replies. The concrete RPC and mutation authority remain in each endpoint; this constraint is not a generic lifecycle command registry.
// assistantConversationStateResponse ограничивает проекцию результата двумя отдельными owner replies archive/restore. Конкретный RPC и полномочия mutation остаются в endpoint; constraint не является generic реестром lifecycle-команд.
type assistantConversationStateResponse interface {
	*controlplanev1.ArchiveAssistantConversationResponse | *controlplanev1.RestoreAssistantConversationResponse
	proto.Message

	// GetConversation reads the exact returned conversation; both admitted protobuf pointer types safely return nil from a typed-nil receiver.
	// GetConversation читает точный диалог ответа; оба допустимых protobuf pointer types безопасно возвращают nil для типизированного nil receiver.
	GetConversation() *controlplanev1.AssistantConversation
}

// writeAssistantConversationState checks the exact requested reference, positive safe version and the specialized caller's expected state before the existing public projection. It handles only a successful RPC reply, performs no mutation or retry, and does not infer authority from the supplied state.
// writeAssistantConversationState сверяет точную запрошенную ссылку, положительную safe version и ожидаемый state специализированного вызывающего кода до прежней публичной проекции. Обрабатывает только успешный RPC reply, не выполняет mutation и retry, не выводит полномочия из переданного state.
func writeAssistantConversationState[R assistantConversationStateResponse](w http.ResponseWriter, response R, ref string, expectedState controlplanev1.AssistantConversationState) {
	conversation := response.GetConversation()
	if conversation == nil || conversation.Ref != ref || !validManagedVersion(conversation.Version) || conversation.State != expectedState {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "conversation", "")
}
