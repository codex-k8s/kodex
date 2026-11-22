package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesInvocationCaptures reads only current sealed generations in the original plan.
// queryKubernetesInvocationCaptures читает только current sealed generations original plan.
//
//go:embed sql/kubernetes_capture__invocation.sql
var queryKubernetesInvocationCaptures string

// queryKubernetesCaptureAdoptedEligible checks current canonical artifact admission for completion.
// queryKubernetesCaptureAdoptedEligible проверяет current canonical artifact admission для completion.
//
//go:embed sql/kubernetes_capture__adopted_eligible.sql
var queryKubernetesCaptureAdoptedEligible string

// kubernetesInvocationCapture separates a proven artifact wait from a retained partial result.
// kubernetesInvocationCapture отделяет proven artifact wait от retained partial result.
type kubernetesInvocationCapture struct {
	record                  kubernetesCaptureTaskRecord
	prefix, dropped         uint64
	digest                  string
	ready, pending, partial bool
}

// readKubernetesInvocationCaptures validates sealed bytes and ready artifacts without reading any
// output payload or current target. A pending result never grants the next child or a tool replay.
// readKubernetesInvocationCaptures проверяет sealed bytes и ready artifacts без чтения output
// payload либо current target. Pending result не разрешает следующий child либо tool replay.
func readKubernetesInvocationCaptures(ctx context.Context, runner queryRunner, organizationID string, binding value.ManagedKubernetesBinding) (map[string]kubernetesInvocationCapture, error) {
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return nil, errs.ErrInvalid
	}
	rows, err := runner.Query(ctx, queryKubernetesInvocationCaptures, pgx.StrictNamedArgs{"organization_id": organizationID, "invocation_ref": binding.InvocationRef, "generation": binding.Generation, "binding_digest": digest})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	result := map[string]kubernetesInvocationCapture{}
	for rows.Next() {
		var capture kubernetesInvocationCapture
		var raw, state, kind string
		record := &capture.record
		if rows.Scan(&record.id, &record.organizationID, &record.streamID, &record.receipt.CaptureRef, &raw, &record.receipt.SnapshotDigest, &record.receipt.CaptureGeneration, &state, &kind, &record.receipt.ReasonCode, &capture.digest, &capture.prefix, &capture.dropped) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		sum := sha256.Sum256([]byte(raw))
		if len(result) >= 256 || len(raw) > 16384 || hex.EncodeToString(sum[:]) != record.receipt.SnapshotDigest || internalrpcauth.DecodeStrictJSON([]byte(raw), &record.source) != nil || !validKubernetesCaptureSource(record.source) || record.source.BindingDigest != digest || record.source.InvocationRef != binding.InvocationRef || record.source.Generation != binding.Generation || record.source.CaptureGeneration != record.receipt.CaptureGeneration || result[record.source.StreamRef].record.id != "" {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		record.receipt.State = enum.ManagedKubernetesCaptureState(state)
		record.receipt.Kind = enum.ManagedKubernetesCaptureKind(kind)
		if !knownKubernetesCaptureState(record.receipt.State) || !managedkubernetes.ValidCaptureReason(record.receipt.ReasonCode) {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		native := record.source.TerminalReceipt
		if record.source.OwnerState == "COMPLETED" && native != nil && native.LocalState == "COMPLETED" && native.TransportJoined && record.source.OutputWatermark > 0 {
			switch record.receipt.State {
			case enum.KubernetesCaptureReady, enum.KubernetesCaptureClaimed:
				capture.pending = true
			case enum.KubernetesCaptureComplete:
				if !managedkubernetes.ValidDigest(capture.digest) || capture.prefix > record.source.AcknowledgedWatermark || capture.dropped > record.source.MaximumTotalBytes {
					rows.Close()
					return nil, errs.ErrUnavailable
				}
				if record.receipt.Kind == enum.KubernetesCaptureFull && capture.prefix == record.source.OutputWatermark && capture.dropped == 0 {
					capture.ready = true
				} else if record.receipt.Kind == enum.KubernetesCaptureEmpty && capture.prefix == record.source.OutputWatermark && capture.dropped == 0 {
					// A zero-byte EOF needs no fabricated artifact; the child query independently proves hasOutput=false.
					// Zero-byte EOF не требует fake artifact; child query независимо доказывает hasOutput=false.
				} else if record.receipt.Kind == enum.KubernetesCapturePartial {
					capture.partial = true
				} else {
					rows.Close()
					return nil, errs.ErrUnavailable
				}
			case enum.KubernetesCaptureRejected, enum.KubernetesCaptureWaitingOwner:
				capture.partial = true
			default:
				rows.Close()
				return nil, errs.ErrUnavailable
			}
		}
		result[record.source.StreamRef] = capture
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	for ref, capture := range result {
		if capture.ready {
			var eligible bool
			if err := runner.QueryRow(ctx, queryKubernetesCaptureAdoptedEligible, pgx.StrictNamedArgs{"organization_id": organizationID, "actor_id": capture.record.source.RootActorID, "capture_id": capture.record.id}).Scan(&eligible); err != nil || !eligible {
				return nil, errs.ErrUnavailable
			}
			if err := readKubernetesCaptureArtifacts(ctx, runner, &capture.record); err != nil {
				return nil, err
			}
			if len(capture.record.receipt.Artifacts) == 0 {
				return nil, errs.ErrUnavailable
			}
			result[ref] = capture
		}
	}
	return result, nil
}
