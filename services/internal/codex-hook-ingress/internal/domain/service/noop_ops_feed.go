package service

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

type noopOpsFeed struct{}

func (noopOpsFeed) Ready() bool {
	return true
}

func (noopOpsFeed) Admit(_ context.Context, entry value.OpsFeedEntry) (value.OpsFeedReservation, error) {
	return value.OpsFeedReservation{EntryID: entry.EntryID}, nil
}

func (noopOpsFeed) Complete(_ context.Context, _ value.OpsFeedReservation, _ value.OpsFeedEntry) error {
	return nil
}

func (noopOpsFeed) RecordDiagnostic(_ context.Context, _ value.OpsFeedEntry) error {
	return nil
}

func (noopOpsFeed) Recent(_ context.Context, _ int) []value.OpsFeedEntry {
	return nil
}

func (noopOpsFeed) Snapshot(_ context.Context) value.OpsDiagnosticsSnapshot {
	return value.OpsDiagnosticsSnapshot{}
}
