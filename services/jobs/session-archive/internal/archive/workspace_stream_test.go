package archive

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestWorkspaceCanonicalStreamPreservesExecutableAndExactBytes reads the actual emitted TAR.
// It proves the codec's mode and digest behavior, not stopped-writer or remote object custody.
// TestWorkspaceCanonicalStreamPreservesExecutableAndExactBytes читает настоящий emitted TAR.
// Проверяется codec mode/digest, не stopped-writer или remote object custody.
func TestWorkspaceCanonicalStreamPreservesExecutableAndExactBytes(t *testing.T) {
	var buffer bytes.Buffer
	encoder, err := newWorkspaceStreamEncoder(t.Context(), &buffer, runtimecontract.RuntimeWorkspacePolicyV1())
	if err != nil {
		t.Fatal(err)
	}
	if encoder.appendEntry("src", true, 0o750, 0, nil) != nil ||
		encoder.appendEntry("src/start.sh", false, 0o755, 10, strings.NewReader("#!/bin/sh\n")) != nil {
		t.Fatal("canonical entries rejected")
	}
	digest, dataBytes, entries, streamBytes, err := encoder.finish()
	sum := sha256.Sum256(buffer.Bytes())
	if err != nil || digest != hex.EncodeToString(sum[:]) || dataBytes != 10 || entries != 2 || streamBytes != int64(buffer.Len()) {
		t.Fatal("canonical stream lost exact aggregate commitment")
	}
	reader := tar.NewReader(bytes.NewReader(buffer.Bytes()))
	directory, err := reader.Next()
	if err != nil || directory.Name != "src/" || directory.Typeflag != tar.TypeDir || directory.Mode != 0o750 || directory.Uid != 0 || directory.Gid != 0 {
		t.Fatal("directory metadata was not canonicalized")
	}
	file, err := reader.Next()
	if err != nil || file.Name != "src/start.sh" || file.Typeflag != tar.TypeReg || file.Mode != 0o755 {
		t.Fatal("executable Git/work source lost its execute bits")
	}
	raw, err := io.ReadAll(reader)
	if err != nil || string(raw) != "#!/bin/sh\n" {
		t.Fatal("source bytes changed in the canonical stream")
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatal("unexpected source metadata appeared in the stream")
	}
}

// TestWorkspaceCanonicalPAXPathBound uses a 4096-byte non-ASCII multicomponent path.
// It verifies actual TAR padding against the shared conservative overhead, without filesystem claims.
// TestWorkspaceCanonicalPAXPathBound использует non-ASCII multicomponent path длиной4096 bytes.
// Проверяется настоящий TAR padding против общего overhead, без утверждений о filesystem acceptance.
func TestWorkspaceCanonicalPAXPathBound(t *testing.T) {
	name := strings.Repeat(strings.Repeat("я", 120)+"/", 16) + strings.Repeat("я", 120)
	if len(name) != runtimecontract.MaximumWorkspaceArchivePathBytes {
		t.Fatal("long-path fixture is not the exact byte boundary")
	}
	var buffer bytes.Buffer
	encoder, err := newWorkspaceStreamEncoder(t.Context(), &buffer, runtimecontract.RuntimeWorkspacePolicyV1())
	if err != nil || encoder.appendEntry(name, false, 0o640, 1, strings.NewReader("x")) != nil {
		t.Fatal("bounded canonical PAX path rejected")
	}
	_, size, entries, written, err := encoder.finish()
	bound, boundErr := runtimecontract.MaximumWorkspaceStreamBytes(size, entries)
	if err != nil || boundErr != nil || written > bound {
		t.Fatal("PAX record or padding escaped the unchanged part-count bound")
	}
	reader := tar.NewReader(bytes.NewReader(buffer.Bytes()))
	header, err := reader.Next()
	if err != nil || header.Name != name || header.PAXRecords["path"] != name {
		t.Fatal("non-ASCII long path changed on the actual TAR wire")
	}
	var second bytes.Buffer
	invalid, err := newWorkspaceStreamEncoder(t.Context(), &second, runtimecontract.RuntimeWorkspacePolicyV1())
	if err != nil || invalid.appendEntry(name+"x", false, 0o640, 0, strings.NewReader("")) == nil {
		t.Fatal("4097-byte path bypassed the closed boundary")
	}
	if _, _, _, _, err := invalid.finish(); err == nil {
		t.Fatal("failed entry became an accepted empty checkpoint")
	}
}

// TestWorkspaceCanonicalStreamRejectsUnsafeAndPartialEntries forbids traversal and incomplete input.
// One refused entry poisons the complete stream; callers cannot silently omit failed source files.
// TestWorkspaceCanonicalStreamRejectsUnsafeAndPartialEntries запрещает traversal и неполный input.
// Отказ entry закрывает весь stream: caller не может молча пропустить сломанный исходник.
func TestWorkspaceCanonicalStreamRejectsUnsafeAndPartialEntries(t *testing.T) {
	for _, name := range []string{"..", "../escape", "/absolute", "a/../escape", "a\\escape", "a\nfile", strings.Repeat("x", 256)} {
		var buffer bytes.Buffer
		encoder, err := newWorkspaceStreamEncoder(t.Context(), &buffer, runtimecontract.RuntimeWorkspacePolicyV1())
		if err != nil || encoder.appendEntry(name, false, 0o640, 1, strings.NewReader("x")) == nil {
			t.Fatal("invalid path accepted")
		}
		if _, _, _, _, err := encoder.finish(); err == nil {
			t.Fatal("refused path became a successful incomplete checkpoint")
		}
	}
	for _, payload := range []string{"", "xx"} {
		encoder, err := newWorkspaceStreamEncoder(t.Context(), io.Discard, runtimecontract.RuntimeWorkspacePolicyV1())
		if err != nil || encoder.appendEntry("one", false, 0o640, 1, strings.NewReader(payload)) == nil {
			t.Fatal("short or longer source bypassed exact size binding")
		}
	}
	encoder, err := newWorkspaceStreamEncoder(t.Context(), io.Discard, runtimecontract.RuntimeWorkspacePolicyV1())
	if err != nil || encoder.appendEntry("same", false, 0o600, 0, strings.NewReader("")) != nil ||
		encoder.appendEntry("same", false, 0o600, 0, strings.NewReader("")) == nil {
		t.Fatal("duplicate source name accepted")
	}
}
