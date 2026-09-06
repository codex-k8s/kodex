package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

var errArtifactTransfer = errors.New("runtime artifact transfer binding is invalid")

type artifactTransferPin struct {
	ref, project, name, media, digest string
	size, revision, version           int64
}

func (pin artifactTransferPin) matches(artifact *cp.Artifact) bool {
	return artifact != nil && artifact.GetRef() == pin.ref && artifact.GetProjectRef() == pin.project &&
		artifact.GetDigest() == pin.digest && artifact.GetSizeBytes() == pin.size && int64(artifact.GetRevision()) == pin.revision &&
		(pin.version == 0 || artifact.GetVersion() == pin.version) && (pin.name == "" || artifact.GetFileName() == pin.name) &&
		(pin.media == "" || artifact.GetMediaType() == pin.media)
}

// Partial bytes принадлежат только приватному spool. Успех требует metadata,
// checksum всего тела, owner Complete и clean EOF в этом порядке.
func receiveArtifactTransfer(ctx context.Context, recv func() (*cp.StreamExecutionArtifactResponse, error), destination io.Writer, pin artifactTransferPin) error {
	if pin.size < 0 || pin.size > runtimecontract.MaximumArtifactTransferBytes || !runtimeFileDigestPattern.MatchString(pin.digest) {
		return errArtifactTransfer
	}
	frame, err := recv()
	if err != nil {
		return err
	}
	if !pin.matches(frame.GetMetadata()) {
		return errArtifactTransfer
	}
	hash := sha256.New()
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err = recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errArtifactTransfer
			}
			return err
		}
		switch part := frame.GetPart().(type) {
		case *cp.StreamExecutionArtifactResponse_Chunk:
			if len(part.Chunk) < 1 || len(part.Chunk) > runtimecontract.MaximumArtifactTransferChunkBytes || int64(len(part.Chunk)) > pin.size-total {
				return errArtifactTransfer
			}
			n, err := destination.Write(part.Chunk)
			if err != nil || n != len(part.Chunk) {
				return errArtifactSpool
			}
			_, _ = hash.Write(part.Chunk)
			total += int64(n)
		case *cp.StreamExecutionArtifactResponse_Complete:
			if part.Complete == nil || total != pin.size || part.Complete.GetSizeBytes() != pin.size || part.Complete.GetDigest() != pin.digest ||
				"sha256:"+hex.EncodeToString(hash.Sum(nil)) != pin.digest {
				return errArtifactTransfer
			}
			if _, err := recv(); !errors.Is(err, io.EOF) {
				return errArtifactTransfer
			}
			return ctx.Err()
		default:
			return errArtifactTransfer
		}
	}
}

func (server *Server) serveArtifactTransfer(writer http.ResponseWriter, request *http.Request, input runtimecontract.RunnerInput, pin artifactTransferPin, media string) {
	if server.config.FileTransferTimeout < time.Second || server.config.FileTransferTimeout > runtimecontract.MaximumArtifactTransferDuration {
		http.Error(writer, errArtifactSpool.Error(), http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.config.FileTransferTimeout)
	defer cancel()
	file, release, err := server.spool.acquire(ctx)
	if err != nil {
		http.Error(writer, errArtifactSpool.Error(), http.StatusServiceUnavailable)
		return
	}
	defer release()
	stream, err := server.control.Runtime.StreamExecutionArtifact(ctx, &cp.StreamExecutionArtifactRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, ArtifactRef: pin.ref,
	}, grpc.MaxCallRecvMsgSize(runtimecontract.MaximumArtifactTransferChunkBytes+(64<<10)))
	if err != nil {
		writeControlError(writer, err)
		return
	}
	err = receiveArtifactTransfer(ctx, stream.Recv, file, pin)
	if err != nil {
		if errors.Is(err, errArtifactTransfer) {
			http.Error(writer, errArtifactTransfer.Error(), http.StatusConflict)
		} else if errors.Is(err, errArtifactSpool) {
			http.Error(writer, errArtifactSpool.Error(), http.StatusServiceUnavailable)
		} else {
			writeControlError(writer, err)
		}
		return
	}
	info, err := file.Stat()
	if err != nil || info.Size() != pin.size {
		http.Error(writer, errArtifactSpool.Error(), http.StatusServiceUnavailable)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(writer, errArtifactSpool.Error(), http.StatusServiceUnavailable)
		return
	}
	if _, ok := server.authorize(request, input.LeaseRef); !ok {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", media)
	writer.Header().Set("Content-Length", strconv.FormatInt(pin.size, 10))
	writer.Header().Set("X-Kodex-Artifact-Digest", pin.digest)
	writer.WriteHeader(http.StatusOK)
	if _, err := io.CopyN(writer, file, pin.size); err != nil && server.logger != nil {
		server.logger.WarnContext(request.Context(), "runtime artifact response delivery failed", "error_class", "transport")
	}
}

func (server *Server) catalogArtifact(writer http.ResponseWriter, request *http.Request, input runtimecontract.RunnerInput, ref string) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(request.URL.RawQuery) > 1024 || len(query) != 4 || !validFileRef(ref, "art_") {
		http.NotFound(writer, request)
		return
	}
	for _, key := range []string{"purpose", "entry_ref", "revision", "digest"} {
		if len(query[key]) != 1 || query.Get(key) == "" {
			http.NotFound(writer, request)
			return
		}
	}
	purpose, ok := runtimeFilePurpose(input, query.Get("purpose"))
	revision, revisionErr := strconv.ParseInt(query.Get("revision"), 10, 64)
	if !ok || revisionErr != nil || revision < 1 || strconv.FormatInt(revision, 10) != query.Get("revision") ||
		!validFileRef(query.Get("entry_ref"), "vfe_") || !runtimeFileDigestPattern.MatchString(query.Get("digest")) {
		http.NotFound(writer, request)
		return
	}
	exact := &cp.ExecutionFileRef{EntryRef: query.Get("entry_ref"), ArtifactRef: ref, Revision: revision, Digest: query.Get("digest")}
	ctx, cancel := context.WithTimeout(request.Context(), server.config.RequestTimeout)
	response, err := server.control.Runtime.GetExecutionFileMetadata(ctx, &cp.GetExecutionFileMetadataRequest{Context: &cp.ExecutionFileContext{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, CatalogRef: input.FileCatalog.Ref, CatalogDigest: input.FileCatalog.Digest, Purpose: purpose,
	}, File: exact}, grpc.MaxCallRecvMsgSize(maximumFileToolReplyBytes))
	cancel()
	if err != nil {
		writeControlError(writer, err)
		return
	}
	if _, err := exactFileResult(input, purpose, response.GetCatalog(), response.GetFile(), exact); err != nil {
		http.Error(writer, errArtifactTransfer.Error(), http.StatusConflict)
		return
	}
	file := response.GetFile()
	pin := artifactTransferPin{ref: ref, project: file.GetProjectRef(), name: file.GetName(), media: file.GetMediaType(), digest: file.GetDigest(),
		size: file.GetSizeBytes(), revision: file.GetRevision(), version: file.GetVersion()}
	server.serveArtifactTransfer(writer, request, input, pin, file.GetMediaType())
}
