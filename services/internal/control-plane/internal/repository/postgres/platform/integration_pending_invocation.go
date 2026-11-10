package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// pendingInvocationReceiptOperation isolates tool-call aliases from command receipts.
// pendingInvocationReceiptOperation отделяет aliases вызовов инструментов от квитанций команд.
const pendingInvocationReceiptOperation = "controlplane.integration.pending-alias"

// queryPendingInvocationReceipt reads an exact alias even after expiry, so an expired
// binding is refused rather than mistaken for permission to allocate a new effect.
// queryPendingInvocationReceipt читает точный alias даже после истечения срока:
// просроченная привязка отклоняется и не разрешает выделить новое действие.
//
//go:embed sql/integration_pending_invocation_receipt.sql
var queryPendingInvocationReceipt string

// queryPendingInvocationCandidates bounds candidates to the locked native node and
// current intent pins; a saved alias may address terminal work, a new alias only OPEN work.
// queryPendingInvocationCandidates ограничивает кандидатов заблокированным native node
// и текущими pins; сохранённый alias читает terminal work, новый — только OPEN work.
//
//go:embed sql/integration_pending_invocation_candidates.sql
var queryPendingInvocationCandidates string

// queryPendingInvocationPersonalPin compares the current personal choice with the immutable pin.
// queryPendingInvocationPersonalPin сверяет текущий личный выбор с неизменяемой привязкой.
//
//go:embed sql/integration_pending_invocation_personal_pin.sql
var queryPendingInvocationPersonalPin string

// pendingInvocationRequest carries already admitted native coordinates and the complete
// intent frame. It is internal owner state, never authority supplied by a model.
// pendingInvocationRequest содержит уже допущенные native координаты и полный frame
// намерения. Это внутреннее состояние владельца, а не полномочия из ответа модели.
type pendingInvocationRequest struct {
	nodeID, connectionID, grantID, capability, inputDigest string
	definitionDigest, resourceScopeDigest, approvalPolicy  string
	grantVersion                                           int64
	idempotencyKey, intentDigest                           string
	connectionSnapshotDigest                               string
	intentParts                                            []string
	allowPending                                           bool
}

// pendingInvocationResolution identifies existing work without transferring credentials,
// changing its approval or claiming its result as a successful external effect.
// pendingInvocationResolution указывает существующую работу без передачи credentials,
// изменения согласования и объявления результата успешным внешним действием.
type pendingInvocationResolution struct {
	ref, state, gateRef string
}

// pendingInvocationReceipt stores owner coordinates and a configuration commitment only.
// It never stores configuration values, credentials or provider result content.
// pendingInvocationReceipt хранит только координаты владельца и хеш конфигурации,
// без значений конфигурации, credentials и содержимого результата провайдера.
type pendingInvocationReceipt struct {
	InvocationRef            string `json:"invocationRef"`
	ConnectionSnapshotDigest string `json:"connectionSnapshotDigest"`
}

// rejoinPendingInvocationTx binds another call key to one exact pending native intent.
// The caller holds the node/Connection/Grant locks in Serializable and has rechecked all
// normal authority. Existing receipts outlive the bounded physical turn; expired aliases
// fail closed. No provider, gate resolution, reservation or continuation is invoked here.
// rejoinPendingInvocationTx привязывает другой ключ вызова к одному точному pending intent.
// Caller держит node/Connection/Grant в Serializable и повторно проверил обычные права.
// Существующие receipts живут дольше ограниченного физического хода; просроченные aliases
// закрыто отклоняются. Provider, решение Gate, резерв и continuation здесь не запускаются.
func (repository *Repository) rejoinPendingInvocationTx(ctx context.Context, tx pgx.Tx, current scope, request pendingInvocationRequest) (*pendingInvocationResolution, error) {
	saved, savedDigest, err := loadPendingInvocationReceiptTx(ctx, tx, current, request.nodeID, request.idempotencyKey)
	if err != nil {
		return nil, err
	}
	aliasRef := ""
	if saved != nil {
		if savedDigest != request.intentDigest {
			return nil, errs.ErrIdempotencyReuse
		}
		aliasRef = saved.InvocationRef
	} else if !request.allowPending {
		return nil, nil
	}
	rows, err := tx.Query(ctx, queryPendingInvocationCandidates, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "node_id": request.nodeID,
		"connection_id": request.connectionID, "grant_id": request.grantID,
		"capability": request.capability, "input_digest": request.inputDigest,
		"definition_digest": request.definitionDigest, "resource_scope_digest": request.resourceScopeDigest,
		"grant_version": request.grantVersion, "approval_policy": request.approvalPolicy, "alias_ref": aliasRef,
	})
	if err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	var result *pendingInvocationResolution
	originalKey := ""
	count := 0
	for rows.Next() {
		var candidate pendingInvocationResolution
		var originalDigest string
		if err := rows.Scan(&candidate.ref, &candidate.state, &candidate.gateRef, &originalKey, &originalDigest); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		count++
		parts := append([]string(nil), request.intentParts...)
		parts[1] = originalKey
		digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
		if hex.EncodeToString(digest[:]) != originalDigest {
			rows.Close()
			return nil, errs.ErrConflict
		}
		result = &candidate
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, serializableTransactionError(rowErr, errs.ErrUnavailable)
	}
	if count > 1 || (aliasRef != "" && result == nil) {
		return nil, errs.ErrConflict
	}
	if result == nil || aliasRef != "" {
		return result, nil
	}
	origin, _, err := loadPendingInvocationReceiptTx(ctx, tx, current, request.nodeID, originalKey)
	if err != nil {
		return nil, err
	}
	if origin == nil && originalKey == request.idempotencyKey {
		// Exact historical replay keeps its old native path, without inventing a configuration pin.
		// Точный исторический replay сохраняет прежний native путь без выдуманного config pin.
		return nil, nil
	}
	if origin == nil || origin.InvocationRef != result.ref || origin.ConnectionSnapshotDigest != request.connectionSnapshotDigest {
		return nil, errs.ErrConflict
	}
	choice, err := repository.selectPersonalInvocationCredentialTx(ctx, tx, current.organizationID, result.ref)
	if err != nil {
		return nil, err
	}
	if choice.context.policy == entity.IntegrationCredentialInitiatorRequired {
		var matches bool
		if err := tx.QueryRow(ctx, queryPendingInvocationPersonalPin, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "invocation_ref": result.ref,
			"binding_id": choice.bindingID, "binding_generation": choice.selection.BindingGeneration,
			"credential_ref": choice.selection.Credential.Ref, "configuration_digest": choice.configurationDigest,
			"resource_permission_profile": choice.selection.ResourcePermissionProfile,
		}).Scan(&matches); err != nil {
			return nil, serializableTransactionError(err, errs.ErrUnavailable)
		} else if !matches {
			return nil, errs.ErrForbidden
		}
	}
	if err := storePendingInvocationReceiptTx(ctx, tx, current, request, result.ref); err != nil {
		return nil, err
	}
	return result, nil
}

// loadPendingInvocationReceiptTx reads one exact node/call receipt under verified owner scope.
// Missing receipts differ from expired or malformed receipts, which never allow new allocation.
// loadPendingInvocationReceiptTx читает точную квитанцию node/call в проверенной области.
// Отсутствие отличается от истёкшего или повреждённого receipt: они не разрешают allocation.
func loadPendingInvocationReceiptTx(ctx context.Context, tx pgx.Tx, current scope, nodeID, callKey string) (*pendingInvocationReceipt, string, error) {
	keyDigest := sha256.Sum256([]byte(nodeID + "\x00" + callKey))
	var intent string
	var raw []byte
	var fresh bool
	err := tx.QueryRow(ctx, queryPendingInvocationReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "operation": pendingInvocationReceiptOperation, "key": hex.EncodeToString(keyDigest[:])}).Scan(&intent, &raw, &fresh)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", serializableTransactionError(err, errs.ErrUnavailable)
	}
	var receipt pendingInvocationReceipt
	var fields map[string]json.RawMessage
	if !fresh || json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || json.Unmarshal(raw, &receipt) != nil || receipt.InvocationRef == "" || len(receipt.ConnectionSnapshotDigest) != 64 {
		return nil, "", errs.ErrConflict
	}
	return &receipt, intent, nil
}

// storePendingInvocationReceiptTx atomically stores the alias and its immutable configuration
// commitment using the existing 24-hour owner receipt policy, without committing the caller.
// storePendingInvocationReceiptTx атомарно сохраняет alias и неизменяемый хеш конфигурации
// с существующим сроком owner receipt 24 часа, не завершая транзакцию caller.
func storePendingInvocationReceiptTx(ctx context.Context, tx pgx.Tx, current scope, request pendingInvocationRequest, ref string) error {
	keyDigest := sha256.Sum256([]byte(request.nodeID + "\x00" + request.idempotencyKey))
	payload, err := json.Marshal(pendingInvocationReceipt{InvocationRef: ref, ConnectionSnapshotDigest: request.connectionSnapshotDigest})
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, current.organizationID, current.actorID, pendingInvocationReceiptOperation, hex.EncodeToString(keyDigest[:]), request.intentDigest, "INTEGRATION_INVOCATION_ALIAS", payload); err != nil {
		return serializableTransactionError(err, mapWriteError(err))
	}
	return nil
}
