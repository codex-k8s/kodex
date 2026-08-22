package artifactpolicy

import (
	"archive/zip"
	"bytes"
	"testing"
)

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
