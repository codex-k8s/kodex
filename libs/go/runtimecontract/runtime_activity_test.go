package runtimecontract

import (
	"strings"
	"testing"
)

// TestRuntimeActivityAcceptsOnlyPublishedBoundedMessage accepts a bounded public commentary message while rejecting private phases, invalid text and empty or competing activity variants.
// TestRuntimeActivityAcceptsOnlyPublishedBoundedMessage принимает ограниченное публичное промежуточное сообщение, отклоняя частные фазы, некорректный текст и пустые либо конкурирующие варианты активности.
func TestRuntimeActivityAcceptsOnlyPublishedBoundedMessage(t *testing.T) {
	valid := RuntimeAgentMessage{ItemID: "message-1", Phase: RuntimeMessageCommentary, Revision: 1, Text: "Проверяю доступные файлы"}
	if (RuntimeActivity{Message: &valid}).Validate() != nil {
		t.Fatal("valid published message rejected")
	}
	for _, phase := range []string{"", "USER", "analysis", "reasoning", "REASONING"} {
		invalid := valid
		invalid.Phase = phase
		if invalid.Validate() == nil {
			t.Fatal("private or invalid phase accepted")
		}
	}
	for _, body := range []string{"", strings.Repeat("a", MaximumRuntimeMessageBytes+1), string([]byte{0xff})} {
		invalid := valid
		invalid.Text = body
		if invalid.Validate() == nil {
			t.Fatal("invalid message body accepted")
		}
	}
	if (RuntimeActivity{}).Validate() == nil {
		t.Fatal("empty activity accepted")
	}
	if (RuntimeActivity{Message: &valid, ToolCall: &NativeToolCall{}}).Validate() == nil {
		t.Fatal("ambiguous activity accepted")
	}
}

// TestRuntimeActivityFinancialVariantStaysExclusiveWithProcessObservation accepts a standalone financial observation but rejects its combination with messages, tool calls or process observations.
// TestRuntimeActivityFinancialVariantStaysExclusiveWithProcessObservation принимает отдельное финансовое наблюдение, но отклоняет его сочетание с сообщениями, инструментами или наблюдениями процесса.
func TestRuntimeActivityFinancialVariantStaysExclusiveWithProcessObservation(t *testing.T) {
	financial := ProviderFinancialObservation{Sequence: 1, State: "UNAVAILABLE"}
	if err := (RuntimeActivity{Financial: &financial}).Validate(); err != nil {
		t.Fatalf("valid financial activity rejected: %v", err)
	}
	message := RuntimeAgentMessage{ItemID: "message-1", Phase: RuntimeMessageCommentary, Revision: 1, Text: "Проверяю файлы"}
	for _, activity := range []RuntimeActivity{
		{Financial: &financial, Message: &message},
		{Financial: &financial, ToolCall: &NativeToolCall{}},
		{Financial: &financial, ProviderProcess: &ProviderProcessObservation{}},
		{Financial: &financial, Message: &message, ProviderProcess: &ProviderProcessObservation{}},
	} {
		if activity.Validate() == nil {
			t.Fatal("financial activity accepted another runtime variant")
		}
	}
}
