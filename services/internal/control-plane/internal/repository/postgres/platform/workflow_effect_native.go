package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// workflowEffectNativeProof holds the existing canonical native ledger projection, not a second
// receipt registry. Its data remains private to owner admission and never carries credentials.
// workflowEffectNativeProof хранит существующую canonical native ledger projection, не второй
// реестр receipts. Данные приватны для owner admission и не содержат credentials.
type workflowEffectNativeProof struct {
	InvocationRef, State, EffectKey, InputDigest                    string
	ReceiptRef, ReceiptDigest, PublicationRef, OriginalKey, GateRef string
	Input, Manifest                                                 []byte
	SourcePolicy                                                    string
}

// readWorkflowEffectNativeTx retains the original exact origin/lease/grant query for both old
// and new profiles. SQL errors remain unavailable and absence remains a closed conflict.
// readWorkflowEffectNativeTx сохраняет прежний exact origin/lease/grant запрос для старых
// и новых профилей. Ошибка SQL остаётся unavailable, отсутствие — закрытым conflict.
func readWorkflowEffectNativeTx(ctx context.Context, tx pgx.Tx, current scope, rootID, anchorID string, intent workflowEffectIntent) (workflowEffectNativeProof, error) {
	var proof workflowEffectNativeProof
	err := tx.QueryRow(ctx, queryWorkflowEffectReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": rootID, "anchor_id": anchorID,
		"origin_node_id": intent.OriginNodeID, "origin_revision_id": intent.OriginRevisionID, "origin_lease_id": intent.OriginLeaseID, "invocation_ref": intent.InvocationRef,
		"operation": intent.Operation, "connection_ref": intent.ConnectionRef, "grant_ref": intent.GrantRef, "grant_version": intent.GrantVersion,
		"definition_digest": intent.DefinitionDigest, "scope_digest": intent.ScopeDigest, "approval_policy": intent.ApprovalPolicy}).Scan(
		&proof.InvocationRef, &proof.State, &proof.EffectKey, &proof.InputDigest, &proof.ReceiptRef, &proof.ReceiptDigest, &proof.PublicationRef,
		&proof.OriginalKey, &proof.GateRef, &proof.Input, &proof.Manifest, &proof.SourcePolicy)
	if errors.Is(err, pgx.ErrNoRows) {
		return workflowEffectNativeProof{}, errs.ErrConflict
	}
	if err != nil {
		return workflowEffectNativeProof{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if proof.InvocationRef != intent.InvocationRef || proof.EffectKey != intent.EffectKey || proof.InputDigest != intent.InputDigest {
		return workflowEffectNativeProof{}, errs.ErrConflict
	}
	return proof, nil
}
