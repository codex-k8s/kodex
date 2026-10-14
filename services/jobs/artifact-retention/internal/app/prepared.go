package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/prepared"
)

const preparedCycleDegraded = "prepared content cleanup cycle degraded"
const preparedCycleRestored = "prepared content cleanup cycle restored"

func runPreparedLoop(processor *prepared.Processor, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		backoff := serviceruntime.NewIdleBackoff(config.PollInterval, 5*time.Minute)
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			processed, err := processor.Process(cycle, config.InstanceID, config.BatchSize, int64(config.ClaimLease/time.Second))
			cancel()
			if err != nil && !degraded {
				logger.WarnContext(ctx, preparedCycleDegraded)
				degraded = true
			}
			if err == nil && degraded {
				logger.InfoContext(ctx, preparedCycleRestored)
				degraded = false
			}
			timer := time.NewTimer(backoff.Next(processed > 0 || err != nil))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
