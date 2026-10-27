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
	// runtimeMaterializationOperation identifies the normal execution credential-materialization RPC
	// operation included in the owner lease binding. It is distinct from the system-assistant envelope
	// and does not itself grant materialization.
	// runtimeMaterializationOperation задаёт обычную execution credential-materialization RPC
	// operation, включаемую в привязку owner lease. Отличается от оболочки системного ассистента и
	// само не разрешает материализацию.
	runtimeMaterializationOperation = "platform.runtime.credentials.materialize"
	// assistantMaterializationOperation identifies system-assistant credential materialization with
	// its wrapper request envelope. The separate operation prevents reusing a normal execution digest
	// as assistant authority.
	// assistantMaterializationOperation задаёт материализацию credentials системного ассистента с
	// обёрнутым request envelope. Отдельная operation не позволяет использовать обычный execution
	// digest как полномочия ассистента.
	assistantMaterializationOperation = "platform.runtime.credentials.system-assistant.materialize"
)

// runtimeMaterializationRegisterSQL embeds first-write operation/request-digest binding to an
// exact live CLAIMED tenant lease only while both stored fields are NULL. The caller requires
// exactly one affected row and owns the transaction.
// runtimeMaterializationRegisterSQL встраивает первичную запись operation/request digest в точную
// действующую CLAIMED аренду организации, только пока оба поля NULL. Вызывающий код требует ровно
// одну изменённую строку и владеет транзакцией.
//
//go:embed sql/runtime_materialization_register.sql
var runtimeMaterializationRegisterSQL string

// runtimeMaterializationInput holds owner-claim workload/lease/fence/generation and immutable
// runtime/session/turn/attempt/input pins plus the assistant-envelope selector. These values must
// come from trusted owner state, not an external authority request.
// runtimeMaterializationInput хранит workload/lease/fence/generation owner claim, неизменяемые
// runtime/session/turn/attempt/input pins и выбор оболочки ассистента. Значения должны поступать
// из доверенного состояния владельца, а не внешнего запроса полномочий.
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

// runtimeMaterializationDigest validates required claim/snapshot fields and a positive
// int32-bounded attempt, constructs the exact normal or assistant protobuf RPC envelope and hashes
// deterministic wire bytes. It matches the pinned RPC schema, not a universal canonical encoding
// across languages/builds, and does not register or authorize the request.
// runtimeMaterializationDigest проверяет обязательные поля claim/snapshot и положительную попытку
// в int32, собирает точный обычный либо assistant protobuf RPC envelope и хеширует deterministic
// wire bytes. Соответствует закреплённой RPC-схеме, а не универсальному каноническому кодированию
// между языками и сборками; не регистрирует и не авторизует запрос.
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

// registerRuntimeMaterializationTx computes the exact operation/envelope digest and conditionally
// binds it to the live owner lease in the supplied transaction. SQL failure is Unavailable and a
// non-single update Conflict; it neither issues proof nor calls the broker or commits.
// registerRuntimeMaterializationTx вычисляет точные operation и дайджест оболочки и условно
// связывает их с действующей owner lease в переданной транзакции. Ошибка SQL даёт Unavailable,
// иное число строк — Conflict; не выдаёт proof, не вызывает брокер и не выполняет Commit.
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
