package app

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/integrationegress"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

const integrationEgressReconcileInterval = 30 * time.Second
const integrationEgressOperationTimeout = 2 * time.Minute
const integrationEgressDNSSamples = 4
const integrationEgressDNSOverlap = 2 * time.Minute

// Дополнительная сеть не участвует в root /readyz: отсутствие внешнего
// OpenAPI origin не должно останавливать SSO, проекты и локальный control-plane.
type integrationEgressProjection struct {
	repository        *platformrepository.Repository
	ready             func(context.Context) error
	baseDigest        string
	kubernetesTimeout time.Duration
	resolver          *dnsresolver.Resolver
	pins              *integrationDNSResolver
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
	if projection.resolver == nil {
		servers, err := dnsresolver.LoadSystemServers("/etc/resolv.conf")
		if err != nil {
			return err
		}
		projection.resolver, err = dnsresolver.New(dnsresolver.Config{
			MinimumTTLSeconds: 5, MaximumTTLSeconds: 300, MaximumCacheEntries: 128,
			MaximumQueries: 8, MaximumCNAMEDepth: 8, MaximumRecords: 64,
			MaximumMessageBytes: 4096, QueryTimeoutMilliseconds: 2000,
		}, servers, nil, nil)
		if err != nil {
			return err
		}
	}
	if projection.pins == nil {
		projection.pins = &integrationDNSResolver{source: projection.resolver}
	}
	document, err := projection.repository.PrepareIntegrationEgressProjection(ctx, projection.baseDigest, projection.pins)
	if err != nil {
		return err
	}
	publisher, err := integrationegress.InCluster(projection.kubernetesTimeout)
	if err != nil {
		return err
	}
	return publisher.Publish(ctx, document)
}

type integrationDNSRefresher interface {
	Refresh(context.Context, string) (dnsresolver.Snapshot, error)
}

type integrationDNSResolver struct {
	source   integrationDNSRefresher
	now      func() time.Time
	mu       sync.Mutex
	observed map[string]map[netip.Addr]time.Time
}

func (resolver *integrationDNSResolver) Resolve(ctx context.Context, host string) (shared.Snapshot, error) {
	if resolver.source == nil {
		return shared.Snapshot{}, integrationegress.ErrUnavailable
	}
	current := make(map[netip.Addr]struct{})
	var earliest time.Time
	for range integrationEgressDNSSamples {
		snapshot, err := resolver.source.Refresh(ctx, host)
		if err != nil || ctx.Err() != nil || mailpolicy.ValidateAddresses(snapshot.Addresses) != nil || !time.Now().Before(snapshot.ExpiresAt) {
			return shared.Snapshot{}, integrationegress.ErrUnavailable
		}
		if earliest.IsZero() || snapshot.ExpiresAt.Before(earliest) {
			earliest = snapshot.ExpiresAt
		}
		for _, address := range snapshot.Addresses {
			current[address] = struct{}{}
		}
		if len(current) > 32 {
			return shared.Snapshot{}, integrationegress.ErrUnavailable
		}
	}
	now := time.Now()
	if resolver.now != nil {
		now = resolver.now()
	}
	if !now.Before(earliest) {
		return shared.Snapshot{}, integrationegress.ErrUnavailable
	}
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if resolver.observed == nil {
		resolver.observed = make(map[string]map[netip.Addr]time.Time)
	}
	for hostname, pins := range resolver.observed {
		for address, seen := range pins {
			if !now.Before(seen.Add(integrationEgressDNSOverlap)) {
				delete(pins, address)
			}
		}
		if len(pins) == 0 {
			delete(resolver.observed, hostname)
		}
	}
	retained := make(map[netip.Addr]time.Time)
	for address, seen := range resolver.observed[host] {
		retained[address] = seen
	}
	for address := range current {
		retained[address] = now
	}
	if len(retained) > 32 {
		return shared.Snapshot{}, integrationegress.ErrUnavailable
	}
	resolver.observed[host] = retained
	result := make([]netip.Addr, 0, len(retained))
	for address := range retained {
		result = append(result, address)
	}
	slices.SortFunc(result, netip.Addr.Compare)
	return shared.Snapshot{Addresses: result, ExpiresAt: earliest}, nil
}
