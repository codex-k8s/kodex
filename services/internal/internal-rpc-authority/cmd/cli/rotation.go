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
	operationID  string
	revision     int64
	sourceDigest string
}

type rotationStatus struct {
	ObservedAt                 time.Time  `json:"observedAt"`
	OperationID                string     `json:"operationId,omitempty"`
	OperationStatus            string     `json:"operationStatus,omitempty"`
	Phase                      string     `json:"phase,omitempty"`
	IntentID                   string     `json:"intentId,omitempty"`
	ProtocolVersion            int        `json:"protocolVersion,omitempty"`
	SourceRevision             int64      `json:"sourceRevision,omitempty"`
	SourceDigestSHA256         string     `json:"sourceDigestSHA256,omitempty"`
	RegistrySourceDigestSHA256 string     `json:"registrySourceDigestSHA256,omitempty"`
	RegistryRevision           *int64     `json:"registryRevision,omitempty"`
	BaseRevision               *int64     `json:"baseRevision,omitempty"`
	BaseDigestSHA256           string     `json:"baseDigestSHA256,omitempty"`
	SwitchNotBefore            *time.Time `json:"switchNotBefore,omitempty"`
	PreviousNotAfter           *time.Time `json:"previousNotAfter,omitempty"`
	CompletedAt                *time.Time `json:"completedAt,omitempty"`
	PredecessorRevision        *int64     `json:"predecessorRevision,omitempty"`
	PredecessorDigestSHA256    string     `json:"predecessorDigestSHA256,omitempty"`
	DeliveryStartedAt          *time.Time `json:"deliveryStartedAt,omitempty"`
	DeliveredAt                *time.Time `json:"deliveredAt,omitempty"`
	PromotedAt                 *time.Time `json:"promotedAt,omitempty"`
	OverlapUntil               *time.Time `json:"overlapUntil,omitempty"`
	RetiredAt                  *time.Time `json:"retiredAt,omitempty"`
	AbortedAt                  *time.Time `json:"abortedAt,omitempty"`
	Status                     string     `json:"status"`
	ExpectedReadbacks          *int       `json:"expectedReadbackCount,omitempty"`
	ActualReadbacks            *int       `json:"actualReadbackCount,omitempty"`
}

func parseRotationOptions(action command, args []string) (rotationOptions, error) {
	if action == commandRotationWatch {
		if len(args) != 3 || args[1] != "--operation-id" {
			return rotationOptions{}, errors.New("invalid authority rotation watch arguments")
		}
		id, err := uuid.Parse(args[2])
		if err != nil || id == uuid.Nil || id.String() != args[2] {
			return rotationOptions{}, errors.New("invalid authority rotation operation identity")
		}
		return rotationOptions{operationID: args[2]}, nil
	}
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
	decodeErr := json.Unmarshal(raw, &result)
	allowed := map[string]bool{
		"EMPTY": true, "PREPARED": true, "DELIVERING": true,
		"DELIVERED": true, "PROMOTED": true, "RETIRED": true, "ABORTED": true,
		"DISTRIBUTING": true, "WAITING_SWITCH": true, "SWITCHING": true,
		"WAITING_RETIRE": true, "RETIRING": true,
	}
	operationAllowed := map[string]bool{
		"DISTRIBUTING": true, "WAITING_SWITCH": true, "SWITCHING": true,
		"WAITING_RETIRE": true, "RETIRING": true, "RETIRED": true,
	}
	operationID, operationIDErr := uuid.Parse(result.OperationID)
	if decodeErr != nil || result.ObservedAt.IsZero() || !allowed[result.Status] ||
		(result.Status == "EMPTY" && result.IntentID != "") ||
		(result.OperationID != "" && (operationIDErr != nil || operationID == uuid.Nil ||
			operationID.String() != result.OperationID || !operationAllowed[result.OperationStatus] ||
			result.OperationStatus != result.Status ||
			result.RegistryRevision == nil || result.BaseRevision == nil ||
			*result.RegistryRevision < 1 || *result.RegistryRevision > 9007199254740991 ||
			*result.BaseRevision < 1 || *result.BaseRevision > 9007199254740988 ||
			*result.RegistryRevision != *result.BaseRevision+1 ||
			result.ExpectedReadbacks == nil || *result.ExpectedReadbacks < 1 || *result.ExpectedReadbacks > 384 ||
			!rotationDigestPattern.MatchString(result.RegistrySourceDigestSHA256) ||
			!rotationDigestPattern.MatchString(result.BaseDigestSHA256) ||
			(result.Phase != "" && result.Phase != "DISTRIBUTE" && result.Phase != "SWITCH" && result.Phase != "RETIRE") ||
			((result.OperationStatus == "WAITING_SWITCH" || result.OperationStatus == "SWITCHING" ||
				result.OperationStatus == "WAITING_RETIRE" || result.OperationStatus == "RETIRING" ||
				result.OperationStatus == "RETIRED") && result.SwitchNotBefore == nil) ||
			((result.OperationStatus == "SWITCHING" || result.OperationStatus == "WAITING_RETIRE" ||
				result.OperationStatus == "RETIRING" || result.OperationStatus == "RETIRED") && result.PreviousNotAfter == nil) ||
			(result.OperationStatus == "RETIRED" && result.CompletedAt == nil))) ||
		(result.Status != "EMPTY" && result.OperationID == "" && (result.IntentID == "" ||
			(result.ProtocolVersion != 1 && result.ProtocolVersion != 2) ||
			result.SourceRevision < 1 || !rotationDigestPattern.MatchString(result.SourceDigestSHA256))) {
		return rotationStatus{}, errors.New("authority rotation readback rejected")
	}
	return result, nil
}

func runRotation(ctx context.Context, db *sql.DB, action command, options rotationOptions, output io.Writer) error {
	timeout := 60 * time.Second
	if action == commandRotationWatch {
		timeout = 280 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
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
	if action == commandRotationWatch {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			state, err := readRotationStatus(ctx, db)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(output).Encode(state); err != nil {
				return errors.New("write authority rotation observation")
			}
			if rotationWatchComplete(state, options.operationID) {
				return nil
			}
			select {
			case <-ctx.Done():
				return errors.New("authority rotation observation timeout")
			case <-ticker.C:
			}
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

func rotationWatchComplete(state rotationStatus, operationID string) bool {
	return state.OperationID == operationID && state.Status == "RETIRED"
}
