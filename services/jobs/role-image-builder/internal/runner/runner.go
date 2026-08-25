// Package runner реализует один bounded claim/build/complete цикл.
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

type Config struct{ RenewInterval time.Duration }

type Runner struct {
	client   *controlplane.Client
	executor *build.Executor
	metrics  *observability.Metrics
	config   Config
}

func New(client *controlplane.Client, executor *build.Executor, metrics *observability.Metrics, config Config) (*Runner, error) {
	if client == nil || executor == nil || metrics == nil || config.RenewInterval < 5*time.Second || config.RenewInterval > time.Minute {
		return nil, errors.New("role image builder runner configuration is invalid")
	}
	return &Runner{client: client, executor: executor, metrics: metrics, config: config}, nil
}

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
			diagnostic, "Immutable build input was rejected"))
	}
	defer prepared.Close()
	runner.metrics.Observe("materialize", "success")
	runner.metrics.Observe("context", "success")
	buildContext, cancelBuild := context.WithCancel(ctx)
	defer cancelBuild()
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
