// Package ops provides a bounded in-memory safe ops feed for codex-hook-ingress.
package ops

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	opsrepo "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/repository/ops"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

const (
	defaultCapacity  = 1024
	defaultRetention = 15 * time.Minute
)

var _ opsrepo.Repository = (*Repository)(nil)

// Config controls bounded in-memory feed retention for the service skeleton.
type Config struct {
	Capacity  int
	Retention time.Duration
}

// Repository stores bounded safe diagnostics without raw payload persistence.
type Repository struct {
	mu        sync.Mutex
	capacity  int
	retention time.Duration
	entries   []value.OpsFeedEntry
	reserved  map[uuid.UUID]struct{}
	metrics   value.OpsDiagnosticsSnapshot
}

// NewRepository creates an in-memory ops feed stub with bounded retention.
func NewRepository(cfg Config) *Repository {
	if cfg.Capacity <= 0 {
		cfg.Capacity = defaultCapacity
	}
	if cfg.Retention <= 0 {
		cfg.Retention = defaultRetention
	}
	return &Repository{
		capacity:  cfg.Capacity,
		retention: cfg.Retention,
		entries:   make([]value.OpsFeedEntry, 0, cfg.Capacity),
		reserved:  make(map[uuid.UUID]struct{}),
		metrics: value.OpsDiagnosticsSnapshot{
			FeedCapacity: cfg.Capacity,
		},
	}
}

// Ready reports whether the bounded feed is initialized.
func (r *Repository) Ready() bool {
	return r != nil && r.capacity > 0 && r.retention > 0 && r.reserved != nil
}

// Admit reserves a bounded feed slot before downstream dispatch.
func (r *Repository) Admit(_ context.Context, entry value.OpsFeedEntry) (value.OpsFeedReservation, error) {
	if !r.Ready() {
		return value.OpsFeedReservation{}, hookerrs.ErrDependencyUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	entry = r.normalizeEntry(entry)
	r.purgeExpiredLocked(entry.ObservedAt)
	if len(r.entries) >= r.capacity {
		r.metrics.Dropped++
		return value.OpsFeedReservation{}, hookerrs.ErrBackpressure
	}
	r.entries = append(r.entries, cloneOpsFeedEntry(entry))
	r.reserved[entry.EntryID] = struct{}{}
	r.metrics.FeedDepth = len(r.entries)
	return value.OpsFeedReservation{EntryID: entry.EntryID}, nil
}

// Complete updates a previously admitted feed slot with final route diagnostics.
func (r *Repository) Complete(_ context.Context, reservation value.OpsFeedReservation, entry value.OpsFeedEntry) error {
	if !r.Ready() {
		return hookerrs.ErrDependencyUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	entry = r.normalizeEntry(entry)
	entry.EntryID = reservation.EntryID
	for idx := range r.entries {
		if r.entries[idx].EntryID == reservation.EntryID {
			r.entries[idx] = cloneOpsFeedEntry(entry)
			delete(r.reserved, reservation.EntryID)
			r.applyMetricsLocked(entry)
			r.metrics.FeedDepth = len(r.entries)
			return nil
		}
	}
	return hookerrs.ErrBackpressure
}

// RecordDiagnostic records a rejected or dropped diagnostic without admitting downstream dispatch.
func (r *Repository) RecordDiagnostic(_ context.Context, entry value.OpsFeedEntry) error {
	if !r.Ready() {
		return hookerrs.ErrDependencyUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	entry = r.normalizeEntry(entry)
	r.purgeExpiredLocked(entry.ObservedAt)
	if len(r.entries) >= r.capacity {
		r.metrics.Dropped++
		if !r.dropOldestCompletedLocked() {
			r.metrics.FeedDepth = len(r.entries)
			return nil
		}
	}
	r.entries = append(r.entries, cloneOpsFeedEntry(entry))
	r.applyMetricsLocked(entry)
	r.metrics.FeedDepth = len(r.entries)
	return nil
}

// Recent returns the newest safe feed entries.
func (r *Repository) Recent(_ context.Context, limit int) []value.OpsFeedEntry {
	if !r.Ready() || limit <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.purgeExpiredLocked(time.Now().UTC())
	if limit > len(r.entries) {
		limit = len(r.entries)
	}
	result := make([]value.OpsFeedEntry, 0, limit)
	for idx := len(r.entries) - 1; idx >= 0 && len(result) < limit; idx-- {
		result = append(result, cloneOpsFeedEntry(r.entries[idx]))
	}
	r.metrics.FeedDepth = len(r.entries)
	return result
}

// Snapshot returns safe counters for operator diagnostics.
func (r *Repository) Snapshot(_ context.Context) value.OpsDiagnosticsSnapshot {
	if !r.Ready() {
		return value.OpsDiagnosticsSnapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.purgeExpiredLocked(time.Now().UTC())
	r.metrics.FeedDepth = len(r.entries)
	return cloneSnapshot(r.metrics)
}

func (r *Repository) normalizeEntry(entry value.OpsFeedEntry) value.OpsFeedEntry {
	if entry.EntryID == uuid.Nil {
		entry.EntryID = uuid.New()
	}
	if entry.ObservedAt.IsZero() {
		entry.ObservedAt = time.Now().UTC()
	}
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = entry.ObservedAt.Add(r.retention)
	}
	entry.RouteResults = cloneOpsRouteResults(entry.RouteResults)
	return entry
}

func (r *Repository) purgeExpiredLocked(now time.Time) {
	if len(r.entries) == 0 {
		return
	}
	kept := r.entries[:0]
	for _, entry := range r.entries {
		_, inFlight := r.reserved[entry.EntryID]
		if inFlight || entry.ExpiresAt.IsZero() || entry.ExpiresAt.After(now) {
			kept = append(kept, entry)
		}
	}
	for idx := len(kept); idx < len(r.entries); idx++ {
		r.entries[idx] = value.OpsFeedEntry{}
	}
	r.entries = kept
	r.metrics.FeedDepth = len(r.entries)
}

func (r *Repository) dropOldestCompletedLocked() bool {
	for idx, entry := range r.entries {
		if _, inFlight := r.reserved[entry.EntryID]; inFlight {
			continue
		}
		copy(r.entries[idx:], r.entries[idx+1:])
		last := len(r.entries) - 1
		r.entries[last] = value.OpsFeedEntry{}
		r.entries = r.entries[:last]
		return true
	}
	return false
}

func (r *Repository) applyMetricsLocked(entry value.OpsFeedEntry) {
	switch entry.Status {
	case hookenum.OpsFeedStatusAccepted:
		r.metrics.Accepted++
	case hookenum.OpsFeedStatusRejected:
		r.metrics.Rejected++
	case hookenum.OpsFeedStatusDropped:
		r.metrics.Dropped++
	}
	if entry.SanitizerResult == hookenum.SanitizerResultRedacted || entry.RedactionCount > 0 {
		r.metrics.Redacted++
	}
	for _, route := range entry.RouteResults {
		switch {
		case route.DiagnosticCode == value.RouteDiagnosticDownstreamFailed:
			r.metrics.DownstreamFailed++
		case route.RouteResult == hookenum.RouteDeliveryStatusDisabled:
			r.metrics.Disabled++
		case route.RouteResult == hookenum.RouteDeliveryStatusUnsupported:
			r.metrics.Unsupported++
		}
	}
	r.metrics.PayloadBuckets = incrementBucket(r.metrics.PayloadBuckets, entry.PayloadSizeBucket)
	r.metrics.LatencyBuckets = incrementBucket(r.metrics.LatencyBuckets, entry.LatencyBucket)
}

func incrementBucket(buckets []value.OpsBucketCount, bucket string) []value.OpsBucketCount {
	if bucket == "" {
		return buckets
	}
	for idx := range buckets {
		if buckets[idx].Bucket == bucket {
			buckets[idx].Count++
			return buckets
		}
	}
	return append(buckets, value.OpsBucketCount{Bucket: bucket, Count: 1})
}

func cloneOpsFeedEntry(entry value.OpsFeedEntry) value.OpsFeedEntry {
	entry.RouteResults = cloneOpsRouteResults(entry.RouteResults)
	return entry
}

func cloneOpsRouteResults(results []value.OpsRouteResult) []value.OpsRouteResult {
	if len(results) == 0 {
		return nil
	}
	return append([]value.OpsRouteResult(nil), results...)
}

func cloneSnapshot(snapshot value.OpsDiagnosticsSnapshot) value.OpsDiagnosticsSnapshot {
	snapshot.PayloadBuckets = append([]value.OpsBucketCount(nil), snapshot.PayloadBuckets...)
	snapshot.LatencyBuckets = append([]value.OpsBucketCount(nil), snapshot.LatencyBuckets...)
	return snapshot
}
