package runtimecontract

import (
	"strings"
	"testing"
)

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
