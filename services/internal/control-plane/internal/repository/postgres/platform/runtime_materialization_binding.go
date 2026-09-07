package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"math"

	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

const (
	runtimeMaterializationOperation   = "platform.runtime.credentials.materialize"
	assistantMaterializationOperation = "platform.runtime.credentials.system-assistant.materialize"
)

//go:embed sql/runtime_materialization_register.sql
var runtimeMaterializationRegisterSQL string

type runtimeMaterializationInput struct {
	WorkloadInstance      string
	LeaseRef              string
	Fence                 string
	Generation            int64
	RuntimeRevisionRef    string
	RuntimeRevisionDigest string
	SessionRef            string
	TurnRef               string
	Attempt               int64
	InputDigest           string
	SystemAssistant       bool
}

// runtimeMaterializationDigest повторяет protobuf envelope рабочего RPC.
// Значения поступают из owner claim, а не из запроса выдачи полномочий.
func runtimeMaterializationDigest(input runtimeMaterializationInput) (string, string, error) {
	if input.WorkloadInstance == "" || input.LeaseRef == "" || input.Fence == "" ||
		input.Generation <= 0 || input.RuntimeRevisionRef == "" || input.RuntimeRevisionDigest == "" ||
		input.SessionRef == "" || input.Attempt <= 0 || input.Attempt > math.MaxInt32 || input.InputDigest == "" {
		return "", "", errs.ErrConflict
	}
	execution := &secretbrokerv1.MaterializeRuntimeCredentialsRequest{
		WorkloadInstance: input.WorkloadInstance, LeaseRef: input.LeaseRef,
		Fence: input.Fence, Generation: input.Generation,
		RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef,
		Attempt: int32(input.Attempt), InputDigest: input.InputDigest,
	}
	operation := runtimeMaterializationOperation
	var request proto.Message = execution
	if input.SystemAssistant {
		operation = assistantMaterializationOperation
		request = &secretbrokerv1.MaterializeSystemAssistantCredentialsRequest{Execution: execution}
	}
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	if err != nil {
		return "", "", errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	return operation, hex.EncodeToString(digest[:]), nil
}

func registerRuntimeMaterializationTx(ctx context.Context, tx pgx.Tx, organizationID string, input runtimeMaterializationInput) error {
	operation, digest, err := runtimeMaterializationDigest(input)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, runtimeMaterializationRegisterSQL, pgx.StrictNamedArgs{
		"organization_id": organizationID, "lease_ref": input.LeaseRef,
		"operation": operation, "request_digest": digest,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
