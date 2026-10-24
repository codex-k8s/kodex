package artifactpolicy

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

// TestInspect checks representative Markdown, invalid JSON and executable inputs produce the
// expected scan and preview pair.
// TestInspect проверяет ожидаемую пару scan и preview для типичных Markdown, неверного JSON и
// исполняемого ввода.
func TestInspect(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, fileName, mediaType string
		body                      []byte
		state, preview            string
	}{
		{name: "markdown", fileName: "brief.md", mediaType: "application/octet-stream", body: []byte("# Brief\n"), state: ScanClean, preview: PreviewAvailable},
		{name: "invalid json", fileName: "input.json", mediaType: "application/json", body: []byte("{"), state: ScanFailed, preview: PreviewUnavailable},
		{name: "executable", fileName: "tool.exe", mediaType: "application/octet-stream", body: []byte("MZpayload"), state: ScanQuarantined, preview: PreviewBlocked},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			verdict := Inspect(test.fileName, test.mediaType, test.body)
			if verdict.ScanState != test.state || verdict.PreviewState != test.preview {
				t.Fatalf("unexpected verdict: %#v", verdict)
			}
		})
	}
}

// TestInspectReaderStreamsTextLargerThanLegacyLimit checks a synthetic seventeen-MiB text section
// passes reader inspection beyond the legacy limit without constructing that full body fixture.
// TestInspectReaderStreamsTextLargerThanLegacyLimit проверяет допуск синтетической текстовой
// секции семнадцать MiB за прежним лимитом без построения полного body оснастки.
func TestInspectReaderStreamsTextLargerThanLegacyLimit(t *testing.T) {
	t.Parallel()
	const size = int64(17 << 20)
	reader := io.NewSectionReader(repeatedByteReaderAt{size: size, value: 'a'}, 0, size)
	verdict, err := InspectReader("notes.txt", "text/plain", reader, size)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.ScanState != ScanClean || verdict.PreviewState != PreviewAvailable {
		t.Fatalf("unexpected verdict: %#v", verdict)
	}
}

// TestInspectReaderQuarantinesDeclaredOrNamedActiveContent checks an active declared MIME or
// filename extension quarantines an otherwise plain-text body and blocks preview.
// TestInspectReaderQuarantinesDeclaredOrNamedActiveContent проверяет карантин и блокировку preview
// обычного текстового body при active declared MIME либо расширении имени.
func TestInspectReaderQuarantinesDeclaredOrNamedActiveContent(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, fileName, mediaType string
	}{
		{name: "declared", fileName: "page.txt", mediaType: "text/html"},
		{name: "extension", fileName: "page.html", mediaType: "text/plain"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			body := []byte("plain text")
			verdict, err := InspectReader(test.fileName, test.mediaType, bytes.NewReader(body), int64(len(body)))
			if err != nil {
				t.Fatal(err)
			}
			if verdict.ScanState != ScanQuarantined || verdict.PreviewState != PreviewBlocked {
				t.Fatalf("active content was not quarantined: %#v", verdict)
			}
		})
	}
}

// TestInspectOfficeDocumentAndMacro checks bounded Office content-type metadata yields the exact
// document MIME while an added VBA member causes quarantine.
// TestInspectOfficeDocumentAndMacro проверяет точный document MIME по ограниченным Office
// content-type метаданным и карантин при добавленном VBA элементе.
func TestInspectOfficeDocumentAndMacro(t *testing.T) {
	t.Parallel()
	document := officeArchive(t, false)
	verdict := Inspect("proposal.docx", "application/octet-stream", document)
	if verdict.ScanState != ScanClean || verdict.MediaType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("unexpected document verdict: %#v", verdict)
	}
	macro := officeArchive(t, true)
	verdict = Inspect("proposal.docm", "application/octet-stream", macro)
	if verdict.ScanState != ScanQuarantined {
		t.Fatalf("macro archive was not quarantined: %#v", verdict)
	}
}

// officeArchive builds a minimal synthetic Office-like ZIP containing content-type metadata and an
// optional macro-named member for inspection tests.
// officeArchive создаёт минимальный синтетический Office-подобный ZIP с content-type метаданными и
// необязательным элементом с macro именем для тестов inspection.
func officeArchive(t *testing.T, macro bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	part, err := writer.Create("[Content_Types].xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(`<Types><Override ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`))
	if macro {
		part, err = writer.Create("word/vbaProject.bin")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("macro"))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// repeatedByteReaderAt represents a bounded synthetic file of repeated bytes for streaming tests
// without allocating its full declared size.
// repeatedByteReaderAt представляет ограниченный синтетический файл повторяющихся байт для
// streaming тестов без выделения памяти на полный объявленный размер.
type repeatedByteReaderAt struct {
	size  int64
	value byte
}

// ReadAt fills the requested range with a fixed synthetic byte and returns EOF on end or a short
// read according to the fixture size.
// ReadAt заполняет запрошенный диапазон фиксированным синтетическим байтом и возвращает EOF на
// конце либо коротком чтении по размеру оснастки.
func (reader repeatedByteReaderAt) ReadAt(target []byte, offset int64) (int, error) {
	if offset >= reader.size {
		return 0, io.EOF
	}
	count := len(target)
	if remaining := reader.size - offset; int64(count) > remaining {
		count = int(remaining)
	}
	for index := 0; index < count; index++ {
		target[index] = reader.value
	}
	if count != len(target) {
		return count, io.EOF
	}
	return count, nil
}
