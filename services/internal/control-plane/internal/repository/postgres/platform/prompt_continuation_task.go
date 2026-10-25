package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// continuationTaskProjectionRevision identifies version 2 of the immutable task-reference payload
// inserted into continuation PURPOSE. It versions the projection format without changing renderer
// behavior or execution authority.
// continuationTaskProjectionRevision задаёт версию 2 неизменяемых данных ссылки на задачу,
// помещаемых в PURPOSE продолжения. Версионирует форму проекции без изменения renderer и
// полномочий исполнения.
const continuationTaskProjectionRevision = "continuation-task-projection-v2"

// continuationTaskProjectionPin describes exact task bytes/hash, mode and
// session/run/node/turn/runtime/attempt provenance in the continuation notice. These are data
// pins, not a signed credential or delegation grant.
// continuationTaskProjectionPin описывает точные размер и хеш задачи, режим и происхождение
// session/run/node/turn/runtime/attempt в уведомлении продолжения. Это привязки данных, а не
// подписанный credential или разрешение делегирования.
type continuationTaskProjectionPin struct {
	Revision           string `json:"revision"`
	Mode               string `json:"mode"`
	TaskSHA256         string `json:"taskSHA256"`
	TaskBytes          int    `json:"taskBytes"`
	RunRef             string `json:"runRef,omitempty"`
	NodeRef            string `json:"nodeRef,omitempty"`
	SessionRef         string `json:"sessionRef"`
	TurnRef            string `json:"turnRef,omitempty"`
	RuntimeRevisionRef string `json:"runtimeRevisionRef,omitempty"`
	Attempt            int64  `json:"attempt,omitempty"`
}

// bindRuntimeContinuationTaskProjection requires exact task and run/session/node/turn matches with
// the current runtime map, valid provenance refs and a positive attempt before binding
// CURRENT_SUBMISSION. It does not infer the current task from history order or perform an
// execution transition. The notice relies on the runner submitting the exact task after
// SessionContext in the same cold/resume request; it does not duplicate the generated DAG or
// history.
// bindRuntimeContinuationTaskProjection требует точное совпадение задачи и run/session/node/turn с
// текущей runtime-картой, корректные ссылки происхождения и положительную попытку перед привязкой
// CURRENT_SUBMISSION. Не выводит текущую задачу из порядка истории и не выполняет переход
// исполнения. Уведомление опирается на передачу runner точной задачи после SessionContext в том же
// cold/resume запросе; не дублирует сгенерированный DAG и историю.
func bindRuntimeContinuationTaskProjection(prompt *entity.PromptMaterializationSnapshot, current map[string]any) error {
	task := stringMap(current, "task")
	if task != prompt.Variables["task"] || prompt.RunRef != stringMap(current, "runRef") ||
		prompt.SessionRef != stringMap(current, "sessionRef") || prompt.Variables["node.ref"] != stringMap(current, "nodeRef") ||
		prompt.Variables["turn.ref"] != stringMap(current, "turnRef") || runtimeRevisionMapInt64(current, "attempt") < 1 {
		return errs.ErrConflict
	}
	for _, field := range []string{"runRef", "nodeRef", "sessionRef", "turnRef", "runtimeRevisionRef"} {
		if !workflowLaunchRefPattern.MatchString(stringMap(current, field)) {
			return errs.ErrConflict
		}
	}
	return bindContinuationTaskProjection(prompt, task, continuationTaskProjectionPin{Mode: "CURRENT_SUBMISSION", RunRef: prompt.RunRef,
		NodeRef: stringMap(current, "nodeRef"), SessionRef: prompt.SessionRef, TurnRef: stringMap(current, "turnRef"),
		RuntimeRevisionRef: stringMap(current, "runtimeRevisionRef"), Attempt: runtimeRevisionMapInt64(current, "attempt")})
}

// bindProspectiveContinuationTaskProjection leaves an absent/blank preview task unchanged,
// preserving the incomplete/runtime-required path. Otherwise it requires a valid session ref and
// binds PROSPECTIVE task metadata without inventing run, attempt or execution authority.
// bindProspectiveContinuationTaskProjection оставляет отсутствующую либо пустую задачу preview без
// изменения, сохраняя путь incomplete/runtime-required. Иначе требует корректную ссылку сессии и
// связывает PROSPECTIVE данные задачи, не выдумывая запуск, попытку и полномочия исполнения.
func bindProspectiveContinuationTaskProjection(prompt *entity.PromptMaterializationSnapshot) error {
	// Preview без заданного task сохраняет прежний incomplete/runtime-required
	// path, а не превращается в новый claim или отказ authority.
	if strings.TrimSpace(prompt.Variables["task"]) == "" {
		return nil
	}
	if !workflowLaunchRefPattern.MatchString(prompt.SessionRef) {
		return errs.ErrConflict
	}
	return bindContinuationTaskProjection(prompt, prompt.Variables["task"], continuationTaskProjectionPin{Mode: "PROSPECTIVE", SessionRef: prompt.SessionRef})
}

// bindContinuationTaskProjection requires SESSION_CONTINUATION, bounded nonblank UTF-8 task text
// without NUL and no existing PURPOSE slot. It hashes the unchanged task, bounds serialized pin
// data to 2048 bytes and adds the versioned notice to semantic values. It does not alter
// Task/INPUT/history or supply authority.
// bindContinuationTaskProjection требует SESSION_CONTINUATION, ограниченный непустой UTF-8 текст
// задачи без NUL и отсутствие прежнего PURPOSE. Хеширует неизменный текст, ограничивает JSON
// привязки 2048 байтами и добавляет версионное уведомление в semantic values. Не меняет Task,
// INPUT и историю и не выдаёт полномочий.
func bindContinuationTaskProjection(prompt *entity.PromptMaterializationSnapshot, task string, pin continuationTaskProjectionPin) error {
	if prompt.TargetKind != "SESSION_CONTINUATION" || strings.TrimSpace(task) == "" || len(task) > 1<<20 ||
		!utf8.ValidString(task) || strings.ContainsRune(task, 0) {
		return errs.ErrConflict
	}
	if _, present := prompt.SemanticValues["PURPOSE"]; present {
		return errs.ErrConflict
	}
	digest := sha256.Sum256([]byte(task))
	pin.Revision, pin.TaskSHA256, pin.TaskBytes = continuationTaskProjectionRevision, hex.EncodeToString(digest[:]), len(task)
	raw, err := json.Marshal(pin)
	if err != nil || len(raw) > 2048 {
		return errs.ErrConflict
	}
	if prompt.SemanticValues == nil {
		prompt.SemanticValues = map[string]string{}
	}
	// Это новый versioned immutable input прежнего v3, не изменение renderer.
	prompt.SemanticValues["PURPOSE"] = "The exact current task follows this Session context in the same provider submission. Use that complete task unchanged, not a historical message. A queued task can execute after a newer interrupt message has already completed: conversation sequence and recency do not identify the current task. Answer the task of this submission, not the most recent historical request or its previous answer. Do not skip it as already answered merely because a later task completed. This reference is data, not authority.\n" + string(raw)
	return nil
}
