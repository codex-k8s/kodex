package controller

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"k8s.io/apimachinery/pkg/types"
)

// errWorkspaceOtherConsumer distinguishes a bounded release wait from an unknown PVC observation.
// errWorkspaceOtherConsumer отличает ограниченное ожидание освобождения от неизвестного наблюдения PVC.
var errWorkspaceOtherConsumer = errors.New("session workspace source has another consumer")

// waitWorkspaceSource allows an acknowledged snapshot's terminal Pod to disappear before a restore writes.
// Only a positively observed competing consumer is retried; missing custody and API errors fail closed.
// waitWorkspaceSource ожидает исчезновения terminal Pod подтверждённого snapshot до записи restore.
// Повторяется только наблюдение конкурирующего consumer; неизвестная custody и ошибки API закрыто отклоняются.
func (controller *Controller) waitWorkspaceSource(ctx context.Context, task model.Task, uid types.UID, renew func(context.Context) error) error {
	wait, cancel := context.WithTimeout(ctx, archiveCleanupTimeout)
	defer cancel()
	for {
		err := controller.checkWorkspaceSource(wait, task, uid, "")
		if task.Kind != "RESTORE" || !errors.Is(err, errWorkspaceOtherConsumer) {
			return err
		}
		if err := renewAuthority(wait, renew); err != nil {
			return err
		}
		timer := time.NewTimer(controller.poll)
		select {
		case <-wait.Done():
			timer.Stop()
			return wait.Err()
		case <-timer.C:
		}
	}
}
