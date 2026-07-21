package recording

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	domain "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
)

// Executor является безопасным PostgreSQL recording-test executor без внешних SDK.
type Executor struct {
	store  domain.RecordingStore
	now    func() time.Time
	random io.Reader
}

var _ domain.RecordingExecutor = (*Executor)(nil)

func New(store domain.RecordingStore, now func() time.Time, randomSource io.Reader) *Executor {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &Executor{store: store, now: now, random: randomSource}
}

// Execute записывает или читает единственную immutable receipt для invocation.
func (executor *Executor) Execute(ctx context.Context, claim domain.ExecutionClaim) (domain.ExecutionReceipt, error) {
	if executor == nil || executor.store == nil {
		return domain.ExecutionReceipt{}, domain.ErrNoExecution
	}
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(executor.random, buffer); err != nil {
		return domain.ExecutionReceipt{}, fmt.Errorf("generate recording execution identity: %w", err)
	}
	executionID := "exec_" + hex.EncodeToString(buffer)
	return executor.store.RecordExecution(ctx, claim, executionID, executor.now().UTC())
}
