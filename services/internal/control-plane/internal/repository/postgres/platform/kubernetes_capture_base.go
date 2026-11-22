package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"hash"
	"io"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureBase reads one exact prior PARTIAL revision set of the same source stream.
// queryKubernetesCaptureBase читает exact prior PARTIAL revision set того же source stream.
//
//go:embed sql/kubernetes_capture__base.sql
var queryKubernetesCaptureBase string

// kubernetesCaptureBaseChannel owns a streaming exact-version canonical prefix, with bounded
// frame buffers and an independent whole-channel checksum. No transcript-sized allocation occurs.
// kubernetesCaptureBaseChannel владеет streaming exact-version canonical prefix с bounded
// frame buffers и whole-channel checksum. Transcript-sized allocation не выполняется.
type kubernetesCaptureBaseChannel struct {
	reader       io.ReadCloser
	checksum     hash.Hash
	digest       string
	size, offset uint64
}

// kubernetesCaptureBase keeps only exact version receipts and open historical channel readers.
// It is private assembly input, not artifact delivery, a native grant or another root's authority.
// kubernetesCaptureBase хранит exact version receipts и historical channel readers.
// Это private assembly input, не artifact delivery, native grant либо authority другого root.
type kubernetesCaptureBase struct {
	prefix   uint64
	channels map[string]*kubernetesCaptureBaseChannel
}

// clear closes all old canonical readers on every success, refusal and cancellation path.
// clear закрывает old canonical readers при success, refusal и cancellation.
func (base *kubernetesCaptureBase) clear() {
	for channel, body := range base.channels {
		if body.reader != nil {
			_ = body.reader.Close()
		}
		delete(base.channels, channel)
	}
}

// frame reads at most the existing32-KiB frame budget and verifies its original source digest.
// The caller owns and clears the returned frame buffer after its bounded private spool write.
// frame читает не более existing32-KiB frame budget и проверяет original source digest.
// Caller владеет frame buffer и очищает его после bounded private spool write.
func (base *kubernetesCaptureBase) frame(frame kubernetesCaptureFrame) ([]byte, error) {
	channel := base.channels[frame.Channel]
	if channel == nil || channel.reader == nil || channel.offset > channel.size || uint64(frame.Bytes) > channel.size-channel.offset || frame.Bytes > 32768 {
		return nil, errs.ErrUnavailable
	}
	body := make([]byte, frame.Bytes)
	if _, err := io.ReadFull(channel.reader, body); err != nil {
		clear(body)
		return nil, errs.ErrUnavailable
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != frame.Digest {
		clear(body)
		return nil, errs.ErrUnavailable
	}
	_, _ = channel.checksum.Write(body)
	channel.offset += uint64(len(body))
	return body, nil
}

// finish proves exact old-channel size/hash/EOF before the new assembly can be inspected or
// published. A changed version, truncated body or appended tail never becomes a usable result.
// finish доказывает exact old-channel size/hash/EOF до inspection либо publication new assembly.
// Changed version, truncated body либо appended tail не становится usable result.
func (base *kubernetesCaptureBase) finish() error {
	for _, channel := range base.channels {
		if channel.offset != channel.size || hex.EncodeToString(channel.checksum.Sum(nil)) != channel.digest {
			return errs.ErrUnavailable
		}
		var extra [1]byte
		count, err := io.ReadFull(channel.reader, extra[:])
		clear(extra[:])
		if count != 0 || err != io.EOF {
			return errs.ErrUnavailable
		}
		if err := channel.reader.Close(); err != nil {
			return errs.ErrUnavailable
		}
		channel.reader = nil
	}
	return nil
}

// readKubernetesCaptureBase permits physical stage cleanup after publication while preserving a
// reachable late-ACK successor. Only PARTIAL bodies are raw; normalized full copy files are excluded.
// Missing canonical content, forged origin or changed frame bytes fail closed without a stage replay.
// readKubernetesCaptureBase позволяет physical stage cleanup после publication, сохраняя
// reachable late-ACK successor. Только PARTIAL bodies содержат raw prefix; normalized full copy исключён.
// Missing canonical content/forged origin/changed bytes закрывают сборку без stage replay.
func (repository *Repository) readKubernetesCaptureBase(ctx context.Context, record kubernetesCaptureTaskRecord) (result kubernetesCaptureBase, resultErr error) {
	result.channels = map[string]*kubernetesCaptureBaseChannel{}
	defer func() {
		if resultErr != nil {
			result.clear()
		}
	}()
	rows, err := repository.pool.Query(ctx, queryKubernetesCaptureBase, pgx.StrictNamedArgs{"organization_id": record.organizationID, "stream_id": record.streamID, "capture_generation": record.source.CaptureGeneration, "acknowledged_watermark": record.source.AcknowledgedWatermark})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	// savedChannel retains only the canonical source and exact immutable object receipt.
	// savedChannel содержит только canonical source и exact immutable object receipt.
	type savedChannel struct {
		prefix                                                                          uint64
		sourceRaw, snapshot, digest, channel, originRaw, bodyDigest, key, version, etag string
		count, size                                                                     int64
	}
	var saved []savedChannel
	for rows.Next() {
		var item savedChannel
		if rows.Scan(&item.prefix, &item.sourceRaw, &item.snapshot, &item.digest, &item.count, &item.channel, &item.originRaw, &item.bodyDigest, &item.size, &item.key, &item.version, &item.etag) != nil || len(saved) >= 5 {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		saved = append(saved, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var total uint64
	for _, item := range saved {
		var source kubernetesCaptureSource
		var origin entity.KubernetesArtifactCaptureOrigin
		sum := sha256.Sum256([]byte(item.sourceRaw))
		if item.count != int64(len(saved)) || item.prefix > record.source.AcknowledgedWatermark || len(item.sourceRaw) > 16384 || hex.EncodeToString(sum[:]) != item.snapshot || internalrpcauth.DecodeStrictJSON([]byte(item.sourceRaw), &source) != nil || !validKubernetesCaptureSource(source) || source.StreamRef != record.source.StreamRef || source.RootActorID != record.source.RootActorID || source.BindingDigest != record.source.BindingDigest || source.ChildInputDigest != record.source.ChildInputDigest || len(item.originRaw) > 16384 || internalrpcauth.DecodeStrictJSON([]byte(item.originRaw), &origin) != nil || !validKubernetesCaptureOrigin(origin, source, item.digest, "PARTIAL") || origin.Channel != item.channel || origin.AcknowledgedPrefix != item.prefix || item.size < 0 || uint64(item.size) > record.source.MaximumTotalBytes-total || item.key == "" || item.version == "" || item.etag == "" || result.channels[item.channel] != nil || len(saved) > 0 && result.prefix != 0 && result.prefix != item.prefix {
			return result, errs.ErrUnavailable
		}
		object, err := repository.objects.Get(ctx, item.key, item.version)
		expected := objectstorage.Receipt{Key: item.key, VersionID: item.version, ETag: item.etag, Digest: item.bodyDigest, SizeBytes: item.size}
		if err != nil || object.Body == nil {
			return result, errs.ErrUnavailable
		}
		if object.Receipt != expected {
			_ = object.Body.Close()
			return result, errs.ErrUnavailable
		}
		result.channels[item.channel] = &kubernetesCaptureBaseChannel{reader: object.Body, checksum: sha256.New(), digest: strings.TrimPrefix(item.bodyDigest, "sha256:"), size: uint64(item.size)}
		result.prefix = item.prefix
		total += uint64(item.size)
	}
	return result, nil
}
