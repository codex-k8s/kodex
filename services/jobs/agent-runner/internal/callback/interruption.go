package callback

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// runtimeInterruptedSummary describes a cancelled provider wait without asserting the external
// provider or a particular environment resource caused the interruption.
// runtimeInterruptedSummary описывает отменённое ожидание без утверждения, что причиной стал
// внешний провайдер либо конкретный ресурс окружения.
const runtimeInterruptedSummary = "i18n:RUNTIME_INTERRUPTED"

// interruptedCompletion normalizes only a validated, exact execution diagnostic before the one
// serialized delivery body is frozen. It never inspects a mutable Pod, changes successful or
// specific failures, or alters usage, artifacts, archive pins and the diagnostic itself.
// interruptedCompletion нормализует только проверенную exact execution diagnostic до фиксации
// единственного сериализованного тела доставки. Она не читает изменяемый Pod, не меняет успех
// либо специфические отказы, usage, artifacts, archive pins и саму диагностику.
func interruptedCompletion(input model.Input, payload runtimecontract.RunnerCompletionRequest) runtimecontract.RunnerCompletionRequest {
	if payload.Success || payload.SafeErrorCode != "PROVIDER_UNAVAILABLE" ||
		payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest || payload.Attempt != input.Attempt {
		return payload
	}
	diagnostic := payload.ProviderDiagnostic
	if diagnostic == nil || !diagnostic.Matches(input) || diagnostic.Kind != "REQUEST_FAILURE" ||
		diagnostic.Stage != "TERMINAL_WAIT" || diagnostic.Class != "PROVIDER" || diagnostic.Detail != "CONTEXT_CANCELLED" {
		return payload
	}
	payload.SafeErrorCode = "RUNTIME_UNAVAILABLE"
	payload.ResultSummary = runtimeInterruptedSummary
	return payload
}
