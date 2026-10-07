package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"os"
	"time"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// Операция оставляет 15 секунд существующего MCP-бюджета для terminal audit
// и ответа. Она не продлевает lease и не вводит новый транспортный timeout.
const maximumFileReadDuration = 45 * time.Second

func (server *Server) readFile(ctx context.Context, input runtimecontract.RunnerInput, purpose cp.RuntimeFilePurpose, arguments map[string]any) (map[string]any, error) {
	if !onlyKeys(arguments, "purpose", "entry_ref", "artifact_ref", "revision", "digest", "offset_bytes", "maximum_bytes") {
		return nil, errRuntimeFileInput
	}
	entry, _ := arguments["entry_ref"].(string)
	artifact, _ := arguments["artifact_ref"].(string)
	digest, _ := arguments["digest"].(string)
	revision, valid := fileInteger(arguments, "revision", 0, 9007199254740991)
	maximum, maximumOK := fileInteger(arguments, "maximum_bytes", 16384, 16384)
	offset, offsetOK := fileReadOffset(arguments)
	if !valid || !maximumOK || maximum < utf8.UTFMax || !offsetOK || !validFileRef(entry, "vfe_") || !validFileRef(artifact, "art_") || !runtimeFileDigestPattern.MatchString(digest) {
		return nil, errRuntimeFileInput
	}
	if server.config.FileTransferTimeout < time.Second || server.config.FileTransferTimeout > runtimecontract.MaximumArtifactTransferDuration {
		return nil, errArtifactSpool
	}
	deadline := time.Now().Add(maximumFileReadDuration)
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Add(-server.config.RequestTimeout).Before(deadline) {
		deadline = parentDeadline.Add(-server.config.RequestTimeout)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	exact := &cp.ExecutionFileRef{EntryRef: entry, ArtifactRef: artifact, Revision: revision, Digest: digest}
	execution := &cp.ExecutionFileContext{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		CatalogRef: input.FileCatalog.Ref, CatalogDigest: input.FileCatalog.Digest, Purpose: purpose}
	metadata, result, err := server.readFileMetadata(ctx, input, purpose, execution, exact)
	if err != nil {
		return nil, err
	}
	if metadata.GetSizeBytes() > runtimecontract.MaximumArtifactTransferBytes || offset > metadata.GetSizeBytes() {
		return nil, errRuntimeFileInput
	}
	pin := artifactTransferPin{ref: artifact, project: metadata.GetProjectRef(), name: metadata.GetName(), media: metadata.GetMediaType(), digest: digest,
		size: metadata.GetSizeBytes(), revision: revision, version: metadata.GetVersion()}
	transferContext, stopTransfer := context.WithTimeout(ctx, server.config.FileTransferTimeout)
	file, release, err := server.verifiedArtifactSpool(transferContext, input, pin)
	stopTransfer()
	if err != nil {
		return nil, err
	}
	defer release()
	// Проверяется весь текст, включая байты до offset и после выдаваемой страницы.
	if err := validateFileReadText(ctx, file, pin.size); err != nil {
		return nil, err
	}
	page, err := fileReadPage(file, pin.size, offset, maximum)
	if err != nil {
		return nil, err
	}
	// После долгого stream/UTF-8 обхода заново проверяется именно catalog/purpose
	// и exact descriptor. Последующий terminal audit ещё раз проверяет lease.
	current, _, err := server.readFileMetadata(ctx, input, purpose, execution, exact)
	if err != nil {
		return nil, err
	}
	if !proto.Equal(metadata, current) {
		return nil, errRuntimeFileReply
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(page)
	next := offset + int64(len(page))
	result["text"], result["offset_bytes"], result["next_offset_bytes"] = string(page), offset, next
	result["eof"], result["chunk_digest"], result["source_digest"] = next == pin.size, "sha256:"+hex.EncodeToString(sum[:]), digest
	return result, nil
}

func fileReadOffset(arguments map[string]any) (int64, bool) {
	raw, present := arguments["offset_bytes"]
	if !present {
		return 0, true
	}
	number, ok := raw.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > float64(runtimecontract.MaximumArtifactTransferBytes) || math.Trunc(number) != number {
		return 0, false
	}
	return int64(number), true
}

func (server *Server) readFileMetadata(ctx context.Context, input runtimecontract.RunnerInput, purpose cp.RuntimeFilePurpose, execution *cp.ExecutionFileContext, exact *cp.ExecutionFileRef) (*cp.ExecutionFileDescriptor, map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.GetExecutionFileMetadata(ctx, &cp.GetExecutionFileMetadataRequest{Context: execution, File: exact}, grpc.MaxCallRecvMsgSize(maximumFileToolReplyBytes))
	if err != nil {
		return nil, nil, err
	}
	result, err := exactFileResult(input, purpose, response.GetCatalog(), response.GetFile(), exact)
	if err != nil {
		return nil, nil, err
	}
	return response.GetFile(), result, nil
}

func validateFileReadText(ctx context.Context, file *os.File, size int64) error {
	reader := io.NewSectionReader(file, 0, size)
	buffer := make([]byte, runtimecontract.MaximumArtifactTransferChunkBytes+utf8.UTFMax-1)
	carry := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := reader.Read(buffer[carry : runtimecontract.MaximumArtifactTransferChunkBytes+carry])
		if err != nil && err != io.EOF {
			return errArtifactSpool
		}
		part := buffer[:carry+n]
		if bytes.IndexByte(part, 0) >= 0 {
			return errRuntimeFileReply
		}
		position := 0
		for position < len(part) && utf8.FullRune(part[position:]) {
			runeValue, length := utf8.DecodeRune(part[position:])
			if runeValue == utf8.RuneError && length == 1 {
				return errRuntimeFileReply
			}
			position += length
		}
		carry = copy(buffer, part[position:])
		if err == io.EOF {
			if carry != 0 {
				return errRuntimeFileReply
			}
			return nil
		}
	}
}

func fileReadPage(file *os.File, size, offset, maximum int64) ([]byte, error) {
	page := make([]byte, min(maximum, size-offset))
	if len(page) == 0 {
		return page, nil
	}
	if _, err := io.ReadFull(io.NewSectionReader(file, offset, int64(len(page))), page); err != nil {
		return nil, errArtifactSpool
	}
	if !utf8.RuneStart(page[0]) {
		return nil, errRuntimeFileInput
	}
	position := 0
	for position < len(page) && utf8.FullRune(page[position:]) {
		_, length := utf8.DecodeRune(page[position:])
		position += length
	}
	if position == 0 || !utf8.Valid(page[:position]) {
		return nil, errRuntimeFileReply
	}
	return page[:position], nil
}
