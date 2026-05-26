// Package ops contains safe bounded ops feed repository contracts.
package ops

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// Repository stores only safe operator diagnostics and never stores raw hook payload.
type Repository interface {
	Ready() bool
	Admit(ctx context.Context, entry value.OpsFeedEntry) (value.OpsFeedReservation, error)
	Complete(ctx context.Context, reservation value.OpsFeedReservation, entry value.OpsFeedEntry) error
	RecordDiagnostic(ctx context.Context, entry value.OpsFeedEntry) error
	Recent(ctx context.Context, limit int) []value.OpsFeedEntry
	Snapshot(ctx context.Context) value.OpsDiagnosticsSnapshot
}
