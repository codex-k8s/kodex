// Package assistantconversationpurge удаляет диалоги после срока корзины.
package assistantconversationpurge

import (
	"context"
	"log/slog"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

type source interface {
	PurgeDueAssistantConversations(context.Context, int32) error
}

func Run(source source, logger *slog.Logger) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			cycle, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := source.PurgeDueAssistantConversations(cycle, 25); err != nil && cycle.Err() == nil {
				logger.WarnContext(ctx, "assistant conversation purge cycle failed", "error_class", "purge_cycle")
			}
			cancel()
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
}
