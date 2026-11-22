package managedkubernetes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestCopyInputManifestKeepsExactArchiveAndEOF proves contiguous ranges reconstruct normalized bytes.
// It tests data commitments only; actor/source authority belongs to the owner integration tests.
// TestCopyInputManifestKeepsExactArchiveAndEOF доказывает восстановление normalized bytes из contiguous ranges.
// Проверяет data commitments; actor/source authority принадлежит owner integration tests.
func TestCopyInputManifestKeepsExactArchiveAndEOF(t *testing.T) {
	source := bytes.Repeat([]byte("synthetic-file"), 6000)
	profile := copyArchiveProfileFixture(source, "FILE", "/app/target")
	content, err := NormalizeCopyUpload(profile, source)
	if err != nil {
		t.Fatal("normalize source fixture")
	}
	defer clear(content.Bytes)
	manifest, err := BuildCopyInputManifest(content)
	if err != nil || manifest.Validate() != nil {
		t.Fatal("build source manifest")
	}
	var joined []byte
	for ordinal := uint64(0); ordinal < uint64(len(manifest.ChunkSHA256)); ordinal++ {
		offset, length, digest, eof, err := manifest.Slice(ordinal)
		if err != nil || eof != (ordinal+1 == uint64(len(manifest.ChunkSHA256))) || length == 0 || length > MaximumFrameBytes {
			t.Fatal("invalid source slice")
		}
		part := content.Bytes[offset : offset+length]
		sum := sha256.Sum256(part)
		if hex.EncodeToString(sum[:]) != digest {
			t.Fatal("slice digest changed")
		}
		joined = append(joined, part...)
	}
	if !bytes.Equal(joined, content.Bytes) {
		t.Fatal("source ranges changed normalized content")
	}
	clear(joined)
	if _, _, _, _, err = manifest.Slice(uint64(len(manifest.ChunkSHA256))); err == nil {
		t.Fatal("separate EOF or source ordinal overflow admitted")
	}
	first, err := manifest.Digest()
	if err != nil {
		t.Fatal("manifest digest unavailable")
	}
	manifest.ChunkSHA256[0] = manifest.SHA256
	next, err := manifest.Digest()
	if err != nil || first == next {
		t.Fatal("slice change did not change commitment")
	}
}

// TestCopyInputManifestRejectsUnqualifiedContent rejects partial hashes, caller recipes and count drift.
// TestCopyInputManifestRejectsUnqualifiedContent отклоняет partial hashes, caller recipes и count drift.
func TestCopyInputManifestRejectsUnqualifiedContent(t *testing.T) {
	source := []byte("synthetic")
	content, err := NormalizeCopyUpload(copyArchiveProfileFixture(source, "FILE", "/app/name"), source)
	if err != nil {
		t.Fatal("normalize source fixture")
	}
	defer clear(content.Bytes)
	manifest, err := BuildCopyInputManifest(content)
	if err != nil {
		t.Fatal("build source fixture")
	}
	for _, change := range []func(*CopyInputManifest){func(m *CopyInputManifest) { m.SizeBytes = 0 }, func(m *CopyInputManifest) { m.FrameBytes = 1 }, func(m *CopyInputManifest) { m.NormalizationRevision++ }, func(m *CopyInputManifest) { m.ChunkSHA256 = nil }, func(m *CopyInputManifest) { m.SHA256 = "bad" }} {
		invalid := manifest
		change(&invalid)
		if invalid.Validate() == nil {
			t.Fatal("unqualified source manifest admitted")
		}
	}
	content.Bytes[0] ^= 1
	if _, err = BuildCopyInputManifest(content); err == nil {
		t.Fatal("changed normalized bytes admitted")
	}
}
