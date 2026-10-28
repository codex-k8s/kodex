package repositorybundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

// TestEncodedBundleRoundTripPreservesFullProject verifies an ordinary-outbox-sized 108-file project as exact UTF8/binary bytes, without inlining them into the model invocation.
// TestEncodedBundleRoundTripPreservesFullProject проверяет outbox-sized проект из108 файлов как exact UTF8/binary bytes без включения их в model invocation.
func TestEncodedBundleRoundTripPreservesFullProject(t *testing.T) {
	files := make([]File, 108)
	for index := range files {
		files[index] = File{Entry: Entry{Path: fmt.Sprintf("source/%03d.go", index), Mode: "100644"}, Bytes: bytes.Repeat([]byte("Код\x00\n"), 300)}
	}
	container, manifest, err := CanonicalContainer(files)
	if err != nil || len(container) > MaximumArchiveBytes {
		t.Fatal("native encoded budget exceeded", err)
	}
	_, decoded, err := Open(container, manifest)
	if err != nil || len(decoded) != len(files) {
		t.Fatal("partial decoded set", err)
	}
	for index, file := range decoded {
		if !bytes.Equal(file.Bytes, files[index].Bytes) {
			t.Fatal("file bytes changed")
		}
	}
}

// TestEncodedBundleRejectsSubstitution exercises duplicate fields, malformed Base64, source substitution, missing/reordered entries and manifest hash replacement before any decoded set is returned.
// TestEncodedBundleRejectsSubstitution проверяет duplicate fields, malformed Base64, source substitution, missing/reordered entries и manifest hash replacement до выдачи decoded set.
func TestEncodedBundleRejectsSubstitution(t *testing.T) {
	container, manifest, err := CanonicalContainer([]File{{Entry: Entry{Path: "a.go", Mode: "100644"}, Bytes: []byte("abc")}, {Entry: Entry{Path: "b.go", Mode: "100644"}, Bytes: []byte("def")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*EncodedBundle){
		func(v *EncodedBundle) { v.Files = v.Files[:1] },
		func(v *EncodedBundle) { v.Files[0], v.Files[1] = v.Files[1], v.Files[0] },
		func(v *EncodedBundle) { v.Files[0].ContentBase64 = "YWJj\n" },
		func(v *EncodedBundle) { v.Files[0].ContentBase64 = "ZGVm" },
		func(v *EncodedBundle) { v.Files[0].Path = "../a.go" },
		func(v *EncodedBundle) { v.Files[0].Mode = "120000" },
		func(v *EncodedBundle) { v.ManifestSHA256 = Digest([]byte("other manifest")) },
		func(v *EncodedBundle) { v.Version = 2 },
	} {
		var value EncodedBundle
		if json.Unmarshal(container, &value) != nil {
			t.Fatal("fixture")
		}
		change(&value)
		raw, _ := json.Marshal(value)
		if _, files, err := Open(raw, manifest); err == nil || files != nil {
			t.Fatal("substituted encoded set admitted")
		}
	}
	for _, raw := range [][]byte{append(bytes.Clone(container), '\n'), bytes.Replace(container, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(container, []byte(`"version":1`), []byte(`"version":1,"scan_state":"CLEAN"`), 1)} {
		if _, _, err := Open(raw, manifest); err == nil {
			t.Fatal("noncanonical or caller-authorized envelope admitted")
		}
	}
}
