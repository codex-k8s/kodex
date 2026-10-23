package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestPostJoinWorkspaceOutboxDenialStillNormalizesPrivateDirectories rejects a synthetic empty outbox
// entry yet still normalizes private directories, preserving the original failure/usage and private-file mode.
// TestPostJoinWorkspaceOutboxDenialStillNormalizesPrivateDirectories отклоняет пустую синтетическую запись
// outbox, но нормализует приватные каталоги, сохраняя исходный отказ, usage и права приватного файла.
func TestPostJoinWorkspaceOutboxDenialStillNormalizesPrivateDirectories(t *testing.T) {
	outbox, publication := outboxPublicationFixture(t)
	if os.WriteFile(filepath.Join(outbox, "empty.md"), nil, 0o600) != nil {
		t.Fatal("prepare rejected outbox entry")
	}
	work, directory, uid, group := workspaceAccessFixture(t)
	private := filepath.Join(work, "private")
	file := filepath.Join(private, "private.txt")
	if os.Mkdir(private, 0o700) != nil || os.WriteFile(file, []byte("synthetic private sentinel"), 0o600) != nil {
		t.Fatal("prepare private working directory")
	}
	if publishOutboxDirectory(t.Context(), publication, 10, uid, uid, group) == nil {
		t.Fatal("invalid outbox unexpectedly published")
	}
	want := Result{Outcome: "FAILED", FailureCode: "RUNTIME_ARTIFACT_INVALID", FinalMessage: "i18n:RUNTIME_ARTIFACT_INVALID", Usage: runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}}
	got := normalizeCompletedProviderWorkspace(t.Context(), model.Input{}, want, func(ctx context.Context, _ model.Input) error {
		return normalizeWorkspaceAccess(ctx, directory, runtimecontract.RuntimeWorkspacePolicyV1(), uid, uid, group)
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatal("cleanup changed the original failure or measured usage")
	}
	info, err := os.Stat(private)
	if err != nil || info.Mode().Perm() != 0o750 {
		t.Fatal("failed publication skipped private directory normalization")
	}
	info, err = os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("cleanup opened private file permissions")
	}
}

// TestPostJoinWorkspaceOutcomeAndIndependentBudget checks cleanup after caller cancellation retains
// context values under its own deadline, runs only for terminal outcomes and never replaces an existing failure.
// TestPostJoinWorkspaceOutcomeAndIndependentBudget проверяет сохранение context values после отмены
// с собственным deadline очистки, её запуск лишь для итоговых состояний и сохранение прежнего отказа.
func TestPostJoinWorkspaceOutcomeAndIndependentBudget(t *testing.T) {
	for _, outcome := range []string{"SUCCEEDED", "FAILED", "UNKNOWN"} {
		for _, denied := range []bool{false, true} {
			t.Run(outcome+"/"+map[bool]string{false: "clean", true: "denied"}[denied], func(t *testing.T) {
				// contextKey isolates the synthetic execution marker so cleanup-context inheritance is checked
				// without colliding with application context keys.
				// contextKey изолирует синтетическую отметку исполнения для проверки наследования cleanup context
				// без пересечения с ключами контекста приложения.
				type contextKey struct{}
				ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "bound execution"))
				cancel()
				want := Result{Outcome: outcome, FailureCode: "provider_bad_request", FinalMessage: "original failure", SessionID: "unchanged session", ArchiveRelativePath: "unchanged source", ArchiveSHA256: "unchanged digest", ArchiveSizeBytes: 1,
					Usage: runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}}
				called := false
				var cleanup context.Context
				got := normalizeCompletedProviderWorkspace(ctx, model.Input{}, want, func(ctx context.Context, _ model.Input) error {
					called, cleanup = true, ctx
					deadline, ok := ctx.Deadline()
					if ctx.Err() != nil || ctx.Value(contextKey{}) != "bound execution" || !ok || time.Until(deadline) <= 0 || time.Until(deadline) > workspaceAccessBudget {
						t.Fatal("post-join cleanup lost independent bounded context")
					}
					if denied {
						return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemByteQuota)
					}
					return nil
				})
				if called != (outcome != "UNKNOWN") || called && !errors.Is(cleanup.Err(), context.Canceled) {
					t.Fatal("invalid outcome normalized or cleanup context not released")
				}
				if outcome == "SUCCEEDED" && denied {
					want.Outcome, want.FailureCode, want.FinalMessage = "FAILED", "RUNTIME_ARTIFACT_INVALID", "i18n:RUNTIME_ARTIFACT_INVALID"
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("cleanup changed the original failure, usage or source pins")
				}
			})
		}
	}
}

// TestProviderFilesystemDiagnosticsClosedAndPrivate checks every reason for both known stages and
// requires foreign/unknown metadata to collapse to UNKNOWN with no path/body/error sentinel disclosure.
// TestProviderFilesystemDiagnosticsClosedAndPrivate проверяет все причины обоих этапов и требует
// UNKNOWN для чужих или неизвестных метаданных без утечки маркера пути, содержимого или ошибки.
func TestProviderFilesystemDiagnosticsClosedAndPrivate(t *testing.T) {
	// sentinel is synthetic private data that must stay outside every structured diagnostic field.
	// sentinel является синтетическими приватными данными, недопустимыми в полях структурированной диагностики.
	const sentinel = "private-path-body-error-secret-sentinel"
	for _, stage := range []string{providerOutboxStage, providerWorkspaceStage} {
		cause := errProviderOutboxPublication
		if stage == providerWorkspaceStage {
			cause = errProviderWorkspaceAccess
		}
		for reason := filesystemUnknown; reason <= filesystemIO; reason++ {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			err := errors.Join(providerFilesystemDenied(cause, reason), errors.New(sentinel))
			writeProviderFilesystemDenial(logger, stage, err)
			var row map[string]any
			if json.Unmarshal(output.Bytes(), &row) != nil || len(row) != 5 || row[providerFilesystemStageKey] != stage || row[providerFilesystemReasonKey] != reason.String() || bytes.Contains(output.Bytes(), []byte(sentinel)) {
				t.Fatal("closed diagnostic leaked an error or changed its enum")
			}
		}
	}
	for _, fixture := range []struct {
		stage string
		err   error
	}{
		{sentinel, providerFilesystemDenied(errProviderOutboxPublication, filesystemOwner)},
		{providerOutboxStage, errors.New(sentinel)},
		{providerOutboxStage, providerFilesystemDenied(errProviderOutboxPublication, providerFilesystemReason(255))},
		{providerOutboxStage, providerFilesystemDenied(errProviderWorkspaceAccess, filesystemOwner)},
	} {
		var output bytes.Buffer
		writeProviderFilesystemDenial(slog.New(slog.NewJSONHandler(&output, nil)), fixture.stage, fixture.err)
		var row map[string]any
		if json.Unmarshal(output.Bytes(), &row) != nil || row[providerFilesystemReasonKey] != "UNKNOWN" || bytes.Contains(output.Bytes(), []byte(sentinel)) {
			t.Fatal("unknown diagnostic accepted arbitrary metadata or raw error")
		}
	}
	writeProviderFilesystemDenial(nil, providerOutboxStage, errors.New(sentinel))
	var empty bytes.Buffer
	writeProviderFilesystemDenial(slog.New(slog.NewJSONHandler(&empty, nil)), providerOutboxStage, nil)
	if empty.Len() != 0 {
		t.Fatal("successful cleanup emitted a denial")
	}
}

// assertProviderFilesystemReason requires both the exact sentinel and a nonnil typed guard error,
// then checks its assigned reason rather than inferring a reason from error text.
// assertProviderFilesystemReason требует точный маркер и ненулевую типизированную ошибку guard,
// затем сверяет назначенную категорию, не выводя её из текста ошибки.
func assertProviderFilesystemReason(t *testing.T, err error, cause error, reason providerFilesystemReason) {
	t.Helper()
	var denied *providerFilesystemError
	if !errors.Is(err, cause) || !errors.As(err, &denied) || denied == nil {
		t.Fatal("rejecting guard lost its exact closed diagnostic reason")
	}
	if denied.reason != reason {
		t.Fatalf("rejecting guard diagnostic mismatch: expected=%s actual=%s", reason.String(), denied.reason.String())
	}
}
