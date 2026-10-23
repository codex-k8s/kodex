package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// fileReadReceiptKind identifies safe metadata for one completely verified read_file
	// page, not an acknowledgement that the model provider received or used its content.
	// fileReadReceiptKind обозначает safe metadata одной полностью verified read_file page,
	// не acknowledgement получения/использования content провайдером модели.
	fileReadReceiptKind = "read_file_page"
	// maximumFileReadReceiptBytes bounds the fifteen-field safe receipt to two thousand
	// encoded bytes separately from page, source and public MCP response budgets.
	// maximumFileReadReceiptBytes ограничивает fifteen-field safe receipt двумя тысячами
	// encoded bytes отдельно от page,source и public MCP response budgets.
	maximumFileReadReceiptBytes = 2000
	// maximumFileReadReceiptInteger caps persisted revision/version coordinates at the
	// largest exactly representable JavaScript integer to avoid JSON numeric ambiguity.
	// maximumFileReadReceiptInteger ограничивает persisted revision/version coordinates
	// наибольшим точно представимым JavaScript integer против JSON numeric ambiguity.
	maximumFileReadReceiptInteger int64 = 9007199254740991
)

// fileReadReceipt stores only exact catalog/file/purpose/version/digest/page metadata
// for a verified source page. Names, descriptions, body text, download details and
// transport credentials are excluded; it is not provider delivery acknowledgement.
// fileReadReceipt хранит лишь exact catalog/file/purpose/version/digest/page metadata
// verified source page. Names,descriptions,body text,download details/transport credentials
// исключены; это не provider delivery acknowledgement.
type fileReadReceipt struct {
	Version         int    `json:"version"`
	Kind            string `json:"kind"`
	CatalogRef      string `json:"catalog_ref"`
	CatalogDigest   string `json:"catalog_digest"`
	Purpose         string `json:"purpose"`
	EntryRef        string `json:"entry_ref"`
	ArtifactRef     string `json:"artifact_ref"`
	FileRevision    int64  `json:"file_revision"`
	FileVersion     int64  `json:"file_version"`
	SizeBytes       int64  `json:"size_bytes"`
	OffsetBytes     int64  `json:"offset_bytes"`
	NextOffsetBytes int64  `json:"next_offset_bytes"`
	EOF             bool   `json:"eof"`
	SourceDigest    string `json:"source_digest"`
	ChunkDigest     string `json:"chunk_digest"`
}

// fileReadEvidence keeps private lease/fence/generation provenance alongside the
// validated receipt. It is local trusted-code state, not JSON-reconstructible, signed
// or independently sufficient to authorize a new owner read.
// fileReadEvidence хранит private lease/fence/generation provenance с validated receipt.
// Это local trusted-code state,не JSON-reconstructible,не signed и само
// не авторизует новый owner read.
type fileReadEvidence struct {
	leaseRef   string
	fence      string
	generation int64
	receipt    fileReadReceipt
}

// fileReadToolResult couples the original mutable public wire map to private page
// evidence. Later checks bind selected page/file fields, not every unrelated wire
// property; private evidence is deliberately absent from marshaled public JSON.
// fileReadToolResult связывает исходную mutable public wire map с private page evidence.
// Поздние checks связывают выбранные page/file fields,не каждое постороннее wire
// property; private evidence намеренно отсутствует в public JSON.
type fileReadToolResult struct {
	wire     map[string]any
	evidence *fileReadEvidence
}

// MarshalJSON emits only the unchanged public wire shape, excluding private receipt
// provenance, and propagates encoding errors without claiming a successful audit write.
// MarshalJSON выдаёт лишь неизменную public wire shape без private receipt provenance
// и возвращает encoding errors,не утверждая успешную audit write.
func (result fileReadToolResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(result.wire)
}

// safeFileReadReceipt requires the private typed result and matches current catalog/
// purpose/lease binding, exact request pins, safe integer/size/EOF bounds and selected
// wire file/page fields, including the digest of actual page text. It returns bounded
// whitelisted JSON without body/name/transport details; arbitrary extra wire properties
// are not copied into the receipt, and local success does not prove audit persistence.
// safeFileReadReceipt требует private typed result и сверяет current catalog/purpose/
// lease binding,exact request pins,safe integer/size/EOF bounds и selected wire file/page
// fields с digest actual page text. Возвращает bounded whitelist JSON без body/name/
// transport details; extra wire properties не копируются в receipt,а local success
// не доказывает audit persistence.
func safeFileReadReceipt(input runtimecontract.RunnerInput, arguments map[string]any, result any) (string, error) {
	value, ok := result.(fileReadToolResult)
	if !ok || value.evidence == nil || input.FileCatalog == nil || !onlyKeys(arguments, "purpose", "entry_ref", "artifact_ref", "revision", "digest", "offset_bytes", "maximum_bytes") {
		return "", errRuntimeFileReply
	}
	evidence, receipt := value.evidence, value.evidence.receipt
	purpose, _ := arguments["purpose"].(string)
	if _, valid := runtimeFilePurpose(input, purpose); !valid || receipt.Purpose != purpose ||
		evidence.leaseRef != input.LeaseRef || evidence.fence != input.LeaseFence || evidence.generation != input.LeaseGeneration ||
		receipt.CatalogRef != input.FileCatalog.Ref || receipt.CatalogDigest != input.FileCatalog.Digest ||
		!validFileRef(receipt.CatalogRef, "vfc_") || !validFileRef(receipt.EntryRef, "vfe_") || !validFileRef(receipt.ArtifactRef, "art_") ||
		receipt.Version != 1 || receipt.Kind != fileReadReceiptKind ||
		receipt.FileRevision < 1 || receipt.FileRevision > maximumFileReadReceiptInteger ||
		receipt.FileVersion < 1 || receipt.FileVersion > maximumFileReadReceiptInteger ||
		receipt.SizeBytes < 0 || receipt.SizeBytes > runtimecontract.MaximumArtifactTransferBytes ||
		receipt.OffsetBytes < 0 || receipt.OffsetBytes > receipt.SizeBytes || receipt.NextOffsetBytes < receipt.OffsetBytes ||
		receipt.NextOffsetBytes > receipt.SizeBytes || receipt.NextOffsetBytes-receipt.OffsetBytes > 16384 ||
		receipt.EOF != (receipt.NextOffsetBytes == receipt.SizeBytes) || !receipt.EOF && receipt.NextOffsetBytes == receipt.OffsetBytes ||
		!runtimeFileDigestPattern.MatchString(receipt.SourceDigest) || !runtimeFileDigestPattern.MatchString(receipt.ChunkDigest) {
		return "", errRuntimeFileReply
	}
	if !runtimecontract.ValidSHA256Text(receipt.CatalogDigest) {
		return "", errRuntimeFileReply
	}
	entry, _ := arguments["entry_ref"].(string)
	artifact, _ := arguments["artifact_ref"].(string)
	digest, _ := arguments["digest"].(string)
	revision, revisionOK := fileInteger(arguments, "revision", 0, maximumFileReadReceiptInteger)
	offset, offsetOK := fileReadOffset(arguments)
	maximum, maximumOK := fileInteger(arguments, "maximum_bytes", 16384, 16384)
	if !revisionOK || !offsetOK || !maximumOK || maximum < 4 || receipt.NextOffsetBytes-receipt.OffsetBytes > maximum || receipt.EntryRef != entry || receipt.ArtifactRef != artifact ||
		receipt.FileRevision != revision || receipt.SourceDigest != digest || receipt.OffsetBytes != offset {
		return "", errRuntimeFileReply
	}
	text, textOK := value.wire["text"].(string)
	file, fileOK := value.wire["file"].(map[string]any)
	sum := sha256.Sum256([]byte(text))
	if !textOK || !fileOK || int64(len(text)) != receipt.NextOffsetBytes-receipt.OffsetBytes ||
		receipt.ChunkDigest != "sha256:"+hex.EncodeToString(sum[:]) ||
		value.wire["offset_bytes"] != receipt.OffsetBytes || value.wire["next_offset_bytes"] != receipt.NextOffsetBytes ||
		value.wire["eof"] != receipt.EOF || value.wire["source_digest"] != receipt.SourceDigest || value.wire["chunk_digest"] != receipt.ChunkDigest ||
		file["entry_ref"] != receipt.EntryRef || file["artifact_ref"] != receipt.ArtifactRef || file["revision"] != receipt.FileRevision ||
		file["version"] != receipt.FileVersion || file["size_bytes"] != receipt.SizeBytes || file["digest"] != receipt.SourceDigest || file["purpose"] != receipt.Purpose {
		return "", errRuntimeFileReply
	}
	raw, err := json.Marshal(receipt)
	if err != nil || len(raw) > maximumFileReadReceiptBytes {
		return "", errRuntimeFileReply
	}
	return string(raw), nil
}
