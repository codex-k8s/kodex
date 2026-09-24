package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/integrationegress"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

const integrationEgressReconcileInterval = 30 * time.Second
const integrationEgressOperationTimeout = 2 * time.Minute

// Дополнительная сеть не участвует в root /readyz: отсутствие внешнего
// OpenAPI origin не должно останавливать SSO, проекты и локальный control-plane.
type integrationEgressProjection struct {
	repository        *platformrepository.Repository
	ready             func(context.Context) error
	baseDigest        string
	kubernetesTimeout time.Duration
}

func (projection *integrationEgressProjection) Run(ctx context.Context) error {
	ticker := time.NewTicker(integrationEgressReconcileInterval)
	defer ticker.Stop()
	for {
		operation, cancel := context.WithTimeout(ctx, integrationEgressOperationTimeout)
		err := projection.reconcile(operation)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "integration egress projection reconciliation failed", "error_class", "integration_egress_projection")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (projection *integrationEgressProjection) reconcile(ctx context.Context) error {
	if projection.ready == nil || projection.ready(ctx) != nil {
		return integrationegress.ErrUnavailable
	}
	servers, err := dnsresolver.LoadSystemServers("/etc/resolv.conf")
	if err != nil {
		return err
	}
	resolver, err := dnsresolver.New(dnsresolver.Config{
		MinimumTTLSeconds: 5, MaximumTTLSeconds: 300, MaximumCacheEntries: 128,
		MaximumQueries: 8, MaximumCNAMEDepth: 8, MaximumRecords: 64,
		MaximumMessageBytes: 4096, QueryTimeoutMilliseconds: 2000,
	}, servers, nil, nil)
	if err != nil {
		return err
	}
	document, err := projection.repository.PrepareIntegrationEgressProjection(ctx, projection.baseDigest, integrationDNSResolver{resolver})
	if err != nil {
		return err
	}
	publisher, err := integrationegress.InCluster(projection.kubernetesTimeout)
	if err != nil {
		return err
	}
	return publisher.Publish(ctx, document)
}

type integrationDNSResolver struct{ *dnsresolver.Resolver }

func (resolver integrationDNSResolver) Resolve(ctx context.Context, host string) (shared.Snapshot, error) {
	snapshot, err := resolver.Resolver.Refresh(ctx, host)
	return shared.Snapshot{Addresses: snapshot.Addresses, ExpiresAt: snapshot.ExpiresAt}, err
}
