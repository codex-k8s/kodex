package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CopyInputManifest commits normalized bytes and each bounded INPUT slice independently.
// It describes data only; the owner must separately bind the original catalog/revision, actor,
// plan/child and prepared-content receipt before any stage or native admission.
// CopyInputManifest связывает normalized bytes и каждый bounded INPUT slice отдельно.
// Описывает только данные; owner отдельно связывает original catalog/revision, actor,
// plan/child и prepared-content receipt до stage либо native admission.
type CopyInputManifest struct {
	SHA256                string   `json:"sha256"`
	SizeBytes             uint64   `json:"sizeBytes"`
	FrameBytes            uint32   `json:"frameBytes"`
	NormalizationRevision uint32   `json:"normalizationRevision"`
	ChunkSHA256           []string `json:"chunkSHA256"`
}

// BuildCopyInputManifest verifies an owned normalized tar then freezes frame digests without
// changing its bytes. The caller retains ownership and must clear the original content after use.
// BuildCopyInputManifest проверяет owned normalized tar и фиксирует frame digests, не меняя bytes.
// Caller сохраняет ownership и очищает исходный content после использования.
func BuildCopyInputManifest(content CopyArchiveContent) (CopyInputManifest, error) {
	var manifest CopyInputManifest
	if len(content.Bytes) == 0 || len(content.Bytes) > 16<<20 || content.NormalizationRevision != CopyArchiveNormalizationRevision || content.MediaType != "application/x-tar" {
		return manifest, ErrInvalidOperationInput
	}
	sum := sha256.Sum256(content.Bytes)
	if hex.EncodeToString(sum[:]) != content.SHA256 {
		return manifest, ErrInvalidOperationInput
	}
	manifest = CopyInputManifest{SHA256: content.SHA256, SizeBytes: uint64(len(content.Bytes)), FrameBytes: MaximumFrameBytes, NormalizationRevision: content.NormalizationRevision}
	for offset := 0; offset < len(content.Bytes); offset += MaximumFrameBytes {
		end := min(offset+MaximumFrameBytes, len(content.Bytes))
		chunk := sha256.Sum256(content.Bytes[offset:end])
		manifest.ChunkSHA256 = append(manifest.ChunkSHA256, hex.EncodeToString(chunk[:]))
	}
	return manifest, nil
}

// Validate rejects inconsistent counts, unqualified recipes and unbounded metadata before use.
// The final data slice carries EOF in the same owner frame; there is no separate replayable EOF.
// Validate отклоняет inconsistent counts, unqualified recipes и unbounded metadata до использования.
// Последний data slice несёт EOF в том же owner frame; separate replayable EOF отсутствует.
func (manifest CopyInputManifest) Validate() error {
	if !ValidDigest(manifest.SHA256) || manifest.SizeBytes == 0 || manifest.SizeBytes > 16<<20 || manifest.FrameBytes != MaximumFrameBytes || manifest.NormalizationRevision != CopyArchiveNormalizationRevision || uint64(len(manifest.ChunkSHA256)) != (manifest.SizeBytes+MaximumFrameBytes-1)/MaximumFrameBytes {
		return ErrInvalidOperationInput
	}
	for _, digest := range manifest.ChunkSHA256 {
		if !ValidDigest(digest) {
			return ErrInvalidOperationInput
		}
	}
	return nil
}

// Slice returns one zero-based immutable range, marking only its final data slice with EOF.
// The ordinal is owner progress metadata, never a caller-supplied stream sequence or delivery right.
// Slice возвращает zero-based immutable range; EOF отмечает лишь последний data slice.
// Ordinal принадлежит owner progress metadata, не caller stream sequence либо delivery right.
func (manifest CopyInputManifest) Slice(ordinal uint64) (offset, length uint64, digest string, eof bool, err error) {
	if manifest.Validate() != nil || ordinal >= uint64(len(manifest.ChunkSHA256)) {
		err = ErrInvalidOperationInput
		return
	}
	offset = ordinal * uint64(manifest.FrameBytes)
	length = min(uint64(manifest.FrameBytes), manifest.SizeBytes-offset)
	return offset, length, manifest.ChunkSHA256[ordinal], ordinal+1 == uint64(len(manifest.ChunkSHA256)), nil
}

// Digest binds the complete ordered slice recipe; storage coordinates and source authority remain private owner facts.
// Digest связывает complete ordered slice recipe; storage coordinates и source authority остаются private owner facts.
func (manifest CopyInputManifest) Digest() (string, error) {
	if manifest.Validate() != nil {
		return "", ErrInvalidOperationInput
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", ErrInvalidOperationInput
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
