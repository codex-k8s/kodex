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

const continuationTaskProjectionRevision = "continuation-task-projection-v2"

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

// Runner всегда добавляет exact Task после SessionContext той же submission
// (app.buildPrompt), включая cold и resume. Notice ссылается на эти полные
// bytes, не дублирует generated DAG и не меняет Task, INPUT или USER/history.
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
