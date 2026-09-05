package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/credentialmaterializer"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/emailprojection"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

type emailProjectionStore interface {
	EmailConfiguration(context.Context) (api.Configuration, error)
	EmailCredentialDigests(context.Context, api.Configuration) (map[string]string, error)
}

type emailProjectionPublisher interface {
	Publish(context.Context, api.Configuration, map[string]string) (emailprojection.Receipt, error)
	Check(context.Context, api.Configuration, map[string]string) (emailprojection.Receipt, error)
}

type emailProjection struct {
	store             emailProjectionStore
	publisher         emailProjectionPublisher
	interval, timeout time.Duration
}

func constructEmailCredentialMaterializer(config Config) (*credentialmaterializer.Kubernetes, error) {
	return credentialmaterializer.InCluster(config.IntegrationCredentialNamespace, emailprojection.SecretName, config.KubernetesAPITimeout)
}

func initializeEmailProjection(ctx context.Context, repository *platformrepository.Repository, config Config) (*emailProjection, error) {
	if config.EmailConfigurationFile == "" {
		return nil, nil
	}
	raw, err := readBoundedFileLimit(config.EmailConfigurationFile, 24<<20)
	if err != nil {
		return nil, errors.New("read email configuration")
	}
	if _, err := repository.InitializeEmailConfiguration(ctx, raw); err != nil {
		return nil, errors.New("initialize email configuration")
	}
	publisher, err := emailprojection.InCluster(config.IntegrationCredentialNamespace, config.KubernetesAPITimeout)
	if err != nil {
		return nil, err
	}
	projection := &emailProjection{store: repository, publisher: publisher, interval: config.ReadinessInterval, timeout: config.ReadinessTimeout}
	if err := projection.reconcile(ctx); err != nil {
		return nil, err
	}
	return projection, nil
}

func (projection *emailProjection) reconcile(ctx context.Context) error {
	configuration, err := projection.store.EmailConfiguration(ctx)
	if err != nil {
		return err
	}
	digests, err := projection.store.EmailCredentialDigests(ctx, configuration)
	if err != nil {
		return err
	}
	if _, err := projection.publisher.Publish(ctx, configuration, digests); err != nil {
		return err
	}
	return projection.Check(ctx)
}

func (projection *emailProjection) Check(ctx context.Context) error {
	if projection == nil {
		return nil
	}
	configuration, err := projection.store.EmailConfiguration(ctx)
	if err != nil {
		return err
	}
	digests, err := projection.store.EmailCredentialDigests(ctx, configuration)
	if err != nil {
		return err
	}
	_, err = projection.publisher.Check(ctx, configuration, digests)
	return err
}

func (projection *emailProjection) Run(ctx context.Context) error {
	if projection == nil {
		<-ctx.Done()
		return nil
	}
	ticker := time.NewTicker(projection.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			operation, cancel := context.WithTimeout(ctx, projection.timeout)
			err := projection.reconcile(operation)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.WarnContext(ctx, "email projection reconciliation failed", "error_class", "email_projection")
			}
		}
	}
}
