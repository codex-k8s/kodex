package repositorybundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// bundleFixture builds only an in-memory archive and canonical metadata; no path is written or source executed.
// bundleFixture строит лишь in-memory archive и canonical metadata; paths не записываются и source не исполняется.
func bundleFixture(t *testing.T, count int) ([]byte, []byte, []File) {
	t.Helper()
	files := make([]File, 0, count)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for index := range count {
		file := File{Entry: Entry{Path: fmt.Sprintf("src/file-%03d.txt", index), Mode: "100644"}, Bytes: []byte(strings.Repeat("content-Я\x00\n", 230))}
		entry, err := writer.Create(file.Entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(file.Bytes); err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	if writer.Close() != nil {
		t.Fatal("close synthetic ZIP")
	}
	manifest, err := CanonicalManifest(files)
	if err != nil {
		t.Fatal(err)
	}
	return output.Bytes(), manifest, files
}

// TestBundleFullProject verifies the real project-sized multi-file shape and preserves UTF8, binary bytes, exact count and every digest.
// TestBundleFullProject проверяет multi-file shape размера настоящего проекта и сохраняет UTF8, binary bytes, exact count и каждый digest.
func TestBundleFullProject(t *testing.T) {
	archive, raw, originals := bundleFixture(t, 108)
	manifest, files, err := Open(archive, raw)
	if err != nil || len(files) != 108 || len(manifest.Files) != 108 || manifest.TotalBytes < 291000 {
		t.Fatalf("full project rejected: %v", err)
	}
	for index, file := range files {
		if !bytes.Equal(file.Bytes, originals[index].Bytes) || file.Entry.SHA256 != Digest(originals[index].Bytes) {
			t.Fatal("bundle changed source bytes or omitted a member")
		}
	}
}

// TestManifestClosedShape rejects canonical/digest/path/size ambiguity, including duplicate JSON keys and partial totals.
// TestManifestClosedShape отклоняет canonical/digest/path/size ambiguity, включая duplicate JSON keys и неполные totals.
func TestManifestClosedShape(t *testing.T) {
	_, raw, _ := bundleFixture(t, 2)
	for _, invalid := range [][]byte{append(append([]byte(nil), raw...), []byte(" {}")...), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":2`), 1), []byte(`null`), []byte(`[]`), append([]byte(" "), raw...)} {
		if _, err := ParseManifest(invalid); err == nil {
			t.Fatal("ambiguous or unsupported manifest accepted")
		}
	}
	for _, invalid := range []string{"../x", "/x", "x//y", ".git/config", "a\\b", "x/.env", "x/private.key", "a\x00b", "x/../b", "a/ ", "C:x", "src/a\u202E.go", ".git\u200C/config"} {
		if ValidPath(invalid) {
			t.Fatal("unsafe archive path accepted")
		}
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	manifest.TotalBytes++
	changed, _ := json.Marshal(manifest)
	if _, err := ParseManifest(changed); err == nil {
		t.Fatal("partial or dishonest total accepted")
	}
}

// TestArchiveExactSet fails closed on missing/extra/digest-substituted members and unsupported links without extracting files.
// TestArchiveExactSet закрыто отклоняет missing/extra/digest-substituted members и links без извлечения files.
func TestArchiveExactSet(t *testing.T) {
	archive, raw, _ := bundleFixture(t, 2)
	shortArchive, shorter, shortFiles := bundleFixture(t, 1)
	if _, _, err := Open(archive, shorter); err == nil {
		t.Fatal("extra archive member accepted")
	}
	if _, _, err := Open(shortArchive, raw); err == nil {
		t.Fatal("missing archive member accepted")
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Files[0].SHA256 = strings.Repeat("0", 64)
	changed, _ := json.Marshal(manifest)
	if _, _, err := Open(archive, changed); err == nil {
		t.Fatal("substituted member digest accepted")
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	header := &zip.FileHeader{Name: "src/file-000.txt", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0777)
	member, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write(shortFiles[0].Bytes); err != nil {
		t.Fatal(err)
	}
	if writer.Close() != nil {
		t.Fatal("close link ZIP")
	}
	if _, _, err := Open(output.Bytes(), shorter); err == nil {
		t.Fatal("archive link accepted")
	}
}
