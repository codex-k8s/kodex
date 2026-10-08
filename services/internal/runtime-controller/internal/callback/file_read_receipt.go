package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	fileReadReceiptKind                 = "read_file_page"
	maximumFileReadReceiptBytes         = 2000
	maximumFileReadReceiptInteger int64 = 9007199254740991
)

// Квитанция описывает проверенную страницу, но не ACK её получения provider.
// Поля закрыты whitelist; file/source descriptions и содержимое не сохраняются.
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

type fileReadEvidence struct {
	leaseRef   string
	fence      string
	generation int64
	receipt    fileReadReceipt
}

type fileReadToolResult struct {
	wire     map[string]any
	evidence *fileReadEvidence
}

// Сохраняется точная прежняя MCP форма; private evidence не входит в ответ.
func (result fileReadToolResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(result.wire)
}

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
	catalogDigest, err := hex.DecodeString(receipt.CatalogDigest)
	if err != nil || len(catalogDigest) != sha256.Size || hex.EncodeToString(catalogDigest) != receipt.CatalogDigest {
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
