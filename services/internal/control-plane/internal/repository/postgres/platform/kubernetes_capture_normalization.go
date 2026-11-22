package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// kubernetesCaptureContent owns one inspected channel body and its immutable content commitment.
// kubernetesCaptureContent владеет одним inspected channel body и immutable content commitment.
type kubernetesCaptureContent struct {
	Channel, FileName, MediaType, Digest string
	Body                                 *kubernetesCaptureSpool `json:"-"`
	ContentKind                          artifactpolicy.KubernetesCapturedContentKind
	Origin                               entity.KubernetesArtifactCaptureOrigin
}

// String omits captured bytes and private source coordinates from diagnostics.
// String исключает captured bytes и private source coordinates из diagnostics.
func (kubernetesCaptureContent) String() string { return "[private Kubernetes capture content]" }

// GoString preserves the same exclusion for detailed diagnostic formatting.
// GoString сохраняет исключение данных при detailed diagnostic formatting.
func (content kubernetesCaptureContent) GoString() string { return content.String() }

// normalizeKubernetesCaptureChannels moves anonymous file ownership into inspected content.
// Complete copy normalization alone reads its separately approved16-MiB archive in memory;
// ordinary256-MiB streams and old canonical prefixes stay on bounded private spool files.
// normalizeKubernetesCaptureChannels передаёт anonymous file ownership inspected content.
// Лишь complete copy normalization читает отдельно approved16-MiB archive в память;
// ordinary256-MiB streams и old canonical prefixes остаются в bounded private spool files.
func (repository *Repository) normalizeKubernetesCaptureChannels(ctx context.Context, record kubernetesCaptureTaskRecord, assembly kubernetesCaptureAssembly) (result []kubernetesCaptureContent, resultErr error) {
	defer func() {
		if resultErr != nil {
			for index := range result {
				_ = result[index].Body.Close()
			}
		}
	}()
	for _, channel := range []string{"STDOUT", "STDERR", "CONTROL", "PORT_FORWARD", "FILE_CONTENT"} {
		body := assembly.Channels[channel]
		if body == nil {
			continue
		}
		delete(assembly.Channels, channel)
		content, err := repository.normalizeKubernetesCapturedChannel(ctx, record, assembly, channel, body)
		if err != nil {
			return result, err
		}
		result = append(result, content)
	}
	return result, nil
}

// normalizeKubernetesCapturedChannel inspects a seekable private file without duplicating the
// transcript. A rejected or failed normalization closes only transient local content; immutable
// source stages and prior canonical revisions remain available to fenced recovery.
// normalizeKubernetesCapturedChannel проверяет seekable private file без дублирования transcript.
// Отказ закрывает лишь transient local content; immutable source stages и prior canonical
// revisions сохраняются для fenced recovery.
func (repository *Repository) normalizeKubernetesCapturedChannel(ctx context.Context, record kubernetesCaptureTaskRecord, assembly kubernetesCaptureAssembly, channel string, spool *kubernetesCaptureSpool) (content kubernetesCaptureContent, resultErr error) {
	defer func() {
		if resultErr != nil {
			_ = spool.Close()
		}
	}()
	content = kubernetesCaptureContent{Channel: channel, FileName: "kubernetes-" + record.source.StreamRef + "-" + strings.ToLower(channel) + ".bin", MediaType: "application/octet-stream", Body: spool, Origin: kubernetesCaptureOrigin(record, assembly, channel), ContentKind: artifactpolicy.KubernetesCaptureOpaque}
	reader := &kubernetesCaptureSpoolReader{spool: spool}
	if channel == "STDOUT" || channel == "STDERR" {
		name := "kubernetes-" + record.source.StreamRef + "-" + strings.ToLower(channel) + ".txt"
		verdict, err := artifactpolicy.InspectReader(name, "text/plain; charset=utf-8", reader, reader.Size())
		if err != nil {
			return content, errs.ErrUnavailable
		}
		if verdict.ScanState == artifactpolicy.ScanClean {
			content.FileName, content.MediaType, content.ContentKind = name, "text/plain; charset=utf-8", artifactpolicy.KubernetesCaptureText
		}
	}
	if record.source.Operation == "kubernetes.cp.download" && channel == "FILE_CONTENT" {
		if assembly.Kind != enum.KubernetesCaptureFull {
			content.FileName = "kubernetes-" + record.source.StreamRef + "-partial-copy.bin"
		} else {
			raw, err := repository.readKubernetesCaptureRecipe(ctx, record)
			if err != nil {
				return content, err
			}
			input, _, err := managedkubernetes.DecodeOperationInput(record.source.Operation, raw)
			clear(raw)
			if err != nil {
				return content, errs.ErrUnavailable
			}
			profile, err := managedkubernetes.DecodeCopyProfile(record.source.Operation, input)
			if err != nil {
				return content, errs.ErrUnavailable
			}
			if profile.MaximumBytes > record.source.MaximumTotalBytes {
				profile.MaximumBytes = record.source.MaximumTotalBytes
			}
			if uint64(reader.Size()) > profile.MaximumBytes {
				return content, errors.Join(errs.ErrInvalid, errKubernetesCaptureContentRejected)
			}
			if _, err := reader.Seek(0, io.SeekStart); err != nil {
				return content, errs.ErrUnavailable
			}
			archive, err := io.ReadAll(io.LimitReader(reader, int64(profile.MaximumBytes)+1))
			if err != nil || int64(len(archive)) != reader.Size() {
				clear(archive)
				return content, errs.ErrUnavailable
			}
			normalized, err := managedkubernetes.NormalizeCopyDownload(profile, archive)
			clear(archive)
			if err != nil {
				return content, errors.Join(errs.ErrInvalid, errKubernetesCaptureContentRejected)
			}
			err = spool.reset()
			if err == nil {
				var count int
				count, err = spool.Write(normalized.Bytes)
				if err == nil && count != len(normalized.Bytes) {
					err = io.ErrShortWrite
				}
			}
			clear(normalized.Bytes)
			if err != nil {
				return content, errs.ErrUnavailable
			}
			content.MediaType = normalized.MediaType
			content.FileName = path.Base(profile.RemotePath)
			content.ContentKind = artifactpolicy.KubernetesCaptureCopyFile
			if profile.TransferFormat == "TAR" {
				content.FileName += ".tar"
				content.ContentKind = artifactpolicy.KubernetesCaptureCopyTar
			}
			if content.FileName == "/" || content.FileName == "." || content.FileName == ".tar" || content.FileName == "/.tar" {
				content.FileName = "kubernetes-copy.tar"
			}
		}
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return content, errs.ErrUnavailable
	}
	checksum := sha256.New()
	count, err := io.Copy(checksum, io.LimitReader(reader, reader.Size()+1))
	if err != nil || count != reader.Size() {
		return content, errs.ErrUnavailable
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return content, errs.ErrUnavailable
	}
	content.Digest = "sha256:" + hex.EncodeToString(checksum.Sum(nil))
	return content, nil
}

// kubernetesCaptureOrigin records source truth and capture quality independently from business
// success. Missing native evidence yields UNKNOWN without fabricating a join or remote exit.
// kubernetesCaptureOrigin записывает source truth и capture quality отдельно от business success.
// Missing native evidence даёт UNKNOWN без выдуманного join либо remote exit.
func kubernetesCaptureOrigin(record kubernetesCaptureTaskRecord, assembly kubernetesCaptureAssembly, channel string) entity.KubernetesArtifactCaptureOrigin {
	source := record.source
	result := entity.KubernetesArtifactCaptureOrigin{InvocationRef: source.InvocationRef, StreamRef: source.StreamRef, StreamGeneration: source.Generation, CaptureGeneration: source.CaptureGeneration, Operation: source.Operation, Channel: channel, Kind: string(assembly.Kind), AcknowledgedPrefix: assembly.Prefix, OutputWatermark: source.OutputWatermark, DroppedBytes: assembly.DroppedBytes, NativeTerminalKnown: source.NativeTerminalKnown, TransportJoined: source.TransportJoined, OwnerState: source.OwnerState, LocalState: "UNKNOWN", RemoteOutcome: "UNKNOWN", Resource: source.Resource, TargetRef: source.TargetRef, TargetRevision: source.TargetRevision, ConnectionRef: source.ConnectionRef, SourceDigest: assembly.Digest, SealedAt: source.SealedAt}
	if assembly.History != nil && assembly.History.Terminal != nil {
		terminal := assembly.History.Terminal
		result.LocalState = terminal.LocalState
		result.RemoteOutcome = terminal.RemoteOutcome
		result.ReasonCode = terminal.ReasonCode
		if terminal.ExitCode != nil {
			value := *terminal.ExitCode
			result.ExitCode = &value
		}
	}
	return result
}
