package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

//go:embed sql/freshness__status.sql
var freshnessStatusSQL string

//go:embed sql/freshness__activate.sql
var freshnessActivateSQL string

type freshnessOptions struct{ activationID string }
type freshnessPolicyStatus struct {
	ObservedAt        time.Time       `json:"observedAt"`
	Consumers         json.RawMessage `json:"consumers"`
	Version           int64           `json:"version"`
	MaximumAgeSeconds int             `json:"maximumAgeSeconds"`
	ActivationID      *string         `json:"activationId"`
	ActivatedAt       *time.Time      `json:"activatedAt"`
}

func parseFreshnessOptions(action command, args []string) (freshnessOptions, error) {
	if action != commandFreshnessActivate {
		return freshnessOptions{}, nil
	}
	if len(args) != 7 || args[1] != "--expected-version" || args[2] != "1" || args[3] != "--activation-id" || args[5] != "--confirm" || args[6] != "ACTIVATE-STAGING-AUTHORITY-FRESHNESS" {
		return freshnessOptions{}, errors.New("invalid freshness activation arguments")
	}
	id, err := uuid.Parse(args[4])
	if err != nil || id == uuid.Nil || id.String() != args[4] {
		return freshnessOptions{}, errors.New("invalid freshness activation identity")
	}
	return freshnessOptions{activationID: args[4]}, nil
}

func readFreshnessStatus(ctx context.Context, db *sql.DB) (freshnessPolicyStatus, error) {
	var result freshnessPolicyStatus
	if err := db.QueryRowContext(ctx, freshnessStatusSQL).Scan(&result.Version, &result.MaximumAgeSeconds, &result.ActivationID, &result.ActivatedAt, &result.ObservedAt, &result.Consumers); err != nil {
		return result, errors.New("freshness policy readback failed")
	}
	if (result.Version == 1 && (result.MaximumAgeSeconds != 300 || result.ActivationID != nil || result.ActivatedAt != nil)) ||
		(result.Version == 2 && (result.MaximumAgeSeconds != 30 || result.ActivationID == nil || result.ActivatedAt == nil)) ||
		(result.Version != 1 && result.Version != 2) {
		return result, errors.New("freshness policy readback rejected")
	}
	return result, nil
}

func runFreshness(ctx context.Context, db *sql.DB, action command, options freshnessOptions, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if action == commandFreshnessActivate {
		var version int64
		if err := db.QueryRowContext(ctx, freshnessActivateSQL, int64(1), options.activationID).Scan(&version); err != nil {
			// Потерянный ответ не разрешает новое намерение: caller читает status.
			return errors.New("freshness activation outcome uncertain; read freshness-status before any retry")
		}
		if version != 2 {
			return errors.New("freshness activation result rejected")
		}
	}
	if action == commandFreshnessWatch {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for i := 0; i < 45; i++ {
			state, err := readFreshnessStatus(ctx, db)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(output).Encode(state); err != nil {
				return errors.New("write freshness observation")
			}
			if i == 44 {
				return nil
			}
			select {
			case <-ctx.Done():
				return errors.New("freshness observation timeout")
			case <-ticker.C:
			}
		}
	}
	state, err := readFreshnessStatus(ctx, db)
	if err != nil {
		return err
	}
	if action == commandFreshnessActivate && (state.Version != 2 || state.ActivationID == nil || *state.ActivationID != options.activationID) {
		return errors.New("freshness activation readback mismatch")
	}
	if err := json.NewEncoder(output).Encode(state); err != nil {
		return errors.New("write freshness policy readback")
	}
	return nil
}
