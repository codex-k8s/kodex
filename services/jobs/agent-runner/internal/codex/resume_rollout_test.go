package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// resumeSourceFixture creates a captured synthetic rollout and matching thread/read metadata,
// adding model/approval expectations without starting an app-server or contacting a provider.
// resumeSourceFixture создаёт captured synthetic rollout и совпадающие thread/read metadata,
// добавляя ожидаемые model/approval без запуска app-server или обращения к provider.
func resumeSourceFixture(t *testing.T) (model.Input, Result, json.RawMessage) {
	t.Helper()
	input, before := capturedRolloutFixture(t, model.Input{})
	input.CodexSessionID, input.Model, input.CodexApprovalPolicy = before.SessionID, "codex", "never"
	thread := codex160ThreadFixture(t)
	thread["id"], thread["sessionId"], thread["path"], thread["cwd"] = before.SessionID, before.SessionID, before.ArchivePath, input.WorkspaceRoot
	return input, before, marshalProtocolFixture(t, map[string]any{"thread": thread})
}

// TestConfirmResumeSourceDoesNotBindExecutionOrUsage confirms an exact source without assigning
// thread/turn/baseline/result authority and rejects capture without a joined writer.
// TestConfirmResumeSourceDoesNotBindExecutionOrUsage подтверждает точный source без назначения
// thread/turn/baseline/result authority и отвергает capture без завершённого join writer.
func TestConfirmResumeSourceDoesNotBindExecutionOrUsage(t *testing.T) {
	input, before, raw := resumeSourceFixture(t)
	state := newProtocolState(input.CodexSessionID)
	source, err := confirmResumeSource(input, raw)
	if err != nil {
		t.Fatal("exact protected source was not confirmed")
	}
	defer source.file.Close()
	state.resumeSource = source
	if state.threadID != "" || state.threadPath != "" || state.result.SessionID != "" || state.baselineCaptured || state.turnID != "" || state.measuredResult().HasVerifiedRollout(input) {
		t.Fatal("read-only source locator invented execution binding or usage")
	}
	if source.sessionID != before.SessionID || source.path != before.ArchivePath || source.verifyIdentity(input) != nil {
		t.Fatal("confirmed source identity changed")
	}
	if _, err := source.capture(input, nil); err == nil {
		t.Fatal("source capture did not require a joined writer")
	}
}

// TestConfirmResumeSourceRejectsUntrustedLocators exercises schema/identity/path/link/type/size/mode
// and home failures, requiring no retained source and a bound, payload-free closed diagnostic.
// TestConfirmResumeSourceRejectsUntrustedLocators проверяет отказы schema/identity/path/link/type/size/mode
// и home, требуя отсутствие source и связанный закрытый diagnostic без payload.
func TestConfirmResumeSourceRejectsUntrustedLocators(t *testing.T) {
	for _, mode := range []string{"empty expected", "foreign id", "malformed id", "foreign session metadata", "missing path", "null path", "relative path", "outside home", "archived", "wrong suffix", "unclean", "ephemeral", "unknown field", "duplicate field", "malformed envelope", "symlink", "directory symlink", "hardlink", "missing file", "wrong mode", "empty file", "oversized file", "directory", "foreign home"} {
		t.Run(mode, func(t *testing.T) {
			input, before, _ := resumeSourceFixture(t)
			thread := codex160ThreadFixture(t)
			thread["id"], thread["sessionId"], thread["path"] = before.SessionID, before.SessionID, before.ArchivePath
			switch mode {
			case "empty expected":
				input.CodexSessionID = ""
			case "foreign id":
				thread["id"] = "00000000-0000-4000-8000-000000000002"
			case "malformed id":
				thread["id"] = "not-a-uuid"
			case "foreign session metadata":
				thread["sessionId"] = "bad"
			case "missing path":
				delete(thread, "path")
			case "null path":
				thread["path"] = nil
			case "relative path":
				thread["path"] = "sessions/rollout.jsonl"
			case "outside home":
				thread["path"] = filepath.Join(input.WorkspaceRoot, "other", filepath.Base(before.ArchivePath))
			case "archived":
				thread["path"] = strings.Replace(before.ArchivePath, "/sessions/", "/archived_sessions/", 1)
			case "wrong suffix":
				thread["path"] = strings.Replace(before.ArchivePath, before.SessionID, "00000000-0000-4000-8000-000000000002", 1)
			case "unclean":
				thread["path"] = filepath.Dir(before.ArchivePath) + "/../07/" + filepath.Base(before.ArchivePath)
			case "ephemeral":
				thread["ephemeral"] = true
			case "unknown field":
				thread["trusted"] = true
			case "symlink":
				if os.Rename(before.ArchivePath, before.ArchivePath+".old") != nil || os.Symlink(before.ArchivePath+".old", before.ArchivePath) != nil {
					t.Fatal("fixture symlink setup failed")
				}
			case "directory symlink":
				dir := filepath.Dir(before.ArchivePath)
				if os.Rename(dir, dir+"-old") != nil || os.Symlink(dir+"-old", dir) != nil {
					t.Fatal("fixture directory symlink setup failed")
				}
			case "hardlink":
				if os.Link(before.ArchivePath, before.ArchivePath+".link") != nil {
					t.Fatal("fixture hardlink setup failed")
				}
			case "missing file":
				if os.Remove(before.ArchivePath) != nil {
					t.Fatal("fixture source removal failed")
				}
			case "wrong mode":
				if os.Chmod(before.ArchivePath, 0o660) != nil {
					t.Fatal("fixture mode setup failed")
				}
			case "empty file":
				if os.Truncate(before.ArchivePath, 0) != nil {
					t.Fatal("fixture empty source setup failed")
				}
			case "oversized file":
				if os.Truncate(before.ArchivePath, maximumArchiveBytes+1) != nil {
					t.Fatal("fixture source bound setup failed")
				}
			case "directory":
				if os.Remove(before.ArchivePath) != nil || os.Mkdir(before.ArchivePath, 0o640) != nil {
					t.Fatal("fixture directory setup failed")
				}
			case "foreign home":
				input.CodexHome = filepath.Join(input.WorkspaceRoot, "foreign-home")
			}
			raw := marshalProtocolFixture(t, map[string]any{"thread": thread})
			if mode == "duplicate field" {
				raw = append([]byte(`{"thread":{},`), raw[1:]...)
			}
			if mode == "malformed envelope" {
				raw = []byte(`{"thread":`)
			}
			source, err := confirmResumeSource(input, raw)
			if !errors.Is(err, errResumeSourceInvalid) || source != nil {
				if source != nil {
					source.file.Close()
				}
				t.Fatal("untrusted pre-read locator was accepted")
			}
			wantDetail := "RESUME_SOURCE_LOCATOR"
			switch mode {
			case "empty expected", "foreign id":
				wantDetail = "RESUME_SOURCE_ID"
			case "malformed id", "foreign session metadata", "ephemeral", "unknown field", "duplicate field", "malformed envelope":
				wantDetail = "RESUME_SOURCE_SCHEMA"
			case "symlink", "directory symlink", "hardlink", "missing file", "directory":
				wantDetail = "RESUME_SOURCE_OPEN"
			case "wrong mode", "empty file", "oversized file":
				wantDetail = "RESUME_SOURCE_METADATA"
			}
			assertResumeDiagnostic(t, input, err, wantDetail)
		})
	}
}

// assertResumeDiagnostic checks the fixed error text and exact reason, supplies canonical session/turn
// refs to the synthetic input, and validates safe diagnostic JSON round-trip without a workspace path.
// assertResumeDiagnostic проверяет фиксированный error text и точную причину, подставляет канонические
// session/turn refs synthetic input и проверяет безопасный diagnostic JSON round-trip без workspace path.
func assertResumeDiagnostic(t *testing.T, input model.Input, err error, wantDetail string) {
	t.Helper()
	if err.Error() != errResumeSourceInvalid.Error() {
		t.Fatal("resume failure reflected private input")
	}
	diagnostic, code := providerFailureDetails(providerStageThreadRead, err)
	// Rollout fixture использует короткие refs; wire fixture имеет канонические refs.
	input.SessionRef, input.TurnRef = "ses_fixture1", "trn_fixture1"
	bound := bindProviderDiagnostic(input, diagnostic)
	if code != 0 || diagnostic.Detail != wantDetail || bound == nil || !bound.Matches(input) {
		t.Fatal("resume failure lost closed reason or exact execution binding")
	}
	raw, marshalErr := json.Marshal(bound)
	var decoded runtimecontract.ProviderFailureDiagnostic
	if marshalErr != nil || json.Unmarshal(raw, &decoded) != nil || !decoded.Matches(input) || decoded.Detail != wantDetail || strings.Contains(string(raw), input.WorkspaceRoot) {
		t.Fatal("resume diagnostic failed safe consumer round trip")
	}
}

// TestResumeSourceDiagnosticDoesNotClassifyForeignStagesOrErrors prevents unrelated stages/errors,
// unknown details and joined authentication failures from acquiring a local resume-source diagnostic reason.
// TestResumeSourceDiagnosticDoesNotClassifyForeignStagesOrErrors не даёт чужим stages/errors,
// неизвестным details и joined authentication failures получить локальную resume-source diagnostic reason.
func TestResumeSourceDiagnosticDoesNotClassifyForeignStagesOrErrors(t *testing.T) {
	for _, scenario := range []struct {
		stage providerExecutionStage
		err   error
	}{
		{providerStageArchiveCapture, rejectResumeSource("RESUME_SOURCE_METADATA")},
		{providerStageThreadRead, errors.New("PRIVATE_SENTINEL")},
		{providerStageThreadRead, protocolError("thread/read", json.RawMessage(`{"private":"PRIVATE_SENTINEL"}`))},
		{providerStageThreadRead, rejectResumeSource("PRIVATE_SENTINEL")},
		{providerStageThreadRead, errors.Join(ErrProviderAuthentication, rejectResumeSource("RESUME_SOURCE_METADATA"))},
	} {
		diagnostic, code := providerFailureDetails(scenario.stage, scenario.err)
		if diagnostic.Detail != "NONE" || code != 0 {
			t.Fatal("untrusted error acquired local resume source reason")
		}
	}
}

// TestConfirmedResumeSourceRejectsChangedInodeAndPermissions rejects path replacement, unsafe links,
// mode changes, disappearance, a closed fd and a foreign expected session after initial confirmation.
// TestConfirmedResumeSourceRejectsChangedInodeAndPermissions отвергает замену пути, опасные ссылки,
// смену mode, удаление, закрытый fd и чужую expected session после первоначального подтверждения.
func TestConfirmedResumeSourceRejectsChangedInodeAndPermissions(t *testing.T) {
	for _, mode := range []string{"replacement", "symlink", "hardlink", "mode", "missing", "closed descriptor", "foreign input"} {
		t.Run(mode, func(t *testing.T) {
			input, before, raw := resumeSourceFixture(t)
			source, err := confirmResumeSource(input, raw)
			if err != nil {
				t.Fatal("fixture confirmation failed")
			}
			defer source.file.Close()
			switch mode {
			case "replacement":
				if os.Rename(before.ArchivePath, before.ArchivePath+".old") != nil || os.WriteFile(before.ArchivePath, []byte("new inode"), 0o640) != nil {
					t.Fatal("fixture replacement failed")
				}
			case "symlink":
				if os.Rename(before.ArchivePath, before.ArchivePath+".old") != nil || os.Symlink(before.ArchivePath+".old", before.ArchivePath) != nil {
					t.Fatal("fixture symlink failed")
				}
			case "hardlink":
				if os.Link(before.ArchivePath, before.ArchivePath+".link") != nil {
					t.Fatal("fixture link failed")
				}
			case "mode":
				if os.Chmod(before.ArchivePath, 0o660) != nil {
					t.Fatal("fixture mode failed")
				}
			case "missing":
				if os.Remove(before.ArchivePath) != nil {
					t.Fatal("fixture removal failed")
				}
			case "closed descriptor":
				source.file.Close()
			case "foreign input":
				input.CodexSessionID = "00000000-0000-4000-8000-000000000002"
			}
			err = source.verifyIdentity(input)
			if !errors.Is(err, errResumeSourceInvalid) {
				t.Fatal("changed source identity was accepted")
			}
			assertResumeDiagnostic(t, input, err, "RESUME_SOURCE_IDENTITY")
		})
	}
}

// resumeForeignOwnerInfo preserves real file metadata while substituting syscall ownership in a unit test.
// resumeForeignOwnerInfo сохраняет реальные file metadata, подменяя syscall ownership в unit-тесте.
type resumeForeignOwnerInfo struct {
	os.FileInfo
	stat syscall.Stat_t
}

// Sys returns the substituted stat rather than the embedded file's original ownership record.
// Sys возвращает подставленный stat вместо исходной ownership record вложенного файла.
func (info resumeForeignOwnerInfo) Sys() any { return &info.stat }

// TestResumeSourceRejectsForeignOwner substitutes a different UID in metadata without privileged
// chown and requires rejection by validResumeSourceInfo.
// TestResumeSourceRejectsForeignOwner подставляет другой UID в metadata без привилегированного
// chown и требует отказ validResumeSourceInfo.
func TestResumeSourceRejectsForeignOwner(t *testing.T) {
	_, before, _ := resumeSourceFixture(t)
	info, err := os.Stat(before.ArchivePath)
	if err != nil {
		t.Fatal("fixture stat failed")
	}
	stat := *info.Sys().(*syscall.Stat_t)
	stat.Uid++
	if validResumeSourceInfo(resumeForeignOwnerInfo{info, stat}) {
		t.Fatal("foreign writer owner was accepted")
	}
}

// TestResumeRolloutRPCProcessFixture is an opt-in synthetic JSONL child that records read/resume,
// appends history and injects RPC/binding failures. With no mode it returns without exercising a process.
// TestResumeRolloutRPCProcessFixture — opt-in synthetic JSONL child, записывающий read/resume,
// дописывающий history и подставляющий RPC/binding failures. Без mode она возвращается без проверки процесса.
func TestResumeRolloutRPCProcessFixture(t *testing.T) {
	mode := os.Getenv("KODEX_RESUME_RPC_FIXTURE")
	if mode == "" {
		return
	}
	path, calls := os.Getenv("KODEX_RESUME_RPC_PATH"), os.Getenv("KODEX_RESUME_RPC_CALLS")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(3)
		}
		file, err := os.OpenFile(calls, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			os.Exit(4)
		}
		_, err = file.WriteString(request.Method + "\n")
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			os.Exit(5)
		}
		thread := codex160ThreadFixture(t)
		thread["id"], thread["sessionId"], thread["path"] = "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000001", path
		response := map[string]any{"id": request.ID, "result": map[string]any{"thread": thread}}
		if request.Method == "thread/read" {
			if mode == "invalid pre-read" {
				thread["id"] = "00000000-0000-4000-8000-000000000002"
			}
		} else if request.Method == "thread/resume" {
			writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				os.Exit(6)
			}
			_, writeErr := writer.WriteString("{\"type\":\"event_msg\",\"payload\":\"appended-before-failure\"}\n")
			syncErr, closeErr := writer.Sync(), writer.Close()
			if writeErr != nil || syncErr != nil || closeErr != nil {
				os.Exit(7)
			}
			if mode == "resume error" {
				response = map[string]any{"id": request.ID, "error": map[string]any{"code": -32603, "message": "synthetic resume failure"}}
			} else {
				response["result"] = map[string]any{"approvalPolicy": "never", "approvalsReviewer": "user", "cwd": os.Getenv("KODEX_RESUME_RPC_WORKSPACE"), "model": "wrong-model", "modelProvider": "openai", "sandbox": map[string]any{"type": "readOnly"}, "thread": thread}
			}
		} else {
			os.Exit(8)
		}
		if json.NewEncoder(os.Stdout).Encode(response) != nil {
			os.Exit(9)
		}
	}
	if scanner.Err() != nil {
		os.Exit(10)
	}
	os.Exit(0)
}

// TestResumePreReadCapturesAppendOnRPCAndBindingFailures launches the synthetic RPC child, requires
// bounded abort/join before fresh capture, preserves appended history without success/usage and rejects
// replacement after join. Invalid pre-read must never reach thread/resume or issue archive pins.
// TestResumePreReadCapturesAppendOnRPCAndBindingFailures запускает synthetic RPC child, требует
// bounded abort/join перед новым capture, сохраняет добавленную историю без success/usage и отвергает
// замену после join. Неверный pre-read не должен достигать thread/resume или выдавать archive pins.
func TestResumePreReadCapturesAppendOnRPCAndBindingFailures(t *testing.T) {
	for _, mode := range []string{"resume error", "bind error", "invalid pre-read", "replacement after join"} {
		t.Run(mode, func(t *testing.T) {
			input, before, _ := resumeSourceFixture(t)
			calls := filepath.Join(t.TempDir(), "calls")
			executable, err := os.Executable()
			if err != nil {
				t.Fatal("fixture executable unavailable")
			}
			command := exec.Command(executable, "-test.run=^TestResumeRolloutRPCProcessFixture$")
			fixtureMode := mode
			if mode == "replacement after join" {
				fixtureMode = "resume error"
			}
			command.Env = []string{"KODEX_RESUME_RPC_FIXTURE=" + fixtureMode, "KODEX_RESUME_RPC_PATH=" + before.ArchivePath, "KODEX_RESUME_RPC_CALLS=" + calls, "KODEX_RESUME_RPC_WORKSPACE=" + input.WorkspaceRoot}
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
			server, err := startAppServerCommand(command, nil)
			if err != nil {
				t.Fatal("fixture server start failed")
			}
			t.Cleanup(func() { _ = server.terminate(errors.New("fixture cleanup")) })
			state := newProtocolState(input.CodexSessionID)
			err = server.bindExecutionThread(t.Context(), state, input)
			if state.resumeSource != nil {
				defer state.resumeSource.file.Close()
			}
			wantStage := providerStageThreadCall
			if mode == "bind error" {
				wantStage = providerStageThreadBind
			}
			if mode == "invalid pre-read" {
				wantStage = providerStageThreadRead
			}
			if err == nil || providerStageOf(err) != wantStage || state.threadID != "" || state.threadPath != "" || state.baselineCaptured || state.result.SessionID != "" {
				t.Fatal("failed prebind execution invented state or changed failure stage")
			}
			if captureFailedRollout(input, server, state, Result{}).SessionID != "" {
				t.Fatal("unjoined writer issued archive pins")
			}
			started := time.Now()
			if abortErr := server.abort(t.Context(), state, err); !errors.Is(abortErr, err) || !server.captureReady() || time.Since(started) > 3*terminationGrace {
				t.Fatal("failed resume was not joined with its original cause")
			}
			if mode == "replacement after join" {
				if os.Rename(before.ArchivePath, before.ArchivePath+".old") != nil || os.WriteFile(before.ArchivePath, []byte("foreign replacement"), 0o640) != nil {
					t.Fatal("fixture source replacement failed")
				}
				result := captureFailedRollout(input, server, state, state.measuredResult())
				if result.SessionID != "" || result.ArchiveSHA256 != "" || result.ArchiveSizeBytes != 0 || result.HasVerifiedRollout(input) {
					t.Fatal("joined writer issued pins for a replacement inode")
				}
				return
			}
			result := captureFailedRollout(input, server, state, state.measuredResult())
			methods, readErr := os.ReadFile(calls)
			if readErr != nil {
				t.Fatal("fixture invocation proof unavailable")
			}
			if mode == "invalid pre-read" {
				if string(methods) != "thread/read\n" || state.resumeSource != nil || result.SessionID != "" {
					t.Fatal("invalid pre-read reached resume or issued capture")
				}
				return
			}
			if string(methods) != "thread/read\nthread/resume\n" || !result.HasVerifiedRollout(input) || result.ArchiveSHA256 == before.ArchiveSHA256 || result.ArchiveSizeBytes <= before.ArchiveSizeBytes || result.Outcome != "" || result.FinalMessage != "" || result.Usage.TotalTokens != 0 {
				t.Fatal("failed prebind lost fresh verified history or invented success/usage")
			}
		})
	}
}
