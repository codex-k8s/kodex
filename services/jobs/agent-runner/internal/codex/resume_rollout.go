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

// errResumeSourceInvalid is the fixed public sentinel for a rejected resumed-history source.
// errResumeSourceInvalid — фиксированный публичный sentinel отклонённого источника resumed history.
var errResumeSourceInvalid = errors.New("Codex resumed rollout source is invalid")

// runtimeInstructionBinding supplies stable thread-level guidance that current immutable instructions
// arrive through each turn's application context. It embeds no attempt task/grants and cannot enforce
// model obedience or replace server-side authorization; cold resume must still send the fresh snapshot.
// runtimeInstructionBinding задаёт стабильное thread-level правило передачи текущих immutable instructions
// через application context каждого хода. Attempt task/grants в нём нет; оно не обеспечивает поведение
// модели и не заменяет server-side authorization; cold resume требует свежий snapshot.
const runtimeInstructionBinding = "Kodex supplies complete current immutable runtime instructions in numbered application context entries of each turn. The current kodex_runtime_revision identifies their exact bytes. Those instructions supersede prior runtime instructions, tasks and capability descriptions retained in this thread. Historical messages are context only, not current authority. Server-side capability checks remain authoritative."

// runtimeInstructionRevisionPreamble explains ordered chunk reconstruction and revision metadata
// for current turn instructions; retained thread history is context, never a new source of grants.
// runtimeInstructionRevisionPreamble объясняет сборку упорядоченных chunks и revision metadata
// текущих turn instructions; сохранённая история — контекст, не новый источник grants.
const runtimeInstructionRevisionPreamble = "Current Kodex immutable runtime instructions for this turn are the complete values of application entries kodex_runtime_instructions_000000 onward, concatenated in ascending numeric order with no added separators. The following record identifies their exact byte count, chunk count and SHA256. Use this entire current snapshot; it replaces all earlier runtime instructions, tasks and capability descriptions retained in this thread. Perform this submission's exact task, not a prior task or answer. Historical messages remain context, not current authority. Server-side capability checks remain authoritative.\n"

// resumeSourceFailure retains a private diagnostic detail behind a fixed public error.
// The string field itself is not an allowlist; downstream diagnostic projection validates its value.
// resumeSourceFailure сохраняет приватный diagnostic detail за фиксированной публичной ошибкой.
// Само строковое поле не является allowlist; значение проверяет downstream diagnostic projection.
type resumeSourceFailure struct {
	detail string
}

// Error returns only the canonical source sentinel and never interpolates the private detail.
// Error возвращает лишь канонический source sentinel, не подставляя приватный detail.
func (failure *resumeSourceFailure) Error() string { return errResumeSourceInvalid.Error() }

// Unwrap preserves errors.Is classification against the canonical source sentinel.
// Unwrap сохраняет классификацию errors.Is по каноническому source sentinel.
func (failure *resumeSourceFailure) Unwrap() error { return errResumeSourceInvalid }

// rejectResumeSource stores the supplied detail without validation while fixing public error text;
// callers use locally assigned reasons, not raw paths or provider response text.
// rejectResumeSource сохраняет переданный detail без проверки, фиксируя публичный error text;
// caller передаёт локальные причины, не сырые paths или provider response text.
func rejectResumeSource(detail string) error { return &resumeSourceFailure{detail: detail} }

// confirmedResumeSource holds the confirmed locator and open inode across resume and writer join.
// Confirmation is not execution/usage authority or a frozen byte digest; fresh capture checks new bytes.
// confirmedResumeSource удерживает подтверждённый locator и открытый inode при resume и writer join.
// Подтверждение не даёт execution/usage authority и не фиксирует byte digest; capture проверяет новые байты.
type confirmedResumeSource struct {
	sessionID string
	path      string
	file      *os.File
	identity  os.FileInfo
}

// bindExecutionThread starts a persistent thread or first reads/confirms an existing source before
// cold resume with excludeTurns. It pins model/cwd/approval and stable instruction guidance; current
// attempt instructions are delivered later per turn. A failed resume can retain a source fd for capture
// after join, but cannot by itself bind a successful execution or charge restored history.
// bindExecutionThread создаёт persistent thread либо читает/подтверждает источник до cold resume
// с excludeTurns. Она закрепляет model/cwd/approval и стабильный instruction guidance; текущие
// attempt instructions передаются затем в turn. При ошибке resume source fd сохраняется для capture
// после join, но это само по себе не связывает успешное исполнение и не начисляет restored history.
func (server *appServer) bindExecutionThread(ctx context.Context, state *protocolState, input model.Input) error {
	// Каждый execution запускает новый app-server, поэтому resume здесь cold.
	// Полная свежая инструкция передаётся turn/start, а не sticky thread context.
	params := map[string]any{"approvalPolicy": input.CodexApprovalPolicy, "cwd": workingDirectory(input), "model": input.Model,
		"developerInstructions": runtimeInstructionBinding}
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
		// Codex 0.160.0 сохраняет model history, но не повторяет её целиком
		// в JSONL-ответе: для binding нужны только metadata и live-resume state.
		params["excludeTurns"] = true
	}
	raw, err := server.call(ctx, state, method, params)
	if err != nil {
		return atProviderStage(providerStageThreadCall, err)
	}
	if err := state.bindThread(raw, input.Model, workingDirectory(input), input.CodexApprovalPolicy); err != nil {
		return atProviderStage(providerStageThreadBind, err)
	}
	return nil
}

// confirmResumeSource checks trusted expected session identity, canonical home/archive locator,
// protected file metadata and fd/path inode agreement, returning ownership of an open source to caller.
// It closes failed candidates and does not bind protocol execution, parse history semantics or freeze content.
// confirmResumeSource проверяет доверенную ожидаемую session identity, канонический home/archive locator,
// protected file metadata и совпадение fd/path inode, передавая открытый source caller.
// При отказе fd закрывается; protocol execution, смысл истории и неизменность содержимого не подтверждаются.
func confirmResumeSource(input model.Input, raw json.RawMessage) (*confirmedResumeSource, error) {
	sessionID, path, err := parseThreadRead(raw)
	if err != nil {
		return nil, rejectResumeSource("RESUME_SOURCE_SCHEMA")
	}
	if input.CodexSessionID == "" || sessionID != input.CodexSessionID {
		return nil, rejectResumeSource("RESUME_SOURCE_ID")
	}
	if !filepath.IsAbs(input.WorkspaceRoot) || filepath.Clean(input.WorkspaceRoot) != input.WorkspaceRoot ||
		input.CodexHome != filepath.Join(input.WorkspaceRoot, ".kodex/state/codex-home") ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, rejectResumeSource("RESUME_SOURCE_LOCATOR")
	}
	relative, err := filepath.Rel(input.WorkspaceRoot, path)
	if err != nil || runtimecontract.ValidateCodexArchiveIdentity(sessionID, filepath.ToSlash(relative)) != nil {
		return nil, rejectResumeSource("RESUME_SOURCE_LOCATOR")
	}
	file, info, err := openProtectedFile(input.WorkspaceRoot, path)
	if err != nil {
		return nil, rejectResumeSource("RESUME_SOURCE_OPEN")
	}
	if !validResumeSourceInfo(info) {
		file.Close()
		return nil, rejectResumeSource("RESUME_SOURCE_METADATA")
	}
	source := &confirmedResumeSource{sessionID: sessionID, path: path, file: file, identity: info}
	if source.verifyIdentity(input) != nil {
		file.Close()
		return nil, rejectResumeSource("RESUME_SOURCE_IDENTITY")
	}
	return source, nil
}

// validResumeSourceInfo requires a nonempty bounded regular file, permission bits 0640, current
// effective UID and one link; canonical writer UID additionally requires shared group 29000.
// validResumeSourceInfo требует непустой bounded regular file, permission bits 0640, текущий
// effective UID и одну ссылку; для канонического writer UID дополнительно требуется group 29000.
func validResumeSourceInfo(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o640 || info.Size() <= 0 || info.Size() > maximumArchiveBytes {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1 &&
		(os.Geteuid() != providerWriterUID || stat.Gid == 29000)
}

// verifyIdentity rechecks the held file and reopened path against the confirmed inode and current
// input session. Appends remain possible: this validates identity/metadata, not an old content digest.
// verifyIdentity сверяет удерживаемый файл и вновь открытый путь с подтверждённым inode и
// текущей input session. Append допускается: проверяются identity/metadata, не прежний content digest.
func (source *confirmedResumeSource) verifyIdentity(input model.Input) error {
	if source == nil || source.file == nil || source.sessionID != input.CodexSessionID {
		return rejectResumeSource("RESUME_SOURCE_IDENTITY")
	}
	held, err := source.file.Stat()
	if err != nil || !validResumeSourceInfo(held) || !os.SameFile(source.identity, held) {
		return rejectResumeSource("RESUME_SOURCE_IDENTITY")
	}
	current, info, err := openProtectedFile(input.WorkspaceRoot, source.path)
	if err != nil {
		return rejectResumeSource("RESUME_SOURCE_IDENTITY")
	}
	defer current.Close()
	if !validResumeSourceInfo(info) || !os.SameFile(held, info) {
		return rejectResumeSource("RESUME_SOURCE_IDENTITY")
	}
	return nil
}

// capture requires captureReady and matching source identity before and after a fresh stopped-rollout
// capture. It never borrows a rejected resume locator or reuses previous SHA/size as current evidence.
// capture требует captureReady и совпадения source identity до и после нового stopped-rollout
// capture. Locator отклонённого resume и прежние SHA/size не используются как текущие доказательства.
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
