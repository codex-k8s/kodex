package managedkubernetes

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"
)

// copyArchiveTestEntry defines synthetic nonsecret bytes and headers for real standard-library tar IO.
// copyArchiveTestEntry задаёт synthetic nonsecret bytes/headers для actual standard-library tar IO.
type copyArchiveTestEntry struct {
	header tar.Header
	body   string
}

// makeCopyArchiveFixture creates actual tar wire bytes without replacing the production parser.
// makeCopyArchiveFixture создаёт actual tar wire bytes без подмены production parser.
func makeCopyArchiveFixture(t testing.TB, entries ...copyArchiveTestEntry) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := tar.NewWriter(&body)
	for _, entry := range entries {
		header := entry.header
		if header.Mode == 0 {
			header.Mode = 0600
		}
		if header.Typeflag == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal("write synthetic copy archive header", err)
		}
		if _, err := writer.Write([]byte(entry.body)); err != nil {
			t.Fatal("write synthetic copy archive bytes", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal("finish synthetic copy archive", err)
	}
	return body.Bytes()
}

// copyArchiveProfileFixture binds a finite copy recipe to the exact supplied source digest.
// copyArchiveProfileFixture связывает finite copy recipe с exact supplied source digest.
func copyArchiveProfileFixture(source []byte, format, remote string) CopyProfile {
	sum := sha256.Sum256(source)
	return CopyProfile{ArtifactRef: "art_source_revision", ArtifactSHA256: hex.EncodeToString(sum[:]), TransferFormat: format, RemotePath: remote, MaximumBytes: 1 << 20, TimeoutSeconds: 60}
}

// TestNormalizeCopyFILEUsesExactOwnedBytes verifies deterministic packaging and strict single-file
// download semantics, including empty content, immutable hash and no borrowed output alias.
// TestNormalizeCopyFILEUsesExactOwnedBytes проверяет deterministic packaging/strict single-file
// download, включая empty content, immutable hash и отсутствие borrowed output alias.
func TestNormalizeCopyFILEUsesExactOwnedBytes(t *testing.T) {
	raw := []byte("synthetic exact file")
	profile := copyArchiveProfileFixture(raw, "FILE", "/app/file with spaces.txt")
	bad := profile
	bad.ArtifactSHA256 = strings.Repeat("b", 64)
	if _, err := NormalizeCopyUpload(bad, raw); err == nil {
		t.Fatal("source bytes were adopted under another approved artifact hash")
	}
	upload, err := NormalizeCopyUpload(profile, raw)
	if err != nil {
		t.Fatal("valid FILE source rejected", err)
	}
	header, err := tar.NewReader(bytes.NewReader(upload.Bytes)).Next()
	if err != nil || header.Name != "file with spaces.txt" || header.Typeflag != tar.TypeReg || header.Uid != 0 || header.Gid != 0 || header.Mode != 0600 {
		t.Fatal("FILE wrapper changed destination or forwarded ownership", err)
	}
	again, err := NormalizeCopyUpload(profile, raw)
	if err != nil || !bytes.Equal(again.Bytes, upload.Bytes) || again.SHA256 != upload.SHA256 {
		t.Fatal("canonical FILE wrapper is unstable", err)
	}
	clear(raw)
	profile.ArtifactRef, profile.ArtifactSHA256 = "", ""
	result, err := NormalizeCopyDownload(profile, upload.Bytes)
	if err != nil || string(result.Bytes) != "synthetic exact file" || result.MediaType != "application/octet-stream" || result.Files != 1 {
		t.Fatal("FILE download did not recover exact single content", err)
	}
	received := makeCopyArchiveFixture(t, copyArchiveTestEntry{header: tar.Header{Name: "./file with spaces.txt", Typeflag: tar.TypeReg}})
	result, err = NormalizeCopyDownload(profile, received)
	if err != nil || len(result.Bytes) != 0 || result.Files != 1 {
		t.Fatal("empty regular file rejected", err)
	}
}

// TestNormalizeCopyTARClosesUnsafeMembers exercises links, traversal, duplicates, file-parent
// collisions, unknown PAX metadata, special modes, sparse/special entries and hidden trailing bytes.
// TestNormalizeCopyTARClosesUnsafeMembers проверяет links/traversal/duplicates/file-parent
// collisions, unknown PAX metadata, special modes/entries и hidden trailing bytes.
func TestNormalizeCopyTARClosesUnsafeMembers(t *testing.T) {
	normal := copyArchiveTestEntry{header: tar.Header{Name: "app/file", Typeflag: tar.TypeReg}, body: "safe"}
	for _, entries := range [][]copyArchiveTestEntry{
		{{header: tar.Header{Name: "../outside", Typeflag: tar.TypeReg}, body: "x"}},
		{{header: tar.Header{Name: "/absolute", Typeflag: tar.TypeReg}, body: "x"}},
		{{header: tar.Header{Name: "C:/drive", Typeflag: tar.TypeReg}, body: "x"}},
		{{header: tar.Header{Name: "app\\outside", Typeflag: tar.TypeReg}, body: "x"}},
		{{header: tar.Header{Name: "app/link", Typeflag: tar.TypeSymlink, Linkname: "outside"}}},
		{{header: tar.Header{Name: "app/link", Typeflag: tar.TypeLink, Linkname: "app/file"}}},
		{{header: tar.Header{Name: "app/fifo", Typeflag: tar.TypeFifo}}},
		{{header: tar.Header{Name: "app/device", Typeflag: tar.TypeChar}}},
		{{header: tar.Header{Name: "app/setuid", Typeflag: tar.TypeReg, Mode: 04755}, body: "x"}},
		{normal, {header: tar.Header{Name: "./app/file", Typeflag: tar.TypeReg}, body: "duplicate"}},
		{{header: tar.Header{Name: "app", Typeflag: tar.TypeReg}, body: "parent"}, normal},
		{normal, {header: tar.Header{Name: "app", Typeflag: tar.TypeReg}, body: "parent"}},
		{{header: tar.Header{Name: "app/xattr", Typeflag: tar.TypeReg, PAXRecords: map[string]string{"SCHILY.xattr.security.capability": "opaque"}}, body: "x"}},
	} {
		source := makeCopyArchiveFixture(t, entries...)
		profile := copyArchiveProfileFixture(source, "TAR", "/destination")
		if _, err := NormalizeCopyUpload(profile, source); err == nil {
			t.Fatal("unsafe archive member accepted")
		}
	}
	source := makeCopyArchiveFixture(t, normal)
	for _, damaged := range [][]byte{source[:len(source)-1024], append(bytes.Clone(source), bytes.Repeat([]byte{1}, 512)...), append(bytes.Clone(source), source...)} {
		profile := copyArchiveProfileFixture(damaged, "TAR", "/destination")
		if _, err := NormalizeCopyUpload(profile, damaged); err == nil {
			t.Fatal("truncated/hidden trailing archive accepted")
		}
	}
}

// TestNormalizeCopyTARPreservesSafeTreeAndBudget checks complete safe members, normalization of
// producer metadata, requested download root and actual wire overhead before any remote effect.
// TestNormalizeCopyTARPreservesSafeTreeAndBudget проверяет complete safe members/normalization
// producer metadata, requested download root и wire overhead до remote effect.
func TestNormalizeCopyTARPreservesSafeTreeAndBudget(t *testing.T) {
	source := makeCopyArchiveFixture(t, copyArchiveTestEntry{header: tar.Header{Name: "./app/", Typeflag: tar.TypeDir, Mode: 0755, Uid: 1000, Gid: 1000}}, copyArchiveTestEntry{header: tar.Header{Name: "./app/long-" + strings.Repeat("x", 120), Typeflag: tar.TypeReg, Mode: 0755, Uid: 2000, Gid: 2000, Format: tar.FormatPAX}, body: "script"})
	profile := copyArchiveProfileFixture(source, "TAR", "/destination")
	result, err := NormalizeCopyUpload(profile, source)
	if err != nil || result.Files != 1 || result.Directories != 1 {
		t.Fatal("safe archive rejected", err)
	}
	reader := tar.NewReader(bytes.NewReader(result.Bytes))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || header.Uid != 0 || header.Gid != 0 || header.ModTime.Unix() != 0 || header.Linkname != "" {
			t.Fatal("archive metadata was not normalized", err)
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			t.Fatal(err)
		}
	}
	profile.ArtifactRef, profile.ArtifactSHA256, profile.RemotePath = "", "", "/source/app"
	if _, err := NormalizeCopyDownload(profile, result.Bytes); err != nil {
		t.Fatal("valid requested TAR root rejected", err)
	}
	profile.RemotePath = "/source/other"
	if _, err := NormalizeCopyDownload(profile, result.Bytes); err == nil {
		t.Fatal("foreign archive root accepted")
	}
	raw := []byte("raw")
	small := copyArchiveProfileFixture(raw, "FILE", "/app/name")
	small.MaximumBytes = uint64(len(raw))
	if _, err := NormalizeCopyUpload(small, raw); err == nil {
		t.Fatal("tar overhead silently widened native wire budget")
	}
	for _, name := range []string{strings.Repeat("a", 4097), "app/../file", "app//file"} {
		if _, err := copyArchiveName(name, false); err == nil {
			t.Fatal("archive path limit/alias accepted")
		}
	}
}

// TestNormalizeCopyArchiveHeaderCountAndFILECardinality permits every header within the declared
// wire budget while preserving exact single-file output before owner publication.
// TestNormalizeCopyArchiveHeaderCountAndFILECardinality допускает все headers в declared wire
// budget, сохраняя exact single-file output до owner publication.
func TestNormalizeCopyArchiveHeaderCountAndFILECardinality(t *testing.T) {
	entries := make([]copyArchiveTestEntry, 4097)
	for i := range entries {
		entries[i] = copyArchiveTestEntry{header: tar.Header{Name: fmt.Sprintf("file-%d", i), Typeflag: tar.TypeReg}}
	}
	source := makeCopyArchiveFixture(t, entries...)
	profile := copyArchiveProfileFixture(source, "TAR", "/destination")
	profile.MaximumBytes = 16 << 20
	if result, err := NormalizeCopyUpload(profile, source); err != nil || result.Files != 4097 {
		t.Fatal("valid declared byte budget stopped at a hidden header limit", err)
	}
	source = makeCopyArchiveFixture(t, copyArchiveTestEntry{header: tar.Header{Name: "./name", Typeflag: tar.TypeReg}, body: "one"}, copyArchiveTestEntry{header: tar.Header{Name: "other", Typeflag: tar.TypeReg}, body: "two"})
	profile = CopyProfile{TransferFormat: "FILE", RemotePath: "/app/name", MaximumBytes: 1 << 20, TimeoutSeconds: 60}
	if _, err := NormalizeCopyDownload(profile, source); err == nil {
		t.Fatal("multi-file archive was published as one FILE")
	}
}

// BenchmarkNormalizeCopyTARDeepSharedPrefix measures the owner wire budget with long shared
// names, avoiding the old ancestor-map expansion. The prepared fixture is outside the timed run.
// BenchmarkNormalizeCopyTARDeepSharedPrefix измеряет owner wire budget с long shared names,
// исключая прежнее ancestor-map expansion. Fixture готовится вне измеряемого запуска.
func BenchmarkNormalizeCopyTARDeepSharedPrefix(b *testing.B) {
	entries := make([]copyArchiveTestEntry, 2500)
	prefix := strings.Repeat("a/", 1800)
	for index := range entries {
		entries[index] = copyArchiveTestEntry{header: tar.Header{Name: prefix + fmt.Sprintf("file-%04d", index), Typeflag: tar.TypeReg}}
	}
	source := makeCopyArchiveFixture(b, entries...)
	profile := copyArchiveProfileFixture(source, "TAR", "/destination")
	profile.MaximumBytes = 16 << 20
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := NormalizeCopyUpload(profile, source)
		if err != nil || result.Files != 2500 {
			b.Fatal("bounded deep source rejected", err)
		}
		clear(result.Bytes)
	}
}

// BenchmarkNormalizeCopyTARMaximumShortMembers measures the maximum physical header count
// within the existing sixteen-MiB wire budget, independently from the long-prefix fixture.
// BenchmarkNormalizeCopyTARMaximumShortMembers измеряет максимальное physical header count
// в existing sixteen-MiB wire budget независимо от long-prefix fixture.
func BenchmarkNormalizeCopyTARMaximumShortMembers(b *testing.B) {
	entries := make([]copyArchiveTestEntry, (16<<20)/512-2)
	for index := range entries {
		entries[index] = copyArchiveTestEntry{header: tar.Header{Name: fmt.Sprintf("f%05d", index), Typeflag: tar.TypeReg}}
	}
	source := makeCopyArchiveFixture(b, entries...)
	profile := copyArchiveProfileFixture(source, "TAR", "/destination")
	profile.MaximumBytes = 16 << 20
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := NormalizeCopyUpload(profile, source)
		if err != nil || result.Files != uint32(len(entries)) {
			b.Fatal("physical wire-budget member count rejected", err)
		}
		clear(result.Bytes)
	}
}

// TestNormalizeCopyTARNameWorkUsesWireBudget covers a depth above64 and a file-parent collision
// hidden by an intervening lexical name; neither requires a large payload fixture.
// TestNormalizeCopyTARNameWorkUsesWireBudget проверяет depth выше64 и file-parent collision
// с intervening lexical name; большой payload fixture не требуется.
func TestNormalizeCopyTARNameWorkUsesWireBudget(t *testing.T) {
	source := makeCopyArchiveFixture(t, copyArchiveTestEntry{header: tar.Header{Name: strings.Repeat("a/", 80) + "file", Typeflag: tar.TypeReg}})
	profile := copyArchiveProfileFixture(source, "TAR", "/destination")
	result, err := NormalizeCopyUpload(profile, source)
	if err != nil || result.Files != 1 {
		t.Fatal("byte-bounded deep path rejected", err)
	}
	clear(result.Bytes)
	source = makeCopyArchiveFixture(t, copyArchiveTestEntry{header: tar.Header{Name: "a", Typeflag: tar.TypeReg}}, copyArchiveTestEntry{header: tar.Header{Name: "a-lexical", Typeflag: tar.TypeReg}}, copyArchiveTestEntry{header: tar.Header{Name: "a/child", Typeflag: tar.TypeReg}})
	profile = copyArchiveProfileFixture(source, "TAR", "/destination")
	if _, err := NormalizeCopyUpload(profile, source); err == nil {
		t.Fatal("file-parent collision crossed sorted gap")
	}
}
