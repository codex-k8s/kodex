package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/capture"
)

// runCaptureLoop owns one bounded task and all its renewal workers through positive join.
// Errors preserve the durable task for fenced recovery; they never manufacture artifact success.
// runCaptureLoop владеет одной bounded task и всеми renewal workers до positive join.
// Ошибки сохраняют durable task для fenced recovery, не создавая artifact success.
func runCaptureLoop(processor *capture.Processor, metrics *retentionMetrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		degraded := false
		for {
			check, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			err := processor.Check(check, config.InstanceID)
			cancel()
			if err == nil {
				cycle, stop := context.WithTimeout(ctx, config.CaptureTimeout)
				var result capture.Result
				result, err = processor.Process(cycle, config.InstanceID, int64(config.CaptureLease/time.Second))
				metrics.recordCapture(result)
				stop()
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				if !degraded {
					logger.WarnContext(ctx, "artifact capture cycle incomplete", "error_class", "capture")
					degraded = true
				}
			} else if degraded {
				logger.InfoContext(ctx, "artifact capture cycle restored")
				degraded = false
			}
			timer := time.NewTimer(config.CapturePollInterval)
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
