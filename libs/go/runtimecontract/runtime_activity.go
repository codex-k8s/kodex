package runtimecontract

import (
	"errors"
	"unicode/utf8"
)

const (
	// MaximumRuntimeMessageBytes bounds one published agent message.
	// MaximumRuntimeMessageBytes ограничивает размер одного опубликованного сообщения агента.
	MaximumRuntimeMessageBytes = 64 << 10
	// RuntimeMessageCommentary identifies an intermediate published reply.
	// RuntimeMessageCommentary обозначает опубликованный промежуточный ответ.
	RuntimeMessageCommentary = "COMMENTARY"
	// RuntimeMessageFinal identifies a published final reply.
	// RuntimeMessageFinal обозначает опубликованный итоговый ответ.
	RuntimeMessageFinal = "FINAL"
)

// RuntimeAgentMessage contains only a completed published item, excluding reasoning,
// raw protocol envelopes, and service credentials.
// RuntimeAgentMessage содержит лишь завершённый опубликованный элемент без рассуждений,
// сырых протокольных конвертов и служебных учётных данных.
type RuntimeAgentMessage struct {
	ItemID   string `json:"item_id"`
	Phase    string `json:"phase"`
	Revision int64  `json:"revision"`
	Text     string `json:"text"`
}

// Validate rejects unpublished phases, invalid item identity, encoding, and oversized text.
// Validate отклоняет неразрешённые фазы, неверный идентификатор, кодировку и слишком большой текст.
func (message RuntimeAgentMessage) Validate() error {
	if !nativeToolCallIDPattern.MatchString(message.ItemID) ||
		(message.Phase != RuntimeMessageCommentary && message.Phase != RuntimeMessageFinal) ||
		message.Revision != 1 || len(message.Text) == 0 || len(message.Text) > MaximumRuntimeMessageBytes ||
		!utf8.ValidString(message.Text) {
		return errors.New("runtime published message is invalid")
	}
	return nil
}

// RuntimeActivity is a closed union of safe provider-broker observations.
// RuntimeActivity — закрытое объединение безопасных наблюдений от provider broker.
type RuntimeActivity struct {
	Message         *RuntimeAgentMessage          `json:"message,omitempty"`
	ToolCall        *NativeToolCall               `json:"tool_call,omitempty"`
	ProviderProcess *ProviderProcessObservation   `json:"provider_process,omitempty"`
	Financial       *ProviderFinancialObservation `json:"financial,omitempty"`
}

// Validate requires exactly one activity variant and validates its own envelope.
// Validate требует ровно один вариант события и проверяет его собственный конверт.
func (activity RuntimeActivity) Validate() error {
	count := 0
	for _, present := range []bool{activity.Message != nil, activity.ToolCall != nil, activity.ProviderProcess != nil, activity.Financial != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return errors.New("runtime activity is invalid")
	}
	if activity.Financial != nil {
		return activity.Financial.Validate()
	}
	if activity.Message != nil {
		return activity.Message.Validate()
	}
	if activity.ProviderProcess != nil {
		return activity.ProviderProcess.Validate()
	}
	return activity.ToolCall.Validate()
}
