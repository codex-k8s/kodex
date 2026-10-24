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

// emailProjectionStore reads the authoritative accepted email configuration and its exact
// credential commitments for legacy projection reconciliation.
// emailProjectionStore читает авторитетную принятую email конфигурацию и её точные commitments
// credentials для legacy согласования проекции.
type emailProjectionStore interface {
	// EmailConfiguration returns the owner-accepted email configuration rather than selecting a
	// configured candidate as active projection state.
	// EmailConfiguration возвращает принятую владельцем email конфигурацию вместо выбора configured
	// candidate как активного состояния проекции.
	EmailConfiguration(context.Context) (api.Configuration, error)
	// EmailCredentialDigests reads credential commitments for the supplied exact accepted
	// configuration without exposing materialized credentials.
	// EmailCredentialDigests читает commitments credentials переданной точной принятой конфигурации
	// без раскрытия материализованных credentials.
	EmailCredentialDigests(context.Context, api.Configuration) (map[string]string, error)
}

// emailProjectionPublisher separates publishing from authoritative projection readback so
// readiness need not perform a mutation.
// emailProjectionPublisher разделяет публикацию и авторитетный readback проекции, чтобы readiness
// не требовал mutation.
type emailProjectionPublisher interface {
	// Publish publishes the accepted email configuration and bound credential digests, returning the
	// projection receipt or a dependency failure.
	// Publish публикует принятую email конфигурацию и связанные digests credentials с возвратом
	// квитанции проекции либо отказа зависимости.
	Publish(context.Context, api.Configuration, map[string]string) (emailprojection.Receipt, error)
	// Check reads back exact configuration and credential commitments without publishing a change.
	// Check читает точные configuration и commitments credentials без публикации изменения.
	Check(context.Context, api.Configuration, map[string]string) (emailprojection.Receipt, error)
}

// emailProjection owns periodic bounded email reconciliation and readiness, optionally delegating
// exact mailbox publication work to the owner-claim delivery path.
// emailProjection владеет периодическим ограниченным email согласованием и readiness с
// необязательной передачей точной работы публикации mailbox пути доставки по claim владельца.
type emailProjection struct {
	store             emailProjectionStore
	publisher         emailProjectionPublisher
	interval, timeout time.Duration
	delivery          *mailboxDelivery
}

// constructEmailCredentialMaterializer constructs the in-cluster credential materializer for the
// configured namespace, fixed email projection Secret and Kubernetes API budget.
// constructEmailCredentialMaterializer создаёт in-cluster materializer credentials для заданного
// namespace, фиксированного Secret email проекции и бюджета Kubernetes API.
func constructEmailCredentialMaterializer(config Config) (*credentialmaterializer.Kubernetes, error) {
	return credentialmaterializer.InCluster(config.IntegrationCredentialNamespace, emailprojection.SecretName, config.KubernetesAPITimeout)
}

// initializeEmailProjection returns nil when disabled, imports bounded configured bytes into the
// owner and constructs publisher/delivery dependencies, checking admission/readiness without
// running a reconciliation publish.
// initializeEmailProjection возвращает nil при отключении, импортирует ограниченные configured
// bytes владельцу и создаёт publisher и delivery зависимости с проверкой admission и readiness без
// publish согласования.
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
	projection.delivery, err = newMailboxDelivery(repository, publisher, config.EmailGatewayPolicyDigest)
	if err != nil {
		return nil, err
	}
	if err := publisher.CheckPublicationAdmission(ctx); err != nil {
		return nil, err
	}
	projection.delivery.sourceFile = config.EmailConfigurationFile
	if err := projection.Check(ctx); err != nil {
		return nil, err
	}
	return projection, nil
}

// reconcile prefers the mailbox delivery path, suppressing legacy replay whenever handled or
// failed; otherwise reads accepted owner state, publishes it and performs readback.
// reconcile предпочитает путь доставки mailbox с запретом legacy replay при handled либо ошибке;
// иначе читает принятое owner state, публикует его и выполняет readback.
func (projection *emailProjection) reconcile(ctx context.Context) error {
	if projection.delivery != nil {
		found, err := projection.delivery.reconcile(ctx)
		if found || err != nil {
			return err
		}
	}
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

// Check treats nil as disabled, checks admission only for owner publication delivery and otherwise
// reads accepted configuration/commitments before publisher readback without publishing.
// Check считает nil отключением, проверяет только admission для owner publication delivery и иначе
// читает принятые configuration и commitments до publisher readback без публикации.
func (projection *emailProjection) Check(ctx context.Context) error {
	if projection == nil {
		return nil
	}
	if projection.delivery != nil {
		// PENDING не закрывает callback endpoint: READY принадлежит publication.
		return projection.delivery.publisher.CheckPublicationAdmission(ctx)
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

// Run waits for cancellation when disabled and otherwise reconciles on a ticker with a child
// timeout, logging only a fixed failure classification before joining on caller cancellation.
// Run ожидает отмену при отключении и иначе согласует по ticker с дочерним timeout, записывая лишь
// фиксированную классификацию отказа и завершаясь при отмене caller.
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
