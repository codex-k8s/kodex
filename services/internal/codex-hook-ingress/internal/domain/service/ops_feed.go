package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// OpsDiagnosticsSnapshot returns safe in-memory diagnostics for tests and future operator reads.
func (s *Service) OpsDiagnosticsSnapshot(ctx context.Context) value.OpsDiagnosticsSnapshot {
	if s == nil || s.opsFeed == nil {
		return value.OpsDiagnosticsSnapshot{}
	}
	return s.opsFeed.Snapshot(ctx)
}

// RecentOpsFeed returns newest safe in-memory feed entries without exposing a physical transport.
func (s *Service) RecentOpsFeed(ctx context.Context, limit int) []value.OpsFeedEntry {
	if s == nil || s.opsFeed == nil {
		return nil
	}
	return s.opsFeed.Recent(ctx, limit)
}

func (s *Service) admitOpsFeed(
	ctx context.Context,
	envelope value.HookEnvelope,
	decision SanitizerDecision,
	startedAt time.Time,
) (value.OpsFeedReservation, error) {
	entry := s.buildOpsFeedEntry(envelope, decision.EnvelopeBytes, nil, hookenum.OpsFeedStatusAccepted, "", startedAt)
	reservation, err := s.opsFeed.Admit(ctx, entry)
	if err != nil {
		if errors.Is(err, hookerrs.ErrBackpressure) {
			return value.OpsFeedReservation{}, hookerrs.ErrBackpressure
		}
		return value.OpsFeedReservation{}, fmtDependency("admit hook ops feed", err)
	}
	return reservation, nil
}

func (s *Service) completeOpsFeed(
	ctx context.Context,
	reservation value.OpsFeedReservation,
	envelope value.HookEnvelope,
	decision SanitizerDecision,
	diagnostics []value.RouteDeliveryResult,
	startedAt time.Time,
) error {
	entry := s.buildOpsFeedEntry(envelope, decision.EnvelopeBytes, diagnostics, hookenum.OpsFeedStatusAccepted, "", startedAt)
	if err := s.opsFeed.Complete(ctx, reservation, entry); err != nil {
		return fmtDependency("complete hook ops feed", err)
	}
	return nil
}

func (s *Service) recordRejectedOpsDiagnostic(
	ctx context.Context,
	envelope value.HookEnvelope,
	envelopeBytes int,
	startedAt time.Time,
	err error,
) {
	s.recordOpsDiagnostic(ctx, envelope, envelopeBytes, startedAt, hookenum.OpsFeedStatusRejected, err)
}

func (s *Service) recordDroppedOpsDiagnostic(
	ctx context.Context,
	envelope value.HookEnvelope,
	envelopeBytes int,
	startedAt time.Time,
	err error,
) {
	s.recordOpsDiagnostic(ctx, envelope, envelopeBytes, startedAt, hookenum.OpsFeedStatusDropped, err)
}

func (s *Service) recordOpsDiagnostic(
	ctx context.Context,
	envelope value.HookEnvelope,
	envelopeBytes int,
	startedAt time.Time,
	status hookenum.OpsFeedStatus,
	err error,
) {
	entry := s.buildOpsFeedEntry(envelope, envelopeBytes, nil, status, errorReasonCode(err), startedAt)
	if status == hookenum.OpsFeedStatusRejected {
		entry.SafeSummary = ""
	}
	_ = s.opsFeed.RecordDiagnostic(ctx, entry)
}

func (s *Service) buildOpsFeedEntry(
	envelope value.HookEnvelope,
	envelopeBytes int,
	diagnostics []value.RouteDeliveryResult,
	status hookenum.OpsFeedStatus,
	rejectReason string,
	startedAt time.Time,
) value.OpsFeedEntry {
	now := s.clock.Now()
	if startedAt.IsZero() {
		startedAt = now
	}
	return value.OpsFeedEntry{
		EntryID:           uuid.New(),
		EventID:           envelope.EventID,
		HookEventName:     envelope.HookEventName,
		EventKind:         string(envelope.HookEventName),
		Status:            status,
		RejectReason:      rejectReason,
		SafeSummary:       envelope.SafePayload.SafeSummary,
		PayloadDigest:     envelope.PayloadDigest,
		PayloadSizeBucket: payloadSizeBucket(envelopeBytes),
		LatencyBucket:     latencyBucket(now.Sub(startedAt)),
		CorrelationID:     envelope.CorrelationID,
		RetentionClass:    envelope.RetentionClass,
		SanitizerResult:   envelope.SanitizerReport.Result,
		RedactionCount:    envelope.SanitizerReport.RedactionCount,
		RouteResults:      opsRouteResults(diagnostics, now.Sub(startedAt)),
		ObservedAt:        now,
		ExpiresAt:         now.Add(s.config.OpsFeedRetention),
	}
}

func opsRouteResults(diagnostics []value.RouteDeliveryResult, latency time.Duration) []value.OpsRouteResult {
	if len(diagnostics) == 0 {
		return nil
	}
	results := make([]value.OpsRouteResult, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		results = append(results, value.OpsRouteResult{
			OwnerTarget:     diagnostic.Owner,
			DeliveryMode:    diagnostic.DeliveryMode,
			RouteResult:     diagnostic.Status,
			DiagnosticCode:  diagnostic.DiagnosticCode,
			LatencyBucket:   latencyBucket(latency),
			Retryable:       diagnostic.Retryable,
			SafePartsDigest: safePartsDigest(diagnostic.SafeParts),
		})
	}
	return results
}

func estimatedEnvelopeBytes(envelope value.HookEnvelope) int {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return 0
	}
	return len(encoded)
}

func payloadSizeBucket(size int) string {
	switch {
	case size <= 0:
		return "unknown"
	case size <= 1024:
		return "le_1KiB"
	case size <= 4096:
		return "le_4KiB"
	case size <= 16384:
		return "le_16KiB"
	case size <= 65536:
		return "le_64KiB"
	default:
		return "gt_64KiB"
	}
}

func latencyBucket(latency time.Duration) string {
	switch {
	case latency <= 10*time.Millisecond:
		return "le_10ms"
	case latency <= 50*time.Millisecond:
		return "le_50ms"
	case latency <= 250*time.Millisecond:
		return "le_250ms"
	case latency <= time.Second:
		return "le_1s"
	default:
		return "gt_1s"
	}
}

func errorReasonCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, hookerrs.ErrBackpressure):
		return string(hookerrs.ErrBackpressure)
	case errors.Is(err, hookerrs.ErrRateLimited):
		return string(hookerrs.ErrRateLimited)
	case errors.Is(err, hookerrs.ErrInvalidBinding):
		return string(hookerrs.ErrInvalidBinding)
	case errors.Is(err, hookerrs.ErrPayloadTooLarge):
		return string(hookerrs.ErrPayloadTooLarge)
	case errors.Is(err, hookerrs.ErrPayloadRejected):
		return string(hookerrs.ErrPayloadRejected)
	case errors.Is(err, hookerrs.ErrUnsupportedEvent):
		return string(hookerrs.ErrUnsupportedEvent)
	case errors.Is(err, hookerrs.ErrDuplicateConflict):
		return string(hookerrs.ErrDuplicateConflict)
	case errors.Is(err, hookerrs.ErrInvalidArgument):
		return string(hookerrs.ErrInvalidArgument)
	default:
		return string(hookerrs.ErrDependencyUnavailable)
	}
}

func safePartsDigest(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func fmtDependency(action string, err error) error {
	return fmt.Errorf("%w: %s: %v", hookerrs.ErrDependencyUnavailable, action, err)
}
