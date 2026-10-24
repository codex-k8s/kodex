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

// integrationEgressReconcileInterval sets the thirty-second polling interval for the optional
// integration egress projection worker.
// integrationEgressReconcileInterval задаёт тридцатисекундный polling интервал необязательного
// worker проекции integration egress.
const integrationEgressReconcileInterval = 30 * time.Second

// integrationEgressOperationTimeout bounds one integration-egress reconciliation to two minutes
// and suppresses transient deployment-ready warnings only within that initial pending window.
// integrationEgressOperationTimeout ограничивает одно согласование integration egress двумя
// минутами и подавляет временные deployment-ready предупреждения лишь внутри начального окна
// pending.
const integrationEgressOperationTimeout = 2 * time.Minute

// integrationEgressDNSSamples requires four independently refreshed DNS samples before publishing
// their bounded union.
// integrationEgressDNSSamples требует четыре независимо обновлённые DNS выборки до публикации их
// ограниченного объединения.
const integrationEgressDNSSamples = 4

// integrationEgressDNSOverlap retains previously verified public DNS pins for two minutes since
// their last observation, bounded together with current pins.
// integrationEgressDNSOverlap сохраняет ранее проверенные публичные DNS pins две минуты с
// последнего наблюдения с общим ограничением вместе с текущими pins.
const integrationEgressDNSOverlap = 2 * time.Minute

// integrationEgressProjection coordinates owner-derived egress policy with bounded DNS
// observations and Kubernetes publication; this optional path does not determine root readiness.
// integrationEgressProjection согласует egress policy владельца с ограниченными DNS наблюдениями и
// Kubernetes публикацией; этот optional путь не определяет root readiness.
type integrationEgressProjection struct {
	repository        *platformrepository.Repository
	ready             func(context.Context) error
	baseDigest        string
	kubernetesTimeout time.Duration
	resolver          *dnsresolver.Resolver
	pins              *integrationDNSResolver
	pending           *shared.Document
	pendingSince      time.Time
}

// Run reconciles immediately and on the fixed interval with child deadlines, returning on
// cancellation and logging only a fixed class plus stage with a bounded pending-rollout grace
// window.
// Run согласует сразу и по фиксированному интервалу с дочерними deadlines, завершается при отмене
// и записывает лишь фиксированный класс и stage с ограниченным окном ожидания pending rollout.
func (projection *integrationEgressProjection) Run(ctx context.Context) error {
	ticker := time.NewTicker(integrationEgressReconcileInterval)
	defer ticker.Stop()
	for {
		operation, cancel := context.WithTimeout(ctx, integrationEgressOperationTimeout)
		stage, err := projection.reconcile(operation)
		cancel()
		if err != nil && ctx.Err() == nil &&
			(stage != "kubernetes_readback_deployment_ready" || time.Since(projection.pendingSince) >= integrationEgressOperationTimeout) {
			slog.WarnContext(ctx, "integration egress projection reconciliation failed", "error_class", "integration_egress_projection", "safe_stage", stage)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// reconcile requires owner readiness, lazily creates DNS dependencies, discards pending policy
// when source hostnames change and retries the same prepared document until staged Kubernetes
// publication succeeds.
// reconcile требует owner readiness, лениво создаёт DNS зависимости, удаляет pending policy при
// смене source hostnames и повторяет тот же подготовленный документ до успешной staged Kubernetes
// публикации.
func (projection *integrationEgressProjection) reconcile(ctx context.Context) (string, error) {
	if projection.ready == nil || projection.ready(ctx) != nil {
		return "readiness", integrationegress.ErrUnavailable
	}
	if projection.resolver == nil {
		servers, err := dnsresolver.LoadSystemServers("/etc/resolv.conf")
		if err != nil {
			return "dns_configuration", err
		}
		projection.resolver, err = dnsresolver.New(dnsresolver.Config{
			MinimumTTLSeconds: 5, MaximumTTLSeconds: 300, MaximumCacheEntries: 128,
			MaximumQueries: 8, MaximumCNAMEDepth: 8, MaximumRecords: 64,
			MaximumMessageBytes: 4096, QueryTimeoutMilliseconds: 2000,
		}, servers, nil, nil)
		if err != nil {
			return "dns_configuration", err
		}
	}
	if projection.pins == nil {
		projection.pins = &integrationDNSResolver{source: projection.resolver}
	}
	if projection.pending != nil {
		hosts, err := projection.repository.IntegrationEgressHostnames(ctx)
		if err != nil {
			return "owner_projection", err
		}
		if projection.pending.SourceDigest != shared.SourceDigest(hosts) {
			projection.pending = nil
		}
	}
	if projection.pending == nil {
		document, err := projection.repository.PrepareIntegrationEgressProjection(ctx, projection.baseDigest, projection.pins)
		if err != nil {
			return "owner_projection", err
		}
		projection.pending = &document
		projection.pendingSince = time.Now()
	}
	publisher, err := integrationegress.InCluster(projection.kubernetesTimeout)
	if err != nil {
		return "kubernetes_client", err
	}
	stage, err := publisher.PublishWithStage(ctx, *projection.pending)
	if err == nil {
		projection.pending = nil
	}
	return "kubernetes_" + stage, err
}

// integrationDNSRefresher supplies a fresh bounded DNS snapshot rather than treating a prior
// cached answer as an independent observation.
// integrationDNSRefresher предоставляет свежий ограниченный DNS snapshot вместо признания прежнего
// cached ответа независимым наблюдением.
type integrationDNSRefresher interface {
	// Refresh refreshes one exact hostname under the caller context and returns addresses with their
	// expiry or a resolver failure.
	// Refresh обновляет один точный hostname под caller context и возвращает адреса с их сроком либо
	// отказ resolver.
	Refresh(context.Context, string) (dnsresolver.Snapshot, error)
}

// integrationDNSResolver tracks verified public pins per hostname with synchronized recent-history
// overlap and an injectable retention clock.
// integrationDNSResolver отслеживает проверенные публичные pins каждого hostname с
// синхронизированным перекрытием недавней истории и заменяемыми часами хранения.
type integrationDNSResolver struct {
	source   integrationDNSRefresher
	now      func() time.Time
	mu       sync.Mutex
	observed map[string]map[netip.Addr]time.Time
}

// Resolve requires four fresh valid public samples, bounds current/retained union to thirty-two
// addresses, expires old observations and returns sorted pins with earliest current expiry;
// rejected overlap does not replace the retained set.
// Resolve требует четыре свежих правильных публичных выборки, ограничивает current и retained
// union тридцатью двумя адресами, удаляет старые наблюдения и возвращает отсортированные pins с
// ранним текущим сроком; отклонённое перекрытие не заменяет retained set.
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
