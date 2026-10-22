// Package runner coordinates one context-controlled claim/materialize/build/complete cycle with the authoritative owner client.
// Пакет runner координирует один управляемый контекстом цикл claim, материализации, сборки и завершения с авторитетным клиентом владельца.
package runner

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/build"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/clients/controlplane"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/observability"
	"github.com/google/uuid"
)

// Config sets the lease-renew ticker interval while the asynchronous build is active; preparation runs before that ticker starts.
// Config задаёт интервал продления аренды во время асинхронной сборки; подготовка выполняется до запуска этого ticker.
type Config struct{ RenewInterval time.Duration }

// Runner binds the owner RPC client, local build executor and business counters without owning their process-level shutdown.
// Runner связывает owner RPC-клиент, локальный исполнитель сборки и бизнес-счётчики, не владея их остановкой на уровне процесса.
type Runner struct {
	client   *controlplane.Client
	executor *build.Executor
	metrics  *observability.Metrics
	config   Config
}

// New requires all three runtime dependencies and a renew interval from five seconds through one minute, without claiming work.
// New требует все три зависимости среды и интервал продления от пяти секунд до минуты, не захватывая работу.
func New(client *controlplane.Client, executor *build.Executor, metrics *observability.Metrics, config Config) (*Runner, error) {
	if client == nil || executor == nil || metrics == nil || config.RenewInterval < 5*time.Second || config.RenewInterval > time.Minute {
		return nil, errors.New("role image builder runner configuration is invalid")
	}
	return &Runner{client: client, executor: executor, metrics: metrics, config: config}, nil
}

// Cycle claims one candidate, materializes pinned inputs and multiplexes build progress, lease renewals and completion.
// Cancellation or progress/renew failure cancels and joins the build before workspace cleanup; cancellation also attempts a bounded owner failure RPC.
// Build/materialization errors report closed diagnostics, while successful evidence still requires owner Complete; no image admission is granted here.
// The caller owns the overall deadline; the renewal ticker begins only after synchronous preparation has succeeded.
// Cycle захватывает одного кандидата, материализует закреплённые входы и совмещает прогресс сборки, продление аренды и завершение.
// Отмена или ошибка прогресса либо продления отменяет и ожидает сборку до очистки рабочего тома; отмена также пытается сообщить ограниченный по времени owner failure RPC.
// Ошибки сборки и материализации передают закрытую диагностику, а успешные доказательства ещё требуют owner Complete; допуск образа здесь не выдаётся.
// Общий срок задаёт вызывающий; ticker продления начинается только после успешной синхронной подготовки.
func (runner *Runner) Cycle(ctx context.Context) error {
	claim, err := runner.client.Claim(ctx, uuid.NewString())
	if errors.Is(err, controlplane.ErrNoWork) {
		runner.metrics.Observe("claim", "empty")
		return nil
	}
	if err != nil {
		runner.metrics.Observe("claim", "error")
		return err
	}
	runner.metrics.Observe("claim", "success")
	if err := runner.client.Report(ctx, &claim, uuid.NewString(),
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_MATERIALIZATION, 5); err != nil {
		return err
	}
	prepared, diagnostic, err := runner.executor.Prepare(ctx, claim.Input, func() error {
		return runner.client.Report(ctx, &claim, uuid.NewString(),
			controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_CONTEXT_VALIDATION, 15)
	})
	if err != nil {
		if diagnostic == "CONTEXT_REPORT_REJECTED" {
			return err
		}
		runner.metrics.Observe("materialize", "rejected")
		errorCode := "MATERIALIZATION_FAILED"
		if diagnostic == "ARCHIVE_REJECTED" {
			errorCode = "CONTEXT_INVALID"
		}
		return errors.Join(err, runner.client.Fail(ctx, claim, uuid.NewString(), errorCode,
			diagnostic, build.InputRejectionSummary(err)))
	}
	defer prepared.Close()
	runner.metrics.Observe("materialize", "success")
	runner.metrics.Observe("context", "success")
	buildContext, cancelBuild := context.WithCancel(ctx)
	defer cancelBuild()
	// buildResult transfers the executor's final evidence/error through one buffered completion message to the coordinating cycle.
	// buildResult передаёт итоговые доказательства и ошибку исполнителя одним буферизованным сообщением завершения координирующему циклу.
	type buildResult struct {
		evidence controlplane.BuildEvidence
		err      error
	}
	result := make(chan buildResult, 1)
	phases := make(chan build.Phase, 8)
	go func() {
		evidence, buildErr := runner.executor.Build(buildContext, prepared, claim.Attempt, phases)
		result <- buildResult{evidence: evidence, err: buildErr}
	}()
	ticker := time.NewTicker(runner.config.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancelBuild()
			<-result
			return errors.Join(ctx.Err(), runner.client.Fail(context.WithoutCancel(ctx), claim, uuid.NewString(),
				"BUILD_CANCELLED", "LEASE_REVOKED", "Build was cancelled"))
		case phase := <-phases:
			if err := runner.client.Report(ctx, &claim, uuid.NewString(), phase.Stage, phase.Percent); err != nil {
				cancelBuild()
				<-result
				return err
			}
		case <-ticker.C:
			if renewErr := runner.client.Renew(ctx, &claim, uuid.NewString()); renewErr != nil {
				cancelBuild()
				<-result
				runner.metrics.Observe("renew", "error")
				return renewErr
			}
			runner.metrics.Observe("renew", "success")
		case completed := <-result:
			if completed.err != nil {
				runner.metrics.Observe("build", "error")
				failure := build.Failure{ErrorCode: "SOLVE_FAILED", DiagnosticCode: "BUILD_GRAPH_REJECTED",
					DiagnosticSummary: "Build solve failed"}
				_ = errors.As(completed.err, &failure)
				return errors.Join(completed.err, runner.client.Fail(ctx, claim, uuid.NewString(), failure.ErrorCode,
					failure.DiagnosticCode, failure.DiagnosticSummary))
			}
			runner.metrics.Observe("build", "success")
			if err := runner.client.Report(ctx, &claim, uuid.NewString(),
				controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_PROVENANCE, 90); err != nil {
				return err
			}
			if err := runner.client.Complete(ctx, claim, uuid.NewString(), completed.evidence); err != nil {
				runner.metrics.Observe("complete", "error")
				return err
			}
			runner.metrics.Observe("complete", "success")
			return nil
		}
	}
}
