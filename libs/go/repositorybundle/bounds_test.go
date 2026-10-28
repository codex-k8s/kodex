package repositorybundle

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// compressedBoundFixture constructs a complete single-file ZIP and exact manifest without using admission or bypassing their consumer limits.
// compressedBoundFixture строит полный single-file ZIP и exact manifest без admission либо обхода consumer limits.
func compressedBoundFixture(t *testing.T, size int) ([]byte, []byte) {
	t.Helper()
	body := bytes.Repeat([]byte("a"), size)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	member, err := writer.Create("source.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = member.Write(body); err != nil {
		t.Fatal(err)
	}
	if writer.Close() != nil {
		t.Fatal("close fixture")
	}
	raw, err := json.Marshal(Manifest{Version: 1, TotalBytes: int64(size), Files: []Entry{{Path: "source.txt", Mode: "100644", Bytes: int64(size), SHA256: Digest(body)}}})
	if err != nil {
		t.Fatal(err)
	}
	return output.Bytes(), raw
}

// TestBundleExactDecodedAndFileCountBounds proves the independent decoded and count ceilings at their exact positive boundaries and rejects one-byte/one-member excess without returning partial content.
// TestBundleExactDecodedAndFileCountBounds доказывает независимые decoded/count ceilings на exact positive boundary и отказ при одном лишнем byte/member без partial content.
func TestBundleExactDecodedAndFileCountBounds(t *testing.T) {
	for _, size := range []int{MaximumContentBytes, MaximumContentBytes + 1} {
		archive, manifest := compressedBoundFixture(t, size)
		_, files, err := Open(archive, manifest)
		if size == MaximumContentBytes {
			if err != nil || len(files) != 1 || len(files[0].Bytes) != size {
				t.Fatal("exact decoded bound rejected", err)
			}
		} else if err == nil || files != nil {
			t.Fatal("decoded excess admitted")
		}
	}
	files := make([]File, MaximumFiles)
	for i := range files {
		files[i] = File{Entry: Entry{Path: fmt.Sprintf("source/%03d.txt", i), Mode: "100644"}, Bytes: []byte{}}
	}
	archive, manifest, err := CanonicalContainer(files)
	if err != nil {
		t.Fatal("exact file count rejected", err)
	}
	_, decoded, err := Open(archive, manifest)
	if err != nil || len(decoded) != MaximumFiles {
		t.Fatal("exact file count incomplete", err)
	}
	value, err := ParseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	value.Files = append(value.Files, Entry{Path: "source/999.txt", Mode: "100644", SHA256: Digest(nil)})
	oversized, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = Open(archive, oversized); err == nil {
		t.Fatal("257-file manifest admitted")
	}
}

// TestBundleExactEncodedBound uses a canonical valid envelope at exactly1MiB and another at1MiB+1 with the same allowed decoded bytes; size rejection cannot be explained by malformed Base64, manifest or trailing JSON.
// TestBundleExactEncodedBound использует canonical valid envelope ровно1MiB и1MiB+1 с теми же разрешёнными decoded bytes; отказ не объясняется malformed Base64, manifest либо trailing JSON.
func TestBundleExactEncodedBound(t *testing.T) {
	for padding := range 4 {
		path := "source" + strings.Repeat("x", padding) + ".txt"
		empty, _, err := CanonicalContainer([]File{{Entry: Entry{Path: path, Mode: "100644"}, Bytes: []byte{}}})
		if err != nil {
			t.Fatal(err)
		}
		available := MaximumArchiveBytes - len(empty)
		if available%4 != 0 {
			continue
		}
		body := bytes.Repeat([]byte("a"), available/4*3)
		files := []File{{Entry: Entry{Path: path, Mode: "100644"}, Bytes: body}}
		archive, manifest, err := CanonicalContainer(files)
		if err != nil || len(archive) != MaximumArchiveBytes {
			t.Fatal("exact encoded fixture", err)
		}
		if _, _, err = Open(archive, manifest); err != nil {
			t.Fatal("exact encoded limit rejected", err)
		}
		files[0].Entry.Path += "x"
		manifest, err = CanonicalManifest(files)
		if err != nil {
			t.Fatal(err)
		}
		tooLarge, err := json.Marshal(EncodedBundle{Version: 1, ManifestSHA256: Digest(manifest), Files: []EncodedFile{{Path: files[0].Entry.Path, Mode: "100644", ContentBase64: base64.StdEncoding.EncodeToString(body)}}})
		if err != nil || len(tooLarge) != MaximumArchiveBytes+1 {
			t.Fatal("exact excess fixture", err)
		}
		if _, decoded, err := Open(tooLarge, manifest); err == nil || decoded != nil {
			t.Fatal("encoded excess admitted despite allowed decoded size")
		}
		return
	}
	t.Fatal("failed to align canonical encoded boundary")
}
