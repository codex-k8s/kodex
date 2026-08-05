// Package scheduler реализует bounded reconciliation без владения domain state.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	client "github.com/codex-k8s/matter-codex/services/jobs/automation-scheduler/internal/clients/controlplane"
	internalobservability "github.com/codex-k8s/matter-codex/services/jobs/automation-scheduler/internal/observability"
	"github.com/google/uuid"
)

type ControlPlane interface {
	MaterializeDue(context.Context, string, int) (int, error)
	ClaimNext(context.Context, string) (client.Claim, error)
	Complete(context.Context, client.Claim, string) (string, error)
}

type Config struct {
	DueLimit             int
	ClaimLimit           int
	MaximumTrackedClaims int
}

type Scheduler struct {
	controlPlane ControlPlane
	metrics      *internalobservability.Metrics
	config       Config
	claims       map[string]client.Claim
	dueKey       string
	claimKey     string
}

func New(controlPlane ControlPlane, metrics *internalobservability.Metrics, config Config) (*Scheduler, error) {
	if controlPlane == nil || metrics == nil || config.DueLimit < 1 || config.DueLimit > 100 ||
		config.ClaimLimit < 1 || config.ClaimLimit > 100 ||
		config.MaximumTrackedClaims < config.ClaimLimit || config.MaximumTrackedClaims > 4096 {
		return nil, errors.New("automation scheduler configuration is invalid")
	}
	return &Scheduler{
		controlPlane: controlPlane, metrics: metrics, config: config,
		claims: make(map[string]client.Claim, config.MaximumTrackedClaims),
	}, nil
}

func (scheduler *Scheduler) Cycle(ctx context.Context) error {
	var joined error
	if scheduler.dueKey == "" {
		scheduler.dueKey = uuid.NewString()
	}
	due, err := scheduler.controlPlane.MaterializeDue(
		ctx,
		scheduler.dueKey,
		scheduler.config.DueLimit,
	)
	if err != nil {
		joined = errors.Join(joined, fmt.Errorf("materialize due schedules: %w", err))
		scheduler.metrics.ObserveOccurrence("materialize", "error")
	} else if due == 0 {
		scheduler.dueKey = ""
		scheduler.metrics.ObserveOccurrence("materialize", "empty")
	} else {
		scheduler.dueKey = ""
		for range due {
			scheduler.metrics.ObserveOccurrence("materialize", "created")
		}
	}

	joined = errors.Join(joined, scheduler.reconcile(ctx))
	if len(scheduler.claims) < scheduler.config.MaximumTrackedClaims {
		joined = errors.Join(joined, scheduler.claim(ctx))
	}
	scheduler.metrics.SetTrackedClaims(len(scheduler.claims))
	switch {
	case joined == nil:
		scheduler.metrics.ObserveCycle("success")
	case len(scheduler.claims) > 0:
		scheduler.metrics.ObserveCycle("partial")
	default:
		scheduler.metrics.ObserveCycle("error")
	}
	return joined
}

func (scheduler *Scheduler) reconcile(ctx context.Context) error {
	var joined error
	now := time.Now().UTC()
	for occurrenceID, claim := range scheduler.claims {
		state, err := scheduler.controlPlane.Complete(ctx, claim, completionKey(claim))
		switch {
		case err == nil:
			delete(scheduler.claims, occurrenceID)
			scheduler.metrics.ObserveOccurrence("complete", "terminal")
			_ = state
		case !now.Before(claim.LeaseExpiresAt):
			// Локальные часы только освобождают transient tracking после server-issued
			// deadline. Terminal/retry выполняет owner-side PostgreSQL watchdog.
			delete(scheduler.claims, occurrenceID)
			scheduler.metrics.ObserveOccurrence("watchdog", "expired")
		case errors.Is(err, client.ErrPending) && now.Before(claim.LeaseExpiresAt):
			scheduler.metrics.ObserveOccurrence("complete", "pending")
		default:
			scheduler.metrics.ObserveOccurrence("complete", "error")
			joined = errors.Join(joined, fmt.Errorf("reconcile schedule occurrence: %w", err))
		}
	}
	return joined
}

func (scheduler *Scheduler) claim(ctx context.Context) error {
	var joined error
	for claimed := 0; claimed < scheduler.config.ClaimLimit &&
		len(scheduler.claims) < scheduler.config.MaximumTrackedClaims; claimed++ {
		if scheduler.claimKey == "" {
			scheduler.claimKey = uuid.NewString()
		}
		occurrence, err := scheduler.controlPlane.ClaimNext(ctx, scheduler.claimKey)
		if errors.Is(err, client.ErrNoWork) {
			scheduler.claimKey = ""
			scheduler.metrics.ObserveOccurrence("claim", "empty")
			break
		}
		if errors.Is(err, client.ErrClaimRetired) {
			// Только authoritative RETIRED доказывает, что прежний semantic key
			// больше не может rejoin-ить live reservation/materialization.
			scheduler.claimKey = ""
			scheduler.metrics.ObserveOccurrence("claim", "retired")
			break
		}
		if err != nil {
			scheduler.metrics.ObserveOccurrence("claim", "error")
			joined = errors.Join(joined, fmt.Errorf("claim schedule occurrence: %w", err))
			break
		}
		scheduler.claimKey = ""
		if _, duplicate := scheduler.claims[occurrence.OccurrenceID]; duplicate {
			scheduler.metrics.ObserveOccurrence("claim", "error")
			joined = errors.Join(joined, errors.New("duplicate schedule occurrence claim"))
			continue
		}
		scheduler.claims[occurrence.OccurrenceID] = occurrence
		scheduler.metrics.ObserveOccurrence("claim", "claimed")
	}
	return joined
}

func completionKey(claim client.Claim) string {
	return uuid.NewSHA1(
		uuid.NameSpaceOID,
		[]byte(fmt.Sprintf("automation-scheduler-complete\x00%s\x00%d", claim.OccurrenceID, claim.Attempt)),
	).String()
}
