package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
)

//go:embed sql/rotation__status.sql
var rotationStatusSQL string

//go:embed sql/rotation__abort.sql
var rotationAbortSQL string

var rotationDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type rotationOptions struct {
	intentID     string
	revision     int64
	sourceDigest string
}

type rotationStatus struct {
	ObservedAt         time.Time `json:"observedAt"`
	IntentID           string    `json:"intentId,omitempty"`
	ProtocolVersion    int       `json:"protocolVersion,omitempty"`
	SourceRevision     int64     `json:"sourceRevision,omitempty"`
	SourceDigestSHA256 string    `json:"sourceDigestSHA256,omitempty"`
	Status             string    `json:"status"`
	ExpectedReadbacks  *int      `json:"expectedReadbackCount,omitempty"`
	ActualReadbacks    *int      `json:"actualReadbackCount,omitempty"`
}

func parseRotationOptions(action command, args []string) (rotationOptions, error) {
	if action != commandRotationAbort {
		return rotationOptions{}, nil
	}
	if len(args) != 9 || args[1] != "--intent-id" || args[3] != "--source-revision" ||
		args[5] != "--source-digest-sha256" || args[7] != "--confirm" ||
		args[8] != "ABORT-STAGING-AUTHORITY-ROTATION" {
		return rotationOptions{}, errors.New("invalid authority rotation abort arguments")
	}
	id, idErr := uuid.Parse(args[2])
	revision, revisionErr := strconv.ParseInt(args[4], 10, 64)
	if idErr != nil || id == uuid.Nil || id.String() != args[2] ||
		revisionErr != nil || revision < 1 || revision > 9007199254740991 ||
		strconv.FormatInt(revision, 10) != args[4] || !rotationDigestPattern.MatchString(args[6]) {
		return rotationOptions{}, errors.New("invalid authority rotation abort identity")
	}
	return rotationOptions{intentID: args[2], revision: revision, sourceDigest: args[6]}, nil
}

func readRotationStatus(ctx context.Context, db *sql.DB) (rotationStatus, error) {
	var raw []byte
	if err := db.QueryRowContext(ctx, rotationStatusSQL).Scan(&raw); err != nil {
		return rotationStatus{}, errors.New("authority rotation readback failed")
	}
	var result rotationStatus
	allowed := map[string]bool{
		"EMPTY": true, "PREPARED": true, "DELIVERING": true,
		"DELIVERED": true, "PROMOTED": true, "RETIRED": true, "ABORTED": true,
	}
	if json.Unmarshal(raw, &result) != nil || result.ObservedAt.IsZero() || !allowed[result.Status] ||
		(result.Status == "EMPTY" && result.IntentID != "") ||
		(result.Status != "EMPTY" && (result.IntentID == "" ||
			(result.ProtocolVersion != 1 && result.ProtocolVersion != 2) ||
			result.SourceRevision < 1 || !rotationDigestPattern.MatchString(result.SourceDigestSHA256))) {
		return rotationStatus{}, errors.New("authority rotation readback rejected")
	}
	return result, nil
}

func runRotation(ctx context.Context, db *sql.DB, action command, options rotationOptions, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if action == commandRotationAbort {
		var accepted bool
		if err := db.QueryRowContext(ctx, rotationAbortSQL, options.intentID, options.revision, options.sourceDigest).Scan(&accepted); err != nil {
			return errors.New("authority rotation abort outcome uncertain; read rotation-status before any retry")
		}
		if !accepted {
			return errors.New("authority rotation abort rejected")
		}
	}
	state, err := readRotationStatus(ctx, db)
	if err != nil {
		return err
	}
	if action == commandRotationAbort && (state.IntentID != options.intentID || state.Status != "ABORTED") {
		return errors.New("authority rotation abort readback mismatch")
	}
	if err := json.NewEncoder(output).Encode(state); err != nil {
		return errors.New("write authority rotation readback")
	}
	return nil
}
