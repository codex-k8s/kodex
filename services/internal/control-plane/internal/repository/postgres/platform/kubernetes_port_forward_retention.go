package platform

import (
	"context"
	_ "embed"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesPortForwardRetention reads exact current retention tuples under the existing original stream/frame lock.
// queryKubernetesPortForwardRetention читает exact current retention tuples под existing original stream/frame lock.
//
//go:embed sql/kubernetes_pf__retention.sql
var queryKubernetesPortForwardRetention string

// kubernetesPortForwardRetentionTx issues metadata only for a fresh staged INPUT OPEN, preserving closed-but-pending CLOSE and refusing any earlier pending input advance.
// The snapshot removes no owner/output/loss receipt and grants neither native I/O nor future permissions.
// kubernetesPortForwardRetentionTx выдаёт metadata только для fresh staged INPUT OPEN, сохраняя closed-but-pending CLOSE и запрещая advance через прежний pending input.
// Snapshot не удаляет owner/output/loss receipt и не разрешает native I/O либо будущие permissions.
func kubernetesPortForwardRetentionTx(ctx context.Context, tx pgx.Tx, org, stream string, input command.ManagedKubernetesBeginFrame) (*managedkubernetes.PortForwardRetentionSet, error) {
	if input.Direction != enum.KubernetesFrameInput || input.Channel != "PORT_FORWARD" {
		return nil, nil
	}
	frame, err := readKubernetesPortForwardFrameTx(ctx, tx, org, stream, "INPUT", input.Sequence)
	if err != nil {
		return nil, err
	}
	if frame.action != "OPEN" {
		return nil, nil
	}
	if frame.slot >= managedkubernetes.MaximumForwardPorts {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryKubernetesPortForwardRetention, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := &managedkubernetes.PortForwardRetentionSet{}
	for rows.Next() {
		var item managedkubernetes.PortForwardConnectionIdentity
		var pendingClose bool
		if rows.Scan(&item.ConnectionRef, &item.Generation, &item.Slot, &pendingClose) != nil {
			return nil, errs.ErrUnavailable
		}
		// OPEN is the single current input stage; an older pending CLOSE must never be skipped or retired.
		// OPEN — единственный current input stage; прежний pending CLOSE нельзя пропустить либо удалить из retention.
		if pendingClose {
			return nil, errs.ErrConflict
		}
		result.Connections = append(result.Connections, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	opening := managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: frame.ref, Generation: frame.generation, Slot: byte(frame.slot)}
	if result.Validate(managedkubernetes.MaximumForwardPorts, &opening) != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}
