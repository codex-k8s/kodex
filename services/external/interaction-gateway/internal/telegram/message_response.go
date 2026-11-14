package telegram

import (
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// sentMessage preserves presence of optional response coordinates so explicit null cannot become an absent thread or sender; unrelated provider fields are discarded.
// sentMessage сохраняет наличие optional response coordinates, чтобы explicit null не становился отсутствующим thread либо sender; посторонние provider fields отбрасываются.
type sentMessage struct {
	MessageID       int64           `json:"message_id"`
	MessageThreadID json.RawMessage `json:"message_thread_id"`
	Chat            scopedChat      `json:"chat"`
	From            json.RawMessage `json:"from"`
	SenderChat      json.RawMessage `json:"sender_chat"`
	Text            string          `json:"text"`
}

// verifiedSentMessage binds one successful send response to fresh bot/chat evidence and the exact request; malformed consumed fields preserve UNKNOWN rather than granting a retry or a success receipt.
// verifiedSentMessage связывает один successful send response со свежим bot/chat evidence и точным request; неверные consumed fields сохраняют UNKNOWN вместо retry либо success receipt.
func verifiedSentMessage(raw []byte, proof scopeProof, configuration integrationpackage.TelegramConfiguration, text string) (int64, error) {
	unknown := &SafeError{Code: errorUnknown}
	var message sentMessage
	if !uniqueJSON(raw) || json.Unmarshal(raw, &message) != nil || message.MessageID < 1 || message.MessageID > maximumIdentifier ||
		proof.bot.ID != configuration.BotID || !proof.bot.IsBot || proof.chat.ID != configuration.ChatID ||
		message.Chat.ID != configuration.ChatID || message.Chat.Type != proof.chat.Type || message.Text != text {
		return 0, unknown
	}
	if len(message.MessageThreadID) == 0 {
		if configuration.TopicID != 0 {
			return 0, unknown
		}
	} else {
		var thread int64
		if json.Unmarshal(message.MessageThreadID, &thread) != nil || thread < 1 || thread != configuration.TopicID {
			return 0, unknown
		}
	}
	if len(message.From) != 0 {
		var from botIdentity
		if json.Unmarshal(message.From, &from) != nil || from.ID != proof.bot.ID || !from.IsBot {
			return 0, unknown
		}
	}
	if len(message.SenderChat) != 0 {
		var sender scopedChat
		if json.Unmarshal(message.SenderChat, &sender) != nil || proof.chat.Type != "channel" || sender.ID != configuration.ChatID || sender.Type != "channel" {
			return 0, unknown
		}
	}
	if proof.chat.Type == "channel" {
		if len(message.From) == 0 && len(message.SenderChat) == 0 {
			return 0, unknown
		}
	} else if len(message.From) == 0 {
		return 0, unknown
	}
	return message.MessageID, nil
}
