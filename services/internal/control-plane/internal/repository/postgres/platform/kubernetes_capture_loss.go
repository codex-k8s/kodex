package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// kubernetesCaptureLoss retains one native event that was refused before frame admission.
// Source fields must come from the already verified owner connection, not public request authority.
// kubernetesCaptureLoss хранит один native event, отклонённый до frame admission.
// Source fields берутся из проверенного owner connection, не public request authority.
type kubernetesCaptureLoss struct {
	SourceRef        string
	SourceGeneration int64
	NativeOrdinal    uint64
	PayloadDigest    string
	ByteCount        uint32
	ReasonCode       string
}

// kubernetesCapturePFClosed is the finite loss reason for a closed approved TCP connection.
// kubernetesCapturePFClosed — закрытая причина потери для закрытого approved TCP connection.
const kubernetesCapturePFClosed = "PORT_FORWARD_CONNECTION_CLOSED"

// queryKubernetesCaptureLossLock serializes loss facts with the existing mutable stream.
// queryKubernetesCaptureLossLock сериализует loss facts с existing mutable stream.
//
//go:embed sql/kubernetes_capture__loss_lock.sql
var queryKubernetesCaptureLossLock string

// queryKubernetesCaptureLossQuota observes byte accounting after the stream lock has been acquired.
// queryKubernetesCaptureLossQuota читает byte accounting после приобретения stream lock.
//
//go:embed sql/kubernetes_capture__loss_quota.sql
var queryKubernetesCaptureLossQuota string

// queryKubernetesCaptureLossRead reads only the exact native event, without bytes or new credit.
// queryKubernetesCaptureLossRead читает только exact native event без bytes либо нового credit.
//
//go:embed sql/kubernetes_capture__loss_read.sql
var queryKubernetesCaptureLossRead string

// queryKubernetesCaptureLossInsert records bounded dropped-byte accounting independently of ACK counters.
// queryKubernetesCaptureLossInsert записывает bounded dropped-byte accounting отдельно от ACK counters.
//
//go:embed sql/kubernetes_capture__loss_insert.sql
var queryKubernetesCaptureLossInsert string

// valid validates representable immutable evidence, not authority or a lifetime frame budget.
// valid проверяет представимые immutable facts, не authority либо lifetime frame budget.
func (loss kubernetesCaptureLoss) valid() bool {
	if !strings.HasPrefix(loss.SourceRef, "kpf_") || len(loss.SourceRef) < 12 || len(loss.SourceRef) > 93 || loss.SourceGeneration < 1 || !managedkubernetes.ValidFrameSequence(loss.NativeOrdinal) || !managedkubernetes.ValidDigest(loss.PayloadDigest) || loss.ByteCount == 0 || loss.ByteCount > managedkubernetes.MaximumFrameBytes || loss.ReasonCode != kubernetesCapturePFClosed {
		return false
	}
	for _, character := range loss.SourceRef[4:] {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

// markKubernetesCapturePartialTx records an exact verified native loss once under the original stream
// lock. Replay cannot charge bytes again; a changed tuple conflicts. It creates no frame, sequence,
// grant or ACK and never promotes a closed connection. The caller retains transaction ownership.
// markKubernetesCapturePartialTx записывает exact verified native loss один раз под original stream
// lock. Replay не расходует bytes повторно; иной tuple даёт conflict. Не создаёт frame, sequence,
// grant либо ACK и не открывает connection. Caller сохраняет владение transaction.
func markKubernetesCapturePartialTx(ctx context.Context, tx pgx.Tx, organizationID, streamID string, loss kubernetesCaptureLoss) error {
	if !loss.valid() {
		return errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{"organization_id": organizationID, "stream_id": streamID}
	var maximum, logical, used uint64
	var maximumFrame uint32
	err := tx.QueryRow(ctx, queryKubernetesCaptureLossLock, args).Scan(&maximum, &logical, &maximumFrame)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	lookup := pgx.StrictNamedArgs{"organization_id": organizationID, "stream_id": streamID, "source_ref": loss.SourceRef, "source_generation": loss.SourceGeneration, "native_ordinal": loss.NativeOrdinal}
	var digest, reason string
	var count uint32
	err = tx.QueryRow(ctx, queryKubernetesCaptureLossRead, lookup).Scan(&digest, &count, &reason)
	if err == nil {
		if digest != loss.PayloadDigest || count != loss.ByteCount || reason != loss.ReasonCode {
			return errs.ErrConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if tx.QueryRow(ctx, queryKubernetesCaptureLossQuota, args).Scan(&used) != nil {
		return errs.ErrUnavailable
	}
	if loss.ByteCount > maximumFrame || logical > maximum || used > maximum-logical || uint64(loss.ByteCount) > maximum-logical-used {
		return errs.ErrUnavailable
	}
	lookup["payload_digest"], lookup["byte_count"], lookup["reason_code"] = loss.PayloadDigest, loss.ByteCount, loss.ReasonCode
	if _, err = tx.Exec(ctx, queryKubernetesCaptureLossInsert, lookup); err != nil {
		return mapWriteError(err)
	}
	return nil
}
