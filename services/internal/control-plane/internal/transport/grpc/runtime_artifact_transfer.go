package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const artifactTransferContentMismatch = "runtime artifact transfer content mismatch"

func (server *Server) StreamExecutionArtifact(request *cp.StreamExecutionArtifactRequest, stream cp.RuntimeWorkService_StreamExecutionArtifactServer) error {
	ctx := stream.Context()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > runtimecontract.MaximumArtifactTransferDuration {
		return transportError(errs.ErrInvalid)
	}
	p, err := principal(ctx, cp.RuntimeWorkService_StreamExecutionArtifact_FullMethodName)
	if err != nil {
		return err
	}
	download, err := server.service.OpenExecutionArtifactTransfer(ctx, p, request.GetLeaseRef(), request.GetFence(), request.GetGeneration(), request.GetArtifactRef())
	if err != nil {
		return transportError(err)
	}
	return sendExecutionArtifact(ctx, download, stream.Send, func() error {
		current, err := server.service.OpenExecutionArtifactTransfer(ctx, p, request.GetLeaseRef(), request.GetFence(), request.GetGeneration(), request.GetArtifactRef())
		if err != nil {
			return err
		}
		closeErr := current.Reader.Close()
		if closeErr != nil {
			return errs.ErrUnavailable
		}
		if !sameTransferArtifact(download.Artifact, current.Artifact) {
			return errs.ErrConflict
		}
		return nil
	})
}

func sameTransferArtifact(left, right entity.Artifact) bool {
	return left.Ref == right.Ref && left.ProjectRef == right.ProjectRef && left.RunRef == right.RunRef &&
		left.SessionRef == right.SessionRef && left.FileName == right.FileName && left.MediaType == right.MediaType &&
		left.Digest == right.Digest && left.SizeBytes == right.SizeBytes && left.Revision == right.Revision &&
		left.Version == right.Version && left.Source == right.Source
}

// Complete следует после полного checksum, закрытия source и свежего owner
// read. Partial chunks до Complete не являются успешно прочитанным файлом.
func sendExecutionArtifact(ctx context.Context, download platformrepo.ArtifactDownload, send func(*cp.StreamExecutionArtifactResponse) error, confirm func() error) error {
	if download.Reader == nil {
		return transportError(errs.ErrUnavailable)
	}
	closed := false
	defer func() {
		if !closed {
			_ = download.Reader.Close()
		}
	}()
	if download.Artifact.SizeBytes < 0 || download.Artifact.SizeBytes > runtimecontract.MaximumArtifactTransferBytes {
		return transportError(errs.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if err := send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Metadata{Metadata: castArtifact(download.Artifact)}}); err != nil {
		return err
	}
	hash := sha256.New()
	buffer := make([]byte, runtimecontract.MaximumArtifactTransferChunkBytes)
	var total int64
	for total < download.Artifact.SizeBytes {
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		length := min(int64(len(buffer)), download.Artifact.SizeBytes-total)
		n, err := io.ReadFull(download.Reader, buffer[:length])
		if err != nil || int64(n) != length {
			return status.Error(codes.DataLoss, artifactTransferContentMismatch)
		}
		_, _ = hash.Write(buffer[:n])
		total += int64(n)
		// SendMsg не гарантирует, что tracing/transport перестали читать slice
		// после возврата, поэтому следующий Read не меняет отправленный chunk.
		chunk := bytes.Clone(buffer[:n])
		if err := send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Chunk{Chunk: chunk}}); err != nil {
			return err
		}
	}
	var extra [1]byte
	if n, err := io.ReadFull(download.Reader, extra[:]); n != 0 || !errors.Is(err, io.EOF) {
		return status.Error(codes.DataLoss, artifactTransferContentMismatch)
	}
	actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if actual != download.Artifact.Digest {
		return status.Error(codes.DataLoss, artifactTransferContentMismatch)
	}
	closeErr := download.Reader.Close()
	closed = true
	if closeErr != nil {
		return transportError(errs.ErrUnavailable)
	}
	if err := confirm(); err != nil {
		return transportError(err)
	}
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	return send(&cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Complete{Complete: &cp.RuntimeArtifactTransferComplete{SizeBytes: total, Digest: actual}}})
}
