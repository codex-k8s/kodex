package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_schedule_capture.sql
var queryPromptScheduleCapture string

type schedulePromptTemplate struct {
	Ref            string          `json:"ref"`
	Digest         string          `json:"digest"`
	Content        string          `json:"content"`
	Source         string          `json:"source"`
	SourceRevision string          `json:"sourceRevision"`
	Scope          json.RawMessage `json:"scope"`
}

type schedulePromptCapture struct {
	Revision int                     `json:"revision"`
	Values   map[string]any          `json:"values"`
	Template *schedulePromptTemplate `json:"template"`
}

func (repository *Repository) captureSchedulePromptTx(ctx context.Context, tx pgx.Tx, current scope, scheduleRef string, values []byte) ([]byte, error) {
	var captured string
	if err := tx.QueryRow(ctx, queryPromptScheduleCapture, pgx.StrictNamedArgs{"organization_id": current.organizationID, "schedule_ref": scheduleRef, "values": values}).Scan(&captured); err != nil {
		return nil, errs.ErrUnavailable
	}
	if len(captured) > 262144 {
		return nil, errs.ErrInvalid
	}
	if _, err := decodeSchedulePromptCapture(1, []byte(captured)); err != nil {
		return nil, err
	}
	return []byte(captured), nil
}

func schedulePromptDigest(format int, raw []byte) string {
	if format != 0 {
		raw = append([]byte(fmt.Sprintf("schedule-prompt:%d\n", format)), raw...)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func decodeSchedulePromptCapture(format int, raw []byte) (schedulePromptCapture, error) {
	var value schedulePromptCapture
	if format == 0 {
		if json.Unmarshal(raw, &value.Values) != nil || value.Values == nil {
			return value, errs.ErrConflict
		}
		return value, nil
	}
	if format != 1 || json.Unmarshal(raw, &value) != nil || value.Revision != 1 || value.Values == nil {
		return value, errs.ErrConflict
	}
	if value.Template != nil {
		digest := sha256.Sum256([]byte(value.Template.Content))
		if value.Template.Ref == "" || len(value.Template.Ref) > 128 || value.Template.Digest != hex.EncodeToString(digest[:]) {
			return value, errs.ErrConflict
		}
	}
	return value, nil
}
