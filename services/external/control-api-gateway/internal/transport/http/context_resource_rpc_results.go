package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// skillBundleRPCReply exposes only the bundle from an already completed typed owner RPC;
// the result boundary must handle an accompanying error before reading this projection.
// skillBundleRPCReply предоставляет только bundle уже завершённого типизированного owner RPC;
// граница результата обязана обработать сопровождающую ошибку до чтения проекции.
type skillBundleRPCReply interface {
	// GetBundle returns owner metadata for the existing strict bundle writer; it establishes no authority.
	// GetBundle возвращает metadata владельца существующему строгому writer bundle, не назначая полномочий.
	GetBundle() *controlplanev1.SkillBundle
}

// memoryRecordRPCReply exposes only an already completed memory reply and does not replace the
// memory-specific redaction, reference, version and timestamp checks of the existing writer.
// memoryRecordRPCReply предоставляет только завершённый ответ памяти, не заменяя специальные
// проверки redaction, ссылки, версии и времени существующим writer.
type memoryRecordRPCReply interface {
	// GetRecord is read only after the RPC error has been ruled out and may still return invalid metadata.
	// GetRecord читается лишь при отсутствии RPC-ошибки и всё ещё может вернуть неверные metadata.
	GetRecord() *controlplanev1.KodexMemoryRecord
}

// agentContextBindingRPCReply narrows the reply to its binding; the caller retains exact
// agent/resource/revision pins and the existing writer validates all of them before output.
// agentContextBindingRPCReply ограничивает ответ его binding; вызывающий сохраняет точные
// agent/resource/revision pins, а существующий writer проверяет их до выдачи ответа.
type agentContextBindingRPCReply interface {
	// GetBinding returns the owner projection after error handling, without issuing a binding or grant.
	// GetBinding возвращает проекцию владельца после обработки ошибки, не создавая binding или grant.
	GetBinding() *controlplanev1.AgentContextBinding
}

// writeSkillBundleRPCResult preserves error-before-getter ordering and the caller's exact
// success status/reference, delegating every view/ETag check to the existing bundle writer.
// The RPC has already completed; no command, retry, permission or lifecycle is selected here.
// writeSkillBundleRPCResult сохраняет порядок error-before-getter и точные status/reference
// вызывающего, передавая все проверки view/ETag существующему writer bundle. RPC уже завершён;
// команда, повтор, permission или lifecycle здесь не выбираются.
func writeSkillBundleRPCResult(w http.ResponseWriter, response skillBundleRPCReply, err error, ref string, status int) {
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), ref, status)
}

// writeMemoryRecordRPCResult rejects an RPC failure before inspecting a partial reply and
// otherwise preserves the memory writer, requested reference and caller-selected status/ETag.
// It performs no additional read, write, retry or owner resolution.
// writeMemoryRecordRPCResult обрабатывает RPC-ошибку до чтения частичного ответа, иначе
// сохраняет writer памяти, запрошенную ссылку и выбранные вызывающим status/ETag.
// Дополнительных чтений, записей, повторов или разрешения владельца нет.
func writeMemoryRecordRPCResult(w http.ResponseWriter, response memoryRecordRPCReply, err error, ref string, status int) {
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMemoryRecord(w, response.GetRecord(), ref, status)
}

// writeAgentContextBindingRPCResult handles the RPC error first and delegates successful
// projection to the existing writer with exact agent/resource/revision pins. That writer
// intentionally emits no agent ETag; this helper neither changes the binding nor authorizes it.
// writeAgentContextBindingRPCResult сначала обрабатывает RPC-ошибку, затем передаёт проекцию
// существующему writer с точными agent/resource/revision pins. Этот writer намеренно не выдаёт
// ETag агента; helper не меняет binding и не назначает полномочий на него.
func writeAgentContextBindingRPCResult(w http.ResponseWriter, response agentContextBindingRPCReply, err error, agent, resource, revision string) {
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeAgentContextBinding(w, response.GetBinding(), agent, resource, revision)
}
