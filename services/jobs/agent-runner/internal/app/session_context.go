package app

import (
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// appendSessionContext validates owner-provided context but sends only a fresh revision notice; the native transcript owns prior messages.
// appendSessionContext проверяет owner context, отправляя лишь свежую revision notice; прежними сообщениями владеет native transcript.
func appendSessionContext(builder *strings.Builder, input model.Input) (bool, error) {
	messages := input.SessionContext
	if len(messages) == 0 {
		return false, nil
	}
	notice, err := runtimecontract.ValidateRuntimeSessionContext(input)
	if err != nil {
		return false, err
	}
	if notice {
		builder.WriteString(messages[len(messages)-1].Content)
		builder.WriteByte('\n')
	}
	return notice, nil
}
