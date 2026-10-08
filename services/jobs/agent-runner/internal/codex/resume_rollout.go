package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

var errResumeSourceInvalid = errors.New("Codex resumed rollout source is invalid")

// Locator подтверждается до resume, но не связывает исполняемый thread и usage.
// Открытый FD удерживает inode до join; новые байты проверяются отдельно.
type confirmedResumeSource struct {
	sessionID string
	path      string
	file      *os.File
	identity  os.FileInfo
}

func (server *appServer) bindExecutionThread(ctx context.Context, state *protocolState, input model.Input) error {
	params := map[string]any{"approvalPolicy": input.CodexApprovalPolicy, "cwd": input.WorkspaceRoot, "model": input.Model}
	method := "thread/start"
	if input.CodexSessionID == "" {
		params["ephemeral"] = false
		params["sessionStartSource"] = "startup"
	} else {
		raw, err := server.call(ctx, state, "thread/read", map[string]any{"threadId": input.CodexSessionID, "includeTurns": false})
		if err != nil {
			return atProviderStage(providerStageThreadRead, err)
		}
		source, err := confirmResumeSource(input, raw)
		if err != nil {
			return atProviderStage(providerStageThreadRead, err)
		}
		state.resumeSource = source
		method = "thread/resume"
		params["threadId"] = input.CodexSessionID
	}
	raw, err := server.call(ctx, state, method, params)
	if err != nil {
		return atProviderStage(providerStageThreadCall, err)
	}
	if err := state.bindThread(raw, input.Model, input.WorkspaceRoot, input.CodexApprovalPolicy); err != nil {
		return atProviderStage(providerStageThreadBind, err)
	}
	return nil
}

func confirmResumeSource(input model.Input, raw json.RawMessage) (*confirmedResumeSource, error) {
	sessionID, path, err := parseThreadRead(raw)
	if err != nil || input.CodexSessionID == "" || sessionID != input.CodexSessionID ||
		!filepath.IsAbs(input.WorkspaceRoot) || filepath.Clean(input.WorkspaceRoot) != input.WorkspaceRoot ||
		input.CodexHome != filepath.Join(input.WorkspaceRoot, ".kodex/state/codex-home") ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errResumeSourceInvalid
	}
	relative, err := filepath.Rel(input.WorkspaceRoot, path)
	if err != nil || runtimecontract.ValidateCodexArchiveIdentity(sessionID, filepath.ToSlash(relative)) != nil {
		return nil, errResumeSourceInvalid
	}
	file, info, err := openProtectedFile(input.WorkspaceRoot, path)
	if err != nil {
		return nil, errResumeSourceInvalid
	}
	if !validResumeSourceInfo(info) {
		file.Close()
		return nil, errResumeSourceInvalid
	}
	source := &confirmedResumeSource{sessionID: sessionID, path: path, file: file, identity: info}
	if source.verifyIdentity(input) != nil {
		file.Close()
		return nil, errResumeSourceInvalid
	}
	return source, nil
}

func validResumeSourceInfo(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o640 || info.Size() <= 0 || info.Size() > maximumArchiveBytes {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1 &&
		(os.Geteuid() != providerWriterUID || stat.Gid == 29000)
}

func (source *confirmedResumeSource) verifyIdentity(input model.Input) error {
	if source == nil || source.file == nil || source.sessionID != input.CodexSessionID {
		return errResumeSourceInvalid
	}
	held, err := source.file.Stat()
	if err != nil || !validResumeSourceInfo(held) || !os.SameFile(source.identity, held) {
		return errResumeSourceInvalid
	}
	current, info, err := openProtectedFile(input.WorkspaceRoot, source.path)
	if err != nil {
		return errResumeSourceInvalid
	}
	defer current.Close()
	if !validResumeSourceInfo(info) || !os.SameFile(held, info) {
		return errResumeSourceInvalid
	}
	return nil
}

// Этот метод вызывается только после captureReady. Он не восстанавливает
// предыдущие SHA/size и не заимствует locator из отвергнутого resume response.
func (source *confirmedResumeSource) capture(input model.Input, server *appServer) (Result, error) {
	if server == nil || !server.captureReady() || source.verifyIdentity(input) != nil {
		return Result{}, errResumeSourceInvalid
	}
	result, err := CaptureStoppedRollout(input, source.sessionID, source.path)
	if err != nil || source.verifyIdentity(input) != nil {
		return Result{}, errResumeSourceInvalid
	}
	return result, nil
}
