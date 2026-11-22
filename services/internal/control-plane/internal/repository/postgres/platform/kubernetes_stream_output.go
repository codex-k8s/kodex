package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesOutputPublicCursor reads bounded coordinates only after the original stream graph is locked.
// queryKubernetesOutputPublicCursor читает bounded coordinates только после блокировки исходного stream graph.
//
//go:embed sql/kubernetes_output__public_cursor.sql
var queryKubernetesOutputPublicCursor string

var _ port.KubernetesStreamOutputReader = (*Repository)(nil)

// kubernetesOutputDisclosure keeps private immutable storage pins apart from the public result and monotonic deadline.
// kubernetesOutputDisclosure отделяет private immutable storage pins от публичного результата и monotonic deadline.
type kubernetesOutputDisclosure struct {
	view  entity.KubernetesStreamOutput
	frame kubernetesCaptureFrame
	until time.Time
}

// ReadKubernetesStreamOutput authorizes an original-user data read twice around exact versioned object retrieval.
// It never remints frame credit, ACKs output, starts work or treats storage success as a native success.
// ReadKubernetesStreamOutput дважды допускает чтение исходным пользователем вокруг exact versioned object retrieval.
// Не выпускает frame credit, не ACKs output, не запускает работу и не считает storage success native success.
func (r *Repository) ReadKubernetesStreamOutput(ctx context.Context, p value.Principal, q query.KubernetesStreamOutputRead) (entity.KubernetesStreamOutput, error) {
	var zero entity.KubernetesStreamOutput
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.output.read"); err != nil {
		return zero, err
	}
	if q.Generation < 1 || q.AfterSequence > managedkubernetes.MaximumFrameSequence {
		return zero, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	read := func(ctx context.Context) (kubernetesOutputDisclosure, error) {
		return r.readKubernetesOutputDisclosure(ctx, p, q)
	}
	first, err := retryAssistantLockedRead(ctx, read)
	if err != nil {
		return zero, err
	}
	if first.view.Status != "FRAME" {
		return first.view, nil
	}
	fetch, stop := context.WithDeadline(ctx, first.until)
	body, err := r.readKubernetesCaptureObject(fetch, first.frame)
	stop()
	if err != nil {
		return zero, err
	}
	keep := false
	defer func() {
		if !keep {
			clear(body)
		}
	}()
	last, err := retryAssistantLockedRead(ctx, read)
	if err != nil {
		return zero, err
	}
	if last.view.Status != "FRAME" || last.frame != first.frame || !time.Now().Before(first.until) || !time.Now().Before(last.until) {
		return zero, errs.ErrNotFound
	}
	// Later authorization can shorten disclosure but cannot renew the first read's allowance.
	// Поздний допуск может сократить выдачу, но не продлить первое окно чтения.
	if last.view.AuthorizedUntil.After(first.view.AuthorizedUntil) {
		last.view.AuthorizedUntil = first.view.AuthorizedUntil
	}
	last.view.Frame = &entity.KubernetesStreamOutputFrame{StageRef: first.frame.Ref, Sequence: first.frame.Sequence, Channel: first.frame.Channel, Digest: first.frame.Digest, Payload: body}
	if !last.view.Valid() || !last.view.AuthorizedUntil.After(time.Now()) || !time.Now().Before(first.until) || !time.Now().Before(last.until) || ctx.Err() != nil {
		return zero, errs.ErrUnavailable
	}
	keep = true
	return last.view, nil
}

// readKubernetesOutputDisclosure derives every execution pin from the same current owner path used by public INPUT.
// Only a still-running parent can authorize this live surface; stopped history belongs to artifact reads.
// readKubernetesOutputDisclosure выводит execution pins из того же current owner path, что и public INPUT.
// Только продолжающийся parent допускает live surface; история после Stop принадлежит artifact reads.
func (r *Repository) readKubernetesOutputDisclosure(ctx context.Context, p value.Principal, q query.KubernetesStreamOutputRead) (result kubernetesOutputDisclosure, resultErr error) {
	started := time.Now()
	current, err := r.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	var id, connection, run, node string
	err = tx.QueryRow(ctx, queryKubernetesInputPublicOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "invocation_ref": q.InvocationRef, "project_ref": p.ProjectRef}).Scan(&id, &connection, &run, &node)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	owner, _, fence, generation, err := r.readKubernetesChildGateOwnerTx(ctx, tx, current, q.InvocationRef)
	defer clear(owner.normalized)
	defer clear(owner.candidate.boundedInput)
	if err != nil {
		return result, concealAccessDenial(err, errs.ErrNotFound)
	}
	record, err := r.lockPublicKubernetesInput(ctx, tx, current, p, q.InvocationRef, q.StreamRef)
	if err != nil {
		return result, err
	}
	snapshot, err := kubernetesOperationSnapshotDigestFromClaim(owner, fence, generation)
	if err != nil || snapshot != record.snapshotDigest || generation != q.Generation || record.view.Generation != q.Generation || record.invocationState != "RUNNING" {
		return result, errs.ErrNotFound
	}
	var watermark uint64
	var childDigest string
	var observed time.Time
	if err := tx.QueryRow(ctx, queryKubernetesOutputPublicCursor, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": record.id}).Scan(&watermark, &childDigest, &observed); err != nil {
		return result, errs.ErrUnavailable
	}
	if q.AfterSequence > watermark || watermark > managedkubernetes.MaximumFrameSequence {
		return result, errs.ErrInvalid
	}
	stored, err := scanKubernetesStreamRecord(tx.QueryRow(ctx, queryKubernetesCaptureStreamRecord, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": q.InvocationRef, "child_input_digest": childDigest, "binding_digest": record.bindingDigest, "generation": generation, "stream_ref": q.StreamRef}))
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if stored.id != record.id || stored.snapshotDigest != snapshot || !stored.childCurrent || stored.leaseExpired || stored.invocationState != "RUNNING" {
		return result, errs.ErrNotFound
	}
	if stored.state == enum.KubernetesStreamCanceled || stored.state == enum.KubernetesStreamRevoked || stored.state == enum.KubernetesStreamExpired {
		return result, errs.ErrNotFound
	}
	history, err := stored.historyFromOrigin(stored.originFromDigest(record.bindingDigest, q.StreamRef))
	if err != nil {
		return result, err
	}
	until := observed.Add(5 * time.Second)
	for _, bound := range []time.Time{owner.lease.ExpiresAt, stored.hardDeadline, stored.planDeadline} {
		if bound.Before(until) {
			until = bound
		}
	}
	result.until = started.Add(until.Sub(observed))
	v := &result.view
	*v = entity.KubernetesStreamOutput{InvocationRef: q.InvocationRef, StreamRef: q.StreamRef, Generation: generation, AfterSequence: q.AfterSequence, OutputWatermark: watermark, AuthorizedUntil: until, Status: "WAITING"}
	if history.Terminal != nil {
		v.NativeTerminalKnown = true
		v.TransportJoined = history.Terminal.TransportJoined
	}
	var lossFacts uint64
	if err := tx.QueryRow(ctx, queryKubernetesCaptureLosses, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": record.id, "sealed_at": observed}).Scan(&v.DroppedBytes, &lossFacts); err != nil || v.DroppedBytes > stored.maximumTotal || lossFacts > v.DroppedBytes {
		return result, errs.ErrUnavailable
	}
	if q.AfterSequence < watermark {
		if err := readKubernetesDisclosureFrame(ctx, tx, current.organizationID, record.id, observed, q.AfterSequence, &result.frame); err != nil {
			return result, err
		}
		if result.frame.Acknowledged {
			if !result.frame.Accepted || result.frame.Bytes == 0 || result.frame.Bytes > stored.maximumFrame || !captureOutputChannel(result.frame.Channel) || !managedkubernetes.ValidDigest(result.frame.Digest) || result.frame.Key != "organizations/"+current.organizationID+"/kubernetes-output-stages/"+result.frame.Ref || result.frame.Version == "" || result.frame.ETag == "" {
				return result, errs.ErrUnavailable
			}
			v.Status = "FRAME"
		}
	}
	if v.Status != "FRAME" && stored.state.Terminal() {
		v.Status = "INCOMPLETE"
		if stored.state == enum.KubernetesStreamCompleted && v.NativeTerminalKnown && v.TransportJoined && v.DroppedBytes == 0 && q.AfterSequence == watermark {
			v.Status = "ENDED"
		}
	}
	// Re-evaluate finite ACL conditions after all blocking reads; a lock wait cannot extend user access.
	// Повторно проверяем finite ACL conditions после блокирующих чтений; ожидание не продлевает user access.
	for _, access := range []struct{ permission, kind, ref string }{{"run.view", "RUN", run}, {"integration.view", "INTEGRATION", connection}} {
		accessUntil, err := r.requireAccessUntil(ctx, tx, current, access.permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: access.kind, ResourceRef: access.ref})
		if err != nil {
			return result, concealAccessDenial(err, errs.ErrNotFound)
		}
		if !accessUntil.IsZero() {
			if accessUntil.Before(result.until) {
				result.until = accessUntil
			}
			if accessUntil.Before(v.AuthorizedUntil) {
				v.AuthorizedUntil = accessUntil
			}
		}
	}
	if !time.Now().Before(result.until) {
		return result, errs.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if !time.Now().Before(result.until) {
		return kubernetesOutputDisclosure{}, errs.ErrNotFound
	}
	return result, nil
}

// readKubernetesDisclosureFrame reads exactly the next ordinal, preserving gaps and original ACK facts.
// readKubernetesDisclosureFrame читает ровно следующий ordinal, сохраняя gaps и исходные ACK facts.
func readKubernetesDisclosureFrame(ctx context.Context, tx pgx.Tx, org, stream string, observed time.Time, after uint64, frame *kubernetesCaptureFrame) error {
	rows, err := tx.Query(ctx, queryKubernetesCaptureFrames, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "sealed_at": observed, "after_sequence": after, "watermark": after + 1})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	if !rows.Next() {
		return errs.ErrUnavailable
	}
	if err := rows.Scan(&frame.Sequence, &frame.Channel, &frame.Digest, &frame.Bytes, &frame.EOF, &frame.Acknowledged, &frame.Ref, &frame.Key, &frame.Version, &frame.ETag, &frame.Accepted); err != nil {
		return errs.ErrUnavailable
	}
	if rows.Next() || rows.Err() != nil || frame.Sequence != after+1 {
		return errs.ErrUnavailable
	}
	return nil
}
