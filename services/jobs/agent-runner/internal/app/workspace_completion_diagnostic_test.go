package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

// TestClosedWorkspaceReasonKeepsOnlyExactFailureCodes independently fixes the existing four-code output set, rejecting success tokens, private text and noncanonical whitespace/case without normalizing them.
// TestClosedWorkspaceReasonKeepsOnlyExactFailureCodes независимо закрепляет прежние четыре кода отказа, отклоняя признаки успеха, приватный текст и неканонические пробелы/регистр без нормализации.
func TestClosedWorkspaceReasonKeepsOnlyExactFailureCodes(t *testing.T) {
	for _, fixture := range []struct {
		input string
		want  string
	}{
		{"READ_ONLY", "READ_ONLY"},
		{"QUOTA_EXCEEDED", "QUOTA_EXCEEDED"},
		{"PATH_OUTSIDE_WORKSPACE", "PATH_OUTSIDE_WORKSPACE"},
		{"RUNTIME_IO_ERROR", "RUNTIME_IO_ERROR"},
		{"", "RUNTIME_IO_ERROR"},
		{"OK", "RUNTIME_IO_ERROR"},
		{"PRIVATE_REASON_SENTINEL", "RUNTIME_IO_ERROR"},
		{"read_only", "RUNTIME_IO_ERROR"},
		{" READ_ONLY", "RUNTIME_IO_ERROR"},
		{"READ_ONLY\n", "RUNTIME_IO_ERROR"},
	} {
		if got := closedWorkspaceReason(fixture.input); got != fixture.want {
			t.Errorf("closed workspace reason = %q, want %q", got, fixture.want)
		}
	}
}

// TestWorkspaceCompletionDiagnosticIsClosedAndRedacted combines known/private reasons and stages, requiring one structured warning with canonical fallbacks, no private cause and harmless handling of a nil logger.
// TestWorkspaceCompletionDiagnosticIsClosedAndRedacted сочетает известные/приватные причины и этапы, требуя одно структурированное предупреждение с каноническими fallback, без приватной cause и с безопасным nil logger.
func TestWorkspaceCompletionDiagnosticIsClosedAndRedacted(t *testing.T) {
	for _, reason := range []string{runtimecontract.RuntimeWorkspaceReadOnly,
		runtimecontract.RuntimeWorkspaceQuotaExceeded, runtimecontract.RuntimeWorkspacePathOutsideWorkspace,
		runtimecontract.RuntimeWorkspaceIOError, "PRIVATE_PATH_AND_TOKEN_SENTINEL"} {
		for _, stage := range []string{workspaceCompletionCheck, workspaceCompletionPublish, "PRIVATE_STAGE_SENTINEL"} {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			err := errors.Join(&workspacepolicy.Denial{Reason: reason}, errors.New("PRIVATE_CAUSE_SENTINEL"))
			logWorkspaceCompletionDenial(t.Context(), logger, stage, err)
			if strings.Count(output.String(), "\n") != 1 || strings.Contains(output.String(), "PRIVATE_") {
				t.Fatal("workspace diagnostic disclosed input or emitted duplicate records")
			}
			var record map[string]any
			if json.Unmarshal(output.Bytes(), &record) != nil || record["msg"] != workspaceCompletionMessage || record["level"] != "WARN" {
				t.Fatal("workspace diagnostic lost its structured runtime logger")
			}
			wantReason, wantStage := reason, stage
			if strings.HasPrefix(reason, "PRIVATE_") {
				wantReason = runtimecontract.RuntimeWorkspaceIOError
			}
			if strings.HasPrefix(stage, "PRIVATE_") {
				wantStage = "UNKNOWN"
			}
			if record[workspaceCompletionStage] != wantStage || record[workspaceCompletionReason] != wantReason {
				t.Fatal("workspace diagnostic lost its closed stage or denial reason")
			}
		}
	}
	logWorkspaceCompletionDenial(t.Context(), nil, workspaceCompletionCheck, errors.New("PRIVATE_CAUSE_SENTINEL"))
}

// TestWorkspaceArtifactCollectionGuardReasonsAreClosed checks typed, errno, private and nil errors become redacted artifact guards with the expected closed reason and fixed log shape.
// TestWorkspaceArtifactCollectionGuardReasonsAreClosed проверяет преобразование типизированных, errno, приватных и nil-ошибок в очищенные artifact guards с ожидаемой закрытой причиной и фиксированной формой лога.
func TestWorkspaceArtifactCollectionGuardReasonsAreClosed(t *testing.T) {
	for _, fixture := range []struct {
		err    error
		reason string
	}{
		{&os.PathError{Op: "PRIVATE_OPERATION_SENTINEL", Path: "PRIVATE_PATH_SENTINEL", Err: syscall.EACCES}, runtimecontract.RuntimeWorkspaceReadOnly},
		{syscall.EROFS, runtimecontract.RuntimeWorkspaceReadOnly},
		{syscall.ELOOP, runtimecontract.RuntimeWorkspacePathOutsideWorkspace},
		{&workspacepolicy.Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}, runtimecontract.RuntimeWorkspaceQuotaExceeded},
		{errors.New("PRIVATE_ERROR_SENTINEL"), runtimecontract.RuntimeWorkspaceIOError},
		{&workspacepolicy.Denial{Reason: "PRIVATE_REASON_SENTINEL"}, runtimecontract.RuntimeWorkspaceIOError},
		{nil, runtimecontract.RuntimeWorkspaceIOError},
	} {
		denied := denyCompletionArtifact(fixture.err)
		var guard *completionArtifactDenial
		if !errors.As(denied, &guard) || guard.reason != fixture.reason || strings.Contains(denied.Error(), "PRIVATE_") {
			t.Fatal("artifact guard lost its typed reason or retained private error")
		}
		var output bytes.Buffer
		logWorkspaceCompletionDenial(t.Context(), slog.New(slog.NewJSONHandler(&output, nil)), workspaceCompletionPublish, denied)
		var row map[string]any
		if json.Unmarshal(output.Bytes(), &row) != nil || len(row) != 5 || row[workspaceCompletionReason] != fixture.reason || strings.Contains(output.String(), "PRIVATE_") {
			t.Fatal("artifact guard diagnostic disclosed an error or changed its closed reason")
		}
	}
}

// TestWorkspaceArtifactCollectionReportsActualFileGuards creates disposable symlink, hardlink and unreadable files, requiring rejection with no returned artifacts or private data and preservation of the outside sentinel; the unreadable case skips under root.
// TestWorkspaceArtifactCollectionReportsActualFileGuards создаёт одноразовые symlink, hardlink и нечитаемый файл, требуя отказ без артефактов и приватных данных и сохранение внешнего sentinel; нечитаемый случай пропускается под root.
func TestWorkspaceArtifactCollectionReportsActualFileGuards(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "unreadable" && os.Geteuid() == 0 {
				t.Skip("non-root permission fixture required")
			}
			root := t.TempDir()
			outbox := filepath.Join(root, ".kodex", "outbox")
			outside := filepath.Join(t.TempDir(), "PRIVATE_PATH_SENTINEL")
			if os.MkdirAll(outbox, 0o700) != nil || os.WriteFile(outside, []byte("PRIVATE_BODY_SENTINEL"), 0o600) != nil {
				t.Fatal("prepare isolated artifact guard fixture")
			}
			path := filepath.Join(outbox, "artifact.txt")
			want := runtimecontract.RuntimeWorkspaceIOError
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(outside, path)
				want = runtimecontract.RuntimeWorkspacePathOutsideWorkspace
			case "hardlink":
				err = os.Link(outside, path)
			case "unreadable":
				err = os.WriteFile(path, []byte("PRIVATE_BODY_SENTINEL"), 0o000)
				want = runtimecontract.RuntimeWorkspaceReadOnly
			}
			if err != nil {
				t.Fatal("prepare rejected artifact inode")
			}
			artifacts, err := collectArtifacts(model.Input{WorkspaceRoot: root}, "synthetic result")
			var guard *completionArtifactDenial
			if len(artifacts) != 0 || !errors.As(err, &guard) || guard.reason != want || strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatal("actual artifact guard lost its closed reason or disclosed private content")
			}
			body, readErr := os.ReadFile(outside)
			if readErr != nil || string(body) != "PRIVATE_BODY_SENTINEL" {
				t.Fatal("artifact guard changed an outside inode")
			}
		})
	}
}

// TestWorkspaceCompletionArtifactCollectionDenialIsObserved uses a synthetic mutual-TLS callback to verify an artifact guard produces one failed completion retaining usage and one closed publication warning without leaking final text or paths.
// TestWorkspaceCompletionArtifactCollectionDenialIsObserved использует синтетический mutual-TLS callback для проверки одного неуспешного завершения с сохранением usage и одного закрытого publication warning без раскрытия итогового текста и путей.
func TestWorkspaceCompletionArtifactCollectionDenialIsObserved(t *testing.T) {
	input := model.Input{RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 1,
		LeaseRef: "lease_fixture", WorkspaceRoot: filepath.Join(t.TempDir(), "PRIVATE_PATH_SENTINEL"),
		Capabilities: []string{runtimecontract.ArtifactCapability}}
	result := codex.Result{Outcome: "SUCCEEDED", FinalMessage: "PRIVATE_BODY_SENTINEL",
		Usage: runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}}
	calls := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		var payload runtimecontract.RunnerCompletionRequest
		if json.NewDecoder(request.Body).Decode(&payload) != nil || payload.Validate() != nil || payload.Success ||
			payload.SafeErrorCode != "RUNTIME_ARTIFACT_INVALID" || payload.Usage != result.Usage || len(payload.Artifacts) != 0 {
			t.Error("artifact collection changed the failed completion or usage")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()
	client := completionTestClient(t, &input, server)
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	if err := completeExecutedTurn(t.Context(), input, client, result, func(context.Context) error { return nil }, logger); err != nil || calls != 1 {
		t.Fatal("artifact collection did not deliver its original failed completion")
	}
	var record map[string]any
	if json.Unmarshal(output.Bytes(), &record) != nil || strings.Count(output.String(), "\n") != 1 ||
		record[workspaceCompletionStage] != workspaceCompletionPublish || record[workspaceCompletionReason] != runtimecontract.RuntimeWorkspaceIOError {
		t.Fatal("artifact collection lost its closed completion diagnostic")
	}
	if strings.Contains(output.String(), "PRIVATE_") || len(record) != 5 {
		t.Fatal("artifact collection diagnostic disclosed a private path or body")
	}
}
