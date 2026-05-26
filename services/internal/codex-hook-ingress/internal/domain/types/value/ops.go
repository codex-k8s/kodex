package value

import (
	"time"

	"github.com/google/uuid"

	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
)

const (
	OpsDiagnosticAccepted         = "hook.accepted"
	OpsDiagnosticRejected         = "hook.rejected"
	OpsDiagnosticRedacted         = "hook.redacted"
	OpsDiagnosticDropped          = "hook.dropped"
	OpsDiagnosticBackpressure     = "hook.backpressure"
	OpsDiagnosticRateLimited      = "hook.rate_limited"
	OpsDiagnosticDownstreamFailed = "route.downstream_failed"
	OpsDiagnosticDisabled         = "route.disabled"
	OpsDiagnosticUnsupported      = "route.unsupported"
)

// OpsFeedEntry is the bounded realtime/operator event kept by codex-hook-ingress.
type OpsFeedEntry struct {
	EntryID           uuid.UUID
	EventID           uuid.UUID
	HookEventName     hookenum.HookEventName
	EventKind         string
	Status            hookenum.OpsFeedStatus
	RejectReason      string
	SafeSummary       string
	PayloadDigest     string
	PayloadSizeBucket string
	LatencyBucket     string
	CorrelationID     string
	RetentionClass    hookenum.RetentionClass
	SanitizerResult   hookenum.SanitizerResult
	RedactionCount    int
	RouteResults      []OpsRouteResult
	ObservedAt        time.Time
	ExpiresAt         time.Time
}

// OpsRouteResult is one safe owner-target diagnostic inside an ops feed entry.
type OpsRouteResult struct {
	OwnerTarget     hookenum.DownstreamOwner
	DeliveryMode    hookenum.DeliveryMode
	RouteResult     hookenum.RouteDeliveryStatus
	DiagnosticCode  string
	LatencyBucket   string
	Retryable       bool
	SafePartsDigest string
}

// OpsFeedReservation reserves bounded feed capacity before downstream dispatch.
type OpsFeedReservation struct {
	EntryID uuid.UUID
}

// OpsDiagnosticsSnapshot contains safe counters for operator diagnostics.
type OpsDiagnosticsSnapshot struct {
	FeedDepth        int
	FeedCapacity     int
	Accepted         int64
	Rejected         int64
	Redacted         int64
	Dropped          int64
	DownstreamFailed int64
	Disabled         int64
	Unsupported      int64
	PayloadBuckets   []OpsBucketCount
	LatencyBuckets   []OpsBucketCount
}

// OpsBucketCount contains a count for one bounded metric bucket.
type OpsBucketCount struct {
	Bucket string
	Count  int64
}
