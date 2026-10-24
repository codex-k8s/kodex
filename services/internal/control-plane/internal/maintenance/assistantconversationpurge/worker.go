// Package assistantconversationpurge удаляет диалоги после срока корзины.
package assistantconversationpurge

import (
	"context"
	"log/slog"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// source exposes only owner-side purging of due assistant conversations in bounded batches rather
// than granting arbitrary conversation deletion.
// source предоставляет только owner purge наступивших к удалению диалогов помощника ограниченными
// пакетами вместо допуска произвольного удаления диалога.
type source interface {
	// PurgeDueAssistantConversations asks the owner to purge at most the supplied batch limit
	// according to its authoritative due-time and lifecycle rules.
	// PurgeDueAssistantConversations запрашивает у владельца purge не более переданного размера пакета
	// по его авторитетным due time и lifecycle правилам.
	PurgeDueAssistantConversations(context.Context, int32) error
}

// Run returns a worker that purges up to twenty-five due conversations immediately and each minute
// with a thirty-second child budget, logging fixed failures only before that budget expires.
// Run возвращает worker purge до двадцати пяти наступивших диалогов сразу и каждую минуту с
// дочерним бюджетом тридцать секунд и фиксированными сообщениями отказа лишь до истечения бюджета.
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
