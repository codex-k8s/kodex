package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	domainintegrations "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
)

type syntheticCanaryWorkerRepository struct {
	domainintegrations.Repository
	mu      sync.Mutex
	claimed bool
}

func (repository *syntheticCanaryWorkerRepository) ClaimExecution(context.Context, string, string, time.Time, time.Duration) (domainintegrations.ExecutionClaim, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.claimed {
		return domainintegrations.ExecutionClaim{}, false, nil
	}
	repository.claimed = true
	return domainintegrations.ExecutionClaim{
		InvocationID: 1, ExecutionFence: "fence_0123456789abcdef0123456789abcdef", LeaseOwner: "synthetic-worker",
	}, true, nil
}

type syntheticCanaryWorkerExecutor struct {
	err error
}

func (executor syntheticCanaryWorkerExecutor) Execute(context.Context, domainintegrations.ExecutionClaim) (domainintegrations.ExecutionReceipt, error) {
	return domainintegrations.ExecutionReceipt{}, executor.err
}

type notifyingLogHandler struct {
	next     slog.Handler
	once     *sync.Once
	observed chan struct{}
}

func (handler notifyingLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.next.Enabled(ctx, level)
}

func (handler notifyingLogHandler) Handle(ctx context.Context, record slog.Record) error {
	err := handler.next.Handle(ctx, record)
	handler.once.Do(func() { close(handler.observed) })
	return err
}

func (handler notifyingLogHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return notifyingLogHandler{next: handler.next.WithAttrs(attributes), once: handler.once, observed: handler.observed}
}

func (handler notifyingLogHandler) WithGroup(name string) slog.Handler {
	return notifyingLogHandler{next: handler.next.WithGroup(name), once: handler.once, observed: handler.observed}
}

func TestIntegrationWorkerLogUsesSafeReasonForSyntheticOnlyCanary(t *testing.T) {
	canary := `synthetic-only-issue93:"worker-error/value+20260721`
	repository := &syntheticCanaryWorkerRepository{}
	worker := domainintegrations.NewWorker(domainintegrations.WorkerConfig{
		Repository: repository, Executor: syntheticCanaryWorkerExecutor{err: errors.New(canary)}, WorkerID: "synthetic-worker",
	})
	var output bytes.Buffer
	observed := make(chan struct{})
	logger := slog.New(notifyingLogHandler{
		next: slog.NewJSONHandler(&output, nil), once: &sync.Once{}, observed: observed,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runIntegrationWorkerLoop(ctx, worker, time.Millisecond, logger)
		close(done)
	}()
	select {
	case <-observed:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("integration worker log was not emitted")
	}
	<-done
	body := output.String()
	if !strings.Contains(body, "integration recording worker failed") || !strings.Contains(body, "authorization.denied") {
		t.Fatalf("safe integration worker log=%s", body)
	}
	jsonCanary, err := json.Marshal(canary)
	if err != nil {
		t.Fatalf("encode synthetic worker canary: %v", err)
	}
	for _, representation := range []string{
		canary,
		string(jsonCanary[1 : len(jsonCanary)-1]),
		base64.StdEncoding.EncodeToString([]byte(canary)),
		base64.RawStdEncoding.EncodeToString([]byte(canary)),
		base64.URLEncoding.EncodeToString([]byte(canary)),
		base64.RawURLEncoding.EncodeToString([]byte(canary)),
	} {
		if representation != "" && strings.Contains(body, representation) {
			t.Fatalf("synthetic-only worker log canary leaked: %q", representation)
		}
	}
}
