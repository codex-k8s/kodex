package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureFrames pages the sealed OUTPUT chain, including unacknowledged gaps.
// queryKubernetesCaptureFrames читает OUTPUT chain страницами, включая неподтверждённые gaps.
//
//go:embed sql/kubernetes_capture__frames.sql
var queryKubernetesCaptureFrames string

// queryKubernetesCaptureLosses counts only durable, unadmitted loss facts known before this seal.
// queryKubernetesCaptureLosses считает durable unadmitted loss facts, известные до этого seal.
//
//go:embed sql/kubernetes_capture__losses.sql
var queryKubernetesCaptureLosses string

// queryKubernetesCaptureStreamRecord reads immutable native evidence by the sealed owner commitment.
// queryKubernetesCaptureStreamRecord читает immutable native evidence по sealed owner commitment.
//
//go:embed sql/kubernetes_capture__stream_record.sql
var queryKubernetesCaptureStreamRecord string

// kubernetesCaptureFrame separates the immutable admitted tuple from protected storage coordinates.
// It is never returned by a public or job-facing RPC.
// kubernetesCaptureFrame отделяет immutable admitted tuple от protected storage coordinates.
// Не возвращается public либо job-facing RPC.
type kubernetesCaptureFrame struct {
	Sequence                uint64
	Channel, Digest         string
	Bytes                   uint32
	EOF, Acknowledged       bool
	Ref, Key, Version, ETag string
	Accepted                bool
}

// kubernetesCaptureAssembly owns only the contiguous confirmed prefix. No unacknowledged payload
// is loaded, and no later ACK can extend this generation's digest or channel content.
// kubernetesCaptureAssembly владеет только contiguous confirmed prefix. Неподтверждённый payload
// не читается; поздний ACK не расширяет digest либо channel content этого поколения.
type kubernetesCaptureAssembly struct {
	Channels                    map[string]*kubernetesCaptureSpool
	Prefix, Bytes, DroppedBytes uint64
	Digest                      string
	Kind                        enum.ManagedKubernetesCaptureKind
	History                     *entity.ManagedKubernetesStreamHistory
}

// clear releases owned captured bytes on every completion, rejection and cancellation path.
// clear очищает owned captured bytes при completion, rejection и cancellation.
func (assembly *kubernetesCaptureAssembly) clear() {
	for channel, body := range assembly.Channels {
		_ = body.Close()
		delete(assembly.Channels, channel)
	}
}

// readKubernetesCaptureHistory reuses the existing strict receipt decoder without constructing an
// invocation binding from a guessed plaintext fence. A seal predating native evidence stays partial.
// readKubernetesCaptureHistory использует existing strict receipt decoder без конструирования
// invocation binding из выдуманного plaintext fence. Seal до native evidence остаётся partial.
func (repository *Repository) readKubernetesCaptureHistory(ctx context.Context, record kubernetesCaptureTaskRecord) (*entity.ManagedKubernetesStreamHistory, error) {
	source := record.source
	stored, err := scanKubernetesStreamRecord(repository.pool.QueryRow(ctx, queryKubernetesCaptureStreamRecord, pgx.StrictNamedArgs{
		"organization_id": record.organizationID, "invocation_ref": source.InvocationRef,
		"child_input_digest": source.ChildInputDigest, "binding_digest": source.BindingDigest,
		"generation": source.Generation, "stream_ref": source.StreamRef,
	}))
	if err != nil || stored.id != record.streamID || stored.snapshotDigest != source.SnapshotDigest || stored.inputDigest != source.ChildInputDigest || stored.operation != source.Operation || stored.targetRef != source.TargetRef || stored.targetRevision != source.TargetRevision || stored.maximumTotal != source.MaximumTotalBytes {
		return nil, errs.ErrUnavailable
	}
	if !source.NativeTerminalKnown {
		return nil, nil
	}
	if stored.terminalDigest != source.TerminalDigest {
		return nil, errs.ErrUnavailable
	}
	history, err := stored.historyFromOrigin(stored.originFromDigest(source.BindingDigest, source.StreamRef))
	if err != nil || history.Terminal == nil || history.Terminal.TransportJoined != source.TransportJoined {
		return nil, errs.ErrUnavailable
	}
	return history, nil
}

// assembleKubernetesCapture loads exact object versions for the immutable owner-ACK prefix using
// bounded metadata pages and the stream's declared byte budget. It neither scans nor publishes.
// assembleKubernetesCapture читает exact object versions immutable owner-ACK prefix с bounded
// metadata pages и объявленным stream byte budget. Не выполняет scan либо publication.
func (repository *Repository) assembleKubernetesCapture(ctx context.Context, record kubernetesCaptureTaskRecord) (result kubernetesCaptureAssembly, resultErr error) {
	result.Channels = map[string]*kubernetesCaptureSpool{}
	if repository.kubernetesCaptureSpool == nil {
		return result, errs.ErrUnavailable
	}
	defer func() {
		if resultErr != nil {
			result.clear()
		}
	}()
	history, err := repository.readKubernetesCaptureHistory(ctx, record)
	if err != nil {
		return result, err
	}
	result.History = history
	base, err := repository.readKubernetesCaptureBase(ctx, record)
	if err != nil {
		return result, err
	}
	defer base.clear()
	var lossFacts uint64
	if err := repository.pool.QueryRow(ctx, queryKubernetesCaptureLosses, pgx.StrictNamedArgs{"organization_id": record.organizationID, "stream_id": record.streamID, "sealed_at": record.source.SealedAt}).Scan(&result.DroppedBytes, &lossFacts); err != nil || result.DroppedBytes != record.source.LossBytes || lossFacts != record.source.LossFacts || result.DroppedBytes > record.source.MaximumTotalBytes {
		return result, errs.ErrUnavailable
	}
	commitment := sha256.New()
	_, _ = io.WriteString(commitment, "kodex.kubernetes.capture-prefix/v1\n"+record.receipt.SnapshotDigest+"\n")
	for result.Prefix < record.source.AcknowledgedWatermark {
		rows, err := repository.pool.Query(ctx, queryKubernetesCaptureFrames, pgx.StrictNamedArgs{"organization_id": record.organizationID, "stream_id": record.streamID, "sealed_at": record.source.SealedAt, "after_sequence": result.Prefix, "watermark": record.source.AcknowledgedWatermark})
		if err != nil {
			return result, errs.ErrUnavailable
		}
		page := make([]kubernetesCaptureFrame, 0, 256)
		for rows.Next() {
			var frame kubernetesCaptureFrame
			if err := rows.Scan(&frame.Sequence, &frame.Channel, &frame.Digest, &frame.Bytes, &frame.EOF, &frame.Acknowledged, &frame.Ref, &frame.Key, &frame.Version, &frame.ETag, &frame.Accepted); err != nil {
				rows.Close()
				return result, errs.ErrUnavailable
			}
			page = append(page, frame)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(page) == 0 {
			return result, errs.ErrUnavailable
		}
		gap := false
		for _, frame := range page {
			if frame.Sequence != result.Prefix+1 || !frame.Acknowledged {
				gap = true
				break
			}
			if !captureOutputChannel(frame.Channel) || !managedkubernetes.ValidDigest(frame.Digest) || frame.Bytes > 32768 || frame.Bytes == 0 && !frame.EOF || uint64(frame.Bytes) > record.source.MaximumTotalBytes-result.Bytes {
				return result, errs.ErrUnavailable
			}
			if frame.Bytes > 0 {
				if !frame.Accepted || frame.Key != "organizations/"+record.organizationID+"/kubernetes-output-stages/"+frame.Ref || frame.Version == "" || frame.ETag == "" {
					return result, errs.ErrUnavailable
				}
				var body []byte
				borrowed := frame.Sequence <= base.prefix
				if borrowed {
					body, err = base.frame(frame)
				} else {
					body, err = repository.readKubernetesCaptureObject(ctx, frame)
				}
				if err != nil {
					return result, err
				}
				spool := result.Channels[frame.Channel]
				if spool == nil {
					spool, err = repository.kubernetesCaptureSpool.open(ctx, frame.Channel)
					if err == nil {
						result.Channels[frame.Channel] = spool
					}
				}
				if err == nil {
					var count int
					count, err = spool.Write(body)
					if err == nil && count != len(body) {
						err = io.ErrShortWrite
					}
				}
				clear(body)
				if err != nil {
					return result, errs.ErrUnavailable
				}
			}
			metadata, _ := json.Marshal(struct {
				Sequence        uint64
				Channel, Digest string
				Bytes           uint32
				EOF             bool
			}{frame.Sequence, frame.Channel, frame.Digest, frame.Bytes, frame.EOF})
			_, _ = commitment.Write(metadata)
			_, _ = commitment.Write([]byte{'\n'})
			result.Prefix = frame.Sequence

			result.Bytes += uint64(frame.Bytes)
		}
		if gap {
			break
		}
	}
	if result.Bytes > record.source.MaximumTotalBytes-result.DroppedBytes {
		return result, errs.ErrUnavailable
	}
	if result.Prefix < base.prefix {
		return result, errs.ErrUnavailable
	}
	if err := base.finish(); err != nil {
		return result, err
	}
	result.Digest = hex.EncodeToString(commitment.Sum(nil))
	result.Kind = enum.KubernetesCapturePartial
	if result.Prefix == record.source.OutputWatermark && result.DroppedBytes == 0 && record.source.OwnerState == "COMPLETED" && history != nil && history.Terminal != nil && history.Terminal.LocalState == "COMPLETED" && history.Terminal.TransportJoined {
		result.Kind = enum.KubernetesCaptureFull
		if result.Bytes == 0 {
			result.Kind = enum.KubernetesCaptureEmpty
		}
	}
	return result, nil
}

// captureOutputChannel keeps persisted channel metadata within the registered OUTPUT union.
// captureOutputChannel ограничивает persisted channel metadata registered OUTPUT union.
func captureOutputChannel(channel string) bool {
	return channel == "STDOUT" || channel == "STDERR" || channel == "CONTROL" || channel == "PORT_FORWARD" || channel == "FILE_CONTENT"
}

// readKubernetesCaptureObject verifies both the version receipt and actual bounded body digest.
// No provider key, ETag or uninspected payload escapes the owner adapter.
// readKubernetesCaptureObject проверяет version receipt и фактический bounded body digest.
// Provider key, ETag либо uninspected payload не выходят из owner adapter.
func (repository *Repository) readKubernetesCaptureObject(ctx context.Context, frame kubernetesCaptureFrame) ([]byte, error) {
	object, err := repository.objects.Get(ctx, frame.Key, frame.Version)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	if object.Body == nil {
		return nil, errs.ErrUnavailable
	}
	defer object.Body.Close()
	expected := objectstorage.Receipt{Key: frame.Key, VersionID: frame.Version, ETag: frame.ETag, Digest: "sha256:" + frame.Digest, SizeBytes: int64(frame.Bytes)}
	if object.Receipt != expected {
		return nil, errs.ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(object.Body, int64(frame.Bytes)+1))
	sum := sha256.Sum256(body)
	if err != nil || len(body) != int(frame.Bytes) || hex.EncodeToString(sum[:]) != frame.Digest || ctx.Err() != nil {
		clear(body)
		return nil, errs.ErrUnavailable
	}
	return body, nil
}
