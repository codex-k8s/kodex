package runtimecontract

import (
	"errors"
	"unicode/utf8"
)

const (
	MaximumRuntimeMessageBytes = 64 << 10
	RuntimeMessageCommentary   = "COMMENTARY"
	RuntimeMessageFinal        = "FINAL"
)

// RuntimeAgentMessage содержит только опубликованный completed item.
// Reasoning, сырой protocol envelope и служебные credentials не передаются.
type RuntimeAgentMessage struct {
	ItemID   string `json:"item_id"`
	Phase    string `json:"phase"`
	Revision int64  `json:"revision"`
	Text     string `json:"text"`
}

func (message RuntimeAgentMessage) Validate() error {
	if !nativeToolCallIDPattern.MatchString(message.ItemID) ||
		(message.Phase != RuntimeMessageCommentary && message.Phase != RuntimeMessageFinal) ||
		message.Revision != 1 || len(message.Text) == 0 || len(message.Text) > MaximumRuntimeMessageBytes ||
		!utf8.ValidString(message.Text) {
		return errors.New("runtime published message is invalid")
	}
	return nil
}

// RuntimeActivity — закрытое объединение безопасных событий provider broker.
type RuntimeActivity struct {
	Message         *RuntimeAgentMessage        `json:"message,omitempty"`
	ToolCall        *NativeToolCall             `json:"tool_call,omitempty"`
	ProviderProcess *ProviderProcessObservation `json:"provider_process,omitempty"`
}

func (activity RuntimeActivity) Validate() error {
	count := 0
	for _, present := range []bool{activity.Message != nil, activity.ToolCall != nil, activity.ProviderProcess != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return errors.New("runtime activity is invalid")
	}
	if activity.Message != nil {
		return activity.Message.Validate()
	}
	if activity.ProviderProcess != nil {
		return activity.ProviderProcess.Validate()
	}
	return activity.ToolCall.Validate()
}
