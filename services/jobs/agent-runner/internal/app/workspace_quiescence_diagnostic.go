package app

import (
	"encoding/json"
	"errors"
	"os"
)

// workspaceQuiescenceError retains a closed stage and an unexported causal chain.
// Neither Error nor the termination diagnostic serializes the underlying error.
// workspaceQuiescenceError сохраняет закрытый этап и непубличную цепочку причины.
// Ни Error, ни termination diagnostic не сериализуют исходную ошибку.
type workspaceQuiescenceError struct {
	stage string
	cause error
}

// Error exposes no filesystem, transport or provider data.
// Error не раскрывает filesystem, transport либо provider data.
func (*workspaceQuiescenceError) Error() string { return "runtime workspace quiescence is unconfirmed" }

// Unwrap preserves cancellation classification without publishing its text.
// Unwrap сохраняет классификацию отмены без публикации её текста.
func (failure *workspaceQuiescenceError) Unwrap() error { return failure.cause }

// rejectedWorkspaceQuiescence records the existing safe log exactly at the rejecting boundary.
// rejectedWorkspaceQuiescence пишет прежний безопасный log ровно на границе отказа.
func rejectedWorkspaceQuiescence(stage string, cause error) error {
	stage = closedWorkspaceQuiescenceStage(stage)
	logWorkspaceQuiescenceRejection(stage)
	return &workspaceQuiescenceError{stage: stage, cause: cause}
}

// recordWorkspaceQuiescenceFailure is a best-effort trusted-role diagnostic, never a join proof.
// It does not create files or replace an existing positive record, and cannot change the failure.
// recordWorkspaceQuiescenceFailure — best-effort диагностика trusted роли, никогда не join proof.
// Файлов не создаёт, positive record не заменяет и исходный отказ не изменяет.
func recordWorkspaceQuiescenceFailure(cause error) error {
	if os.Geteuid() != 10001 {
		return os.ErrPermission
	}
	return writeWorkspaceQuiescenceFailure(initializationTerminationPath, cause)
}

// writeWorkspaceQuiescenceFailure writes only two closed fields through the existing descriptor guard.
// Its distinct schema is deliberately invalid as WorkspaceQuiescence and grants no authority.
// writeWorkspaceQuiescenceFailure пишет только два закрытых поля через прежний descriptor guard.
// Отдельная schema намеренно недопустима как WorkspaceQuiescence и не выдаёт полномочий.
func writeWorkspaceQuiescenceFailure(path string, cause error) error {
	stage := "UNKNOWN"
	var failure *workspaceQuiescenceError
	if errors.As(cause, &failure) && failure != nil {
		stage = closedWorkspaceQuiescenceStage(failure.stage)
	}
	raw, err := json.Marshal(struct {
		Schema string `json:"schema"`
		Stage  string `json:"stage"`
	}{Schema: "kodex.workspace-quiescence-failure.v1", Stage: stage})
	if err != nil {
		return err
	}
	return writeTerminationRecord(path, raw, true)
}
