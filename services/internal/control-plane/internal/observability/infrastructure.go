package observability

import (
	"github.com/codex-k8s/matter-codex/libs/go/cache/redisstore"
	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

func RegisterInfrastructureMetrics(
	register func(...prometheus.Collector) error,
	runtimePool, relayPool *pgxpool.Pool,
	redis *redisstore.Store,
	relay *eventing.Relay,
) error {
	collectors := []prometheus.Collector{
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "control_plane",
			Name: "pgx_runtime_acquired_connections",
			Help: "Current acquired connections in the primary pgx pool.",
		}, func() float64 { return float64(runtimePool.Stat().AcquiredConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "control_plane",
			Name: "pgx_runtime_idle_connections",
			Help: "Current idle connections in the primary pgx pool.",
		}, func() float64 { return float64(runtimePool.Stat().IdleConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "control_plane",
			Name: "pgx_relay_acquired_connections",
			Help: "Current acquired connections in the relay pgx pool.",
		}, func() float64 { return float64(relayPool.Stat().AcquiredConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "control_plane",
			Name: "redis_total_connections",
			Help: "Current total connections in the Redis client pool.",
		}, func() float64 { return float64(redis.PoolStats().Total) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "control_plane",
			Name: "redis_idle_connections",
			Help: "Current idle connections in the Redis client pool.",
		}, func() float64 { return float64(redis.PoolStats().Idle) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "control_plane",
			Name: "outbox_events_claimed_total",
			Help: "Total durable outbox events claimed by the relay.",
		}, func() float64 { return float64(relay.Stats().Claimed) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "control_plane",
			Name: "outbox_events_published_total",
			Help: "Total outbox events acknowledged by JetStream and finalized.",
		}, func() float64 { return float64(relay.Stats().Published) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "control_plane",
			Name: "outbox_events_failed_total",
			Help: "Total failed outbox publish attempts durably rescheduled.",
		}, func() float64 { return float64(relay.Stats().Failed) }),
	}
	return register(collectors...)
}
