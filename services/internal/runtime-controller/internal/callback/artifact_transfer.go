package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

// errArtifactTransfer is the fixed binding/protocol-integrity failure distinct
// from local spool I/O or propagated owner transport errors.
// errArtifactTransfer — fixed binding/protocol-integrity failure отдельно
// от local spool I/O и возвращаемых owner transport errors.
var errArtifactTransfer = errors.New("runtime artifact transfer binding is invalid")

// artifactTransferPin records exact owner artifact identity, digest, byte count
// and revision, with optional version/name/media checks. It is a locator/commitment,
// not an execution credential or permission grant.
// artifactTransferPin хранит exact owner artifact identity,digest,byte count/revision
// и optional version/name/media checks. Это locator/commitment,не execution
// credential и не permission grant.
type artifactTransferPin struct {
	ref, project, name, media, digest string
	size, revision, version           int64
}

// matches requires identity/project/digest/size/revision equality and checks
// version/name/media only when their pins are nonzero/nonempty. It does not inspect
// the byte stream, scan result, proto unknown fields or current owner eligibility.
// matches требует identity/project/digest/size/revision equality и проверяет version/
// name/media лишь при nonzero/nonempty pins. Byte stream,scan result,proto unknown
// fields и текущая owner eligibility здесь не проверяются.
func (pin artifactTransferPin) matches(artifact *cp.Artifact) bool {
	return artifact != nil && artifact.GetRef() == pin.ref && artifact.GetProjectRef() == pin.project &&
		artifact.GetDigest() == pin.digest && artifact.GetSizeBytes() == pin.size && int64(artifact.GetRevision()) == pin.revision &&
		(pin.version == 0 || artifact.GetVersion() == pin.version) && (pin.name == "" || artifact.GetFileName() == pin.name) &&
		(pin.media == "" || artifact.GetMediaType() == pin.media)
}

// receiveArtifactTransfer validates metadata first, writes bounded nonempty chunks
// only to the caller's private destination and checks full byte count/hash, owner
// Complete and clean EOF in that order before success. Premature EOF, extra frames,
// corruption, cancellation and destination short/error writes fail; partial bytes
// are not rolled back here, so the caller must discard/release a failed private file.
// receiveArtifactTransfer сначала проверяет metadata,пишет bounded nonempty chunks
// лишь в private destination caller и до успеха сверяет full byte count/hash,owner
// Complete и clean EOF в этом порядке. Premature EOF,extra frames,corruption,
// cancellation,short/error writes дают отказ; partial bytes не откатываются здесь,
// поэтому caller обязан discard/release ошибочного private file.
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

// serveArtifactTransfer uses a bounded timeout to obtain a completely verified
// private file before exposing headers or bytes, then reauthorizes the execution
// ticket and sends the exact pinned length/digest. Acquisition/protocol failures
// map to 503/409; a later response-copy failure is logged but cannot undo sent bytes.
// serveArtifactTransfer с bounded timeout получает полностью verified private file
// до выдачи headers/bytes,затем повторно авторизует execution ticket и отправляет
// exact pinned length/digest. Acquisition/protocol failures дают 503/409; поздний
// response-copy failure логируется,но не отменяет уже отправленные bytes.
func (server *Server) serveArtifactTransfer(writer http.ResponseWriter, request *http.Request, input runtimecontract.RunnerInput, pin artifactTransferPin, media string) {
	if server.config.FileTransferTimeout < time.Second || server.config.FileTransferTimeout > runtimecontract.MaximumArtifactTransferDuration {
		http.Error(writer, errArtifactSpool.Error(), http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.config.FileTransferTimeout)
	defer cancel()
	file, release, err := server.verifiedArtifactSpool(ctx, input, pin)
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
	defer release()
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

// verifiedArtifactSpool acquires capacity before opening a lease/fence/generation
// bound owner stream, verifies its full protocol and size, then rewinds the file.
// Every failing path releases descriptor/slot; success transfers that cleanup to
// the returned callback. It does not itself perform the final HTTP ticket recheck.
// verifiedArtifactSpool получает capacity до lease/fence/generation-bound owner stream,
// проверяет full protocol/size и перематывает file. Каждый failing path освобождает
// descriptor/slot; success передаёт cleanup returned callback. Финальная HTTP ticket
// перепроверка здесь не выполняется.
func (server *Server) verifiedArtifactSpool(ctx context.Context, input runtimecontract.RunnerInput, pin artifactTransferPin) (*os.File, func(), error) {
	file, release, err := server.spool.acquire(ctx)
	if err != nil {
		return nil, nil, errArtifactSpool
	}
	success := false
	defer func() {
		if !success {
			release()
		}
	}()
	stream, err := server.control.Runtime.StreamExecutionArtifact(ctx, &cp.StreamExecutionArtifactRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, ArtifactRef: pin.ref,
	}, grpc.MaxCallRecvMsgSize(runtimecontract.MaximumArtifactTransferChunkBytes+(64<<10)))
	if err != nil {
		return nil, nil, err
	}
	if err := receiveArtifactTransfer(ctx, stream.Recv, file, pin); err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() != pin.size {
		return nil, nil, errArtifactSpool
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, nil, errArtifactSpool
	}
	success = true
	return file, release, nil
}

// catalogArtifact validates four single-valued file selectors, canonical revision
// and digest, resolves exact metadata through the lease-bound file catalog, and
// passes its matching pins to verified transfer. Query refs are locators only;
// owner eligibility and final ticket reauthorization remain separate boundaries.
// catalogArtifact проверяет четыре single-valued file selectors,canonical revision
// и digest,получает exact metadata через lease-bound file catalog и передаёт matching
// pins verified transfer. Query refs — лишь locators; owner eligibility
// и final ticket reauthorization остаются отдельными boundaries.
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
