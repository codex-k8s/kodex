// Package runner реализует один bounded claim/build/complete цикл.
package runner

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/build"
	"github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/clients/controlplane"
	"github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/observability"
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
	prepared, err := runner.executor.Prepare(claim.Input)
	if err != nil {
		runner.metrics.Observe("context", "rejected")
		return errors.Join(err, runner.client.Fail(ctx, claim, uuid.NewString(), "CONTEXT_INVALID"))
	}
	defer prepared.Close()
	if err := runner.client.Report(ctx, &claim, uuid.NewString(),
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_CONTEXT_VALIDATION, 15); err != nil {
		return err
	}
	if err := runner.client.Report(ctx, &claim, uuid.NewString(),
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING, 30); err != nil {
		return err
	}
	buildContext, cancelBuild := context.WithCancel(ctx)
	defer cancelBuild()
	type buildResult struct {
		evidence controlplane.BuildEvidence
		err      error
	}
	result := make(chan buildResult, 1)
	go func() {
		evidence, buildErr := runner.executor.Build(buildContext, prepared, claim.Attempt)
		result <- buildResult{evidence: evidence, err: buildErr}
	}()
	ticker := time.NewTicker(runner.config.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancelBuild()
			<-result
			return errors.Join(ctx.Err(), runner.client.Fail(context.WithoutCancel(ctx), claim, uuid.NewString(), "BUILD_CANCELLED"))
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
				return errors.Join(completed.err, runner.client.Fail(ctx, claim, uuid.NewString(), "BUILDKIT_FAILED"))
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
