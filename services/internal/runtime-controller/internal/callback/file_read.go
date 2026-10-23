package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// maximumFileReadDuration caps the full-source read stage at forty-five seconds;
// the actual deadline also reserves config.RequestTimeout before any parent deadline.
// It neither renews the lease nor changes the enclosing MCP transport timeout.
// maximumFileReadDuration ограничивает full-source read сорока пятью секундами;
// фактический deadline также оставляет config.RequestTimeout до parent deadline.
// Lease не продлевается,enclosing MCP transport timeout не меняется.
const maximumFileReadDuration = 45 * time.Second

// readFile validates exact pins and byte coordinates, reserves terminal-audit time,
// reads current owner metadata, then verifies the entire source in private spool and
// scans all bytes for UTF-8/NUL before slicing a rune-safe page. A second exact metadata
// read and equality check precede private lease-bound page evidence. Every acquired
// file is released; no page or success evidence escapes a failed mandatory stage.
// readFile проверяет exact pins/byte coordinates,оставляет время terminal audit,
// читает current owner metadata,проверяет весь source в private spool и все bytes
// на UTF-8/NUL до rune-safe page slicing. Второй exact metadata read/equality check
// предшествует private lease-bound page evidence. Каждый acquired file освобождается;
// при failed mandatory stage page/success evidence не выдаются.
func (server *Server) readFile(ctx context.Context, input runtimecontract.RunnerInput, purpose cp.RuntimeFilePurpose, arguments map[string]any) (any, error) {
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
	// Доказательство создаётся только после полного source и повторной authority
	// проверки; JSON ответа или аргументов не может восстановить private evidence.
	return fileReadToolResult{wire: result, evidence: &fileReadEvidence{
		leaseRef: input.LeaseRef, fence: input.LeaseFence, generation: input.LeaseGeneration,
		receipt: fileReadReceipt{Version: 1, Kind: fileReadReceiptKind,
			CatalogRef: input.FileCatalog.Ref, CatalogDigest: input.FileCatalog.Digest,
			Purpose: strings.TrimPrefix(purpose.String(), "RUNTIME_FILE_PURPOSE_"), EntryRef: metadata.GetEntryRef(), ArtifactRef: metadata.GetArtifactRef(),
			FileRevision: metadata.GetRevision(), FileVersion: metadata.GetVersion(), SizeBytes: pin.size,
			OffsetBytes: offset, NextOffsetBytes: next, EOF: next == pin.size,
			SourceDigest: digest, ChunkDigest: "sha256:" + hex.EncodeToString(sum[:])},
	}}, nil
}

// fileReadOffset permits omitted offset as zero or finite nonnegative integral
// float64 bounded by transfer size policy; actual source EOF/rune alignment is checked later.
// fileReadOffset допускает omitted offset как zero либо finite nonnegative integral
// float64 до transfer size policy; actual source EOF/rune alignment проверяются далее.
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

// readFileMetadata performs one bounded generated owner RPC using supplied exact
// execution/file pins, then returns the owner descriptor only after catalog/descriptor
// and selected-pin validation. Current owner authority is enforced by that RPC.
// readFileMetadata выполняет один bounded generated owner RPC с exact execution/file
// pins,возвращая owner descriptor лишь после catalog/descriptor/selected-pin проверки.
// Текущую owner authority обеспечивает RPC.
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

// validateFileReadText scans the complete declared private-file interval in bounded
// chunks, carrying incomplete UTF-8 runes between reads and rejecting NUL/invalid/truncated
// encoding with cancellation checks. It relies on prior verified spool size and does
// not rewind the descriptor, normalize text or provide antivirus/semantic analysis.
// validateFileReadText сканирует весь declared private-file interval bounded chunks,
// перенося incomplete UTF-8 runes и отвергая NUL/invalid/truncated encoding с cancellation
// checks. Опирается на ранее verified spool size,не перематывает descriptor,
// не нормализует text и не выполняет antivirus/semantic analysis.
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

// fileReadPage requires previously checked size/offset/maximum and a verified UTF-8
// source. It reads at most the remaining bounded interval, rejects a mid-rune starting
// offset and trims an incomplete trailing rune; exact EOF returns an empty page.
// It does not hash or validate bytes outside this already-validated source interval.
// fileReadPage требует ранее проверенные size/offset/maximum и verified UTF-8 source.
// Читает bounded remaining interval,отвергает mid-rune start,убирает incomplete
// trailing rune; exact EOF даёт empty page. Bytes вне этого ранее validated source
// interval не хешируются и не проверяются.
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
