package artifacttype

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf16"
)

type formatCase struct {
	mediaType   string
	extension   string
	contentType string
	mainPart    string
}

func TestSupportedFormatsExactMIMEAndExtensionAllowlist(t *testing.T) {
	t.Parallel()
	expected := strings.Fields(`
text/plain=.txt
text/markdown=.md
text/csv=.csv
application/json=.json
application/pdf=.pdf
image/png=.png
image/jpeg=.jpg
image/webp=.webp
image/gif=.gif
application/zip=.zip
application/x-tar=.tar
application/gzip=.gz
application/msword=.doc
application/vnd.ms-excel=.xls
application/vnd.ms-powerpoint=.ppt
application/vnd.oasis.opendocument.text=.odt
application/vnd.oasis.opendocument.text-template=.ott
application/vnd.oasis.opendocument.text-master=.odm
application/vnd.oasis.opendocument.text-master-template=.otm
application/vnd.oasis.opendocument.text-web=.oth
application/vnd.oasis.opendocument.spreadsheet=.ods
application/vnd.oasis.opendocument.spreadsheet-template=.ots
application/vnd.oasis.opendocument.presentation=.odp
application/vnd.oasis.opendocument.presentation-template=.otp
application/vnd.oasis.opendocument.graphics=.odg
application/vnd.oasis.opendocument.graphics-template=.otg
application/vnd.oasis.opendocument.chart=.odc
application/vnd.oasis.opendocument.chart-template=.otc
application/vnd.oasis.opendocument.image=.odi
application/vnd.oasis.opendocument.image-template=.oti
application/vnd.oasis.opendocument.formula=.odf
application/vnd.oasis.opendocument.formula-template=.odft
application/vnd.oasis.opendocument.base=.odb
application/vnd.oasis.opendocument.database=.odb
application/vnd.sun.xml.writer=.sxw
application/vnd.sun.xml.writer.template=.stw
application/vnd.sun.xml.writer.global=.sxg
application/vnd.sun.xml.calc=.sxc
application/vnd.sun.xml.calc.template=.stc
application/vnd.sun.xml.impress=.sxi
application/vnd.sun.xml.impress.template=.sti
application/vnd.sun.xml.draw=.sxd
application/vnd.sun.xml.draw.template=.std
application/vnd.sun.xml.math=.sxm
application/vnd.openxmlformats-officedocument.wordprocessingml.document=.docx
application/vnd.openxmlformats-officedocument.wordprocessingml.template=.dotx
application/vnd.ms-word.document.macroenabled.12=.docm
application/vnd.ms-word.template.macroenabled.12=.dotm
application/vnd.openxmlformats-officedocument.spreadsheetml.sheet=.xlsx
application/vnd.openxmlformats-officedocument.spreadsheetml.template=.xltx
application/vnd.ms-excel.sheet.macroenabled.12=.xlsm
application/vnd.ms-excel.template.macroenabled.12=.xltm
application/vnd.ms-excel.addin.macroenabled.12=.xlam
application/vnd.ms-excel.sheet.binary.macroenabled.12=.xlsb
application/vnd.openxmlformats-officedocument.presentationml.presentation=.pptx
application/vnd.openxmlformats-officedocument.presentationml.template=.potx
application/vnd.openxmlformats-officedocument.presentationml.slideshow=.ppsx
application/vnd.openxmlformats-officedocument.presentationml.slide=.sldx
application/vnd.ms-powerpoint.presentation.macroenabled.12=.pptm
application/vnd.ms-powerpoint.template.macroenabled.12=.potm
application/vnd.ms-powerpoint.slideshow.macroenabled.12=.ppsm
application/vnd.ms-powerpoint.addin.macroenabled.12=.ppam
application/vnd.ms-powerpoint.slide.macroenabled.12=.sldm
`)
	actual := make(map[string]string, len(SupportedFormats()))
	for _, format := range SupportedFormats() {
		actual[format.MediaType] = format.Extension
	}
	if len(actual) != len(expected) {
		t.Fatalf("число форматов = %d, ожидалось %d", len(actual), len(expected))
	}
	for _, pair := range expected {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || actual[parts[0]] != parts[1] {
			t.Errorf("allowlist[%q] = %q, ожидалось %q", parts[0], actual[parts[0]], parts[1])
		}
	}
}

func TestDetectSupportedOpenDocumentAndOpenOfficePackages(t *testing.T) {
	t.Parallel()
	tests := []formatCase{
		{mediaType: "application/vnd.oasis.opendocument.text", extension: ".odt"},
		{mediaType: "application/vnd.oasis.opendocument.text-template", extension: ".ott"},
		{mediaType: "application/vnd.oasis.opendocument.text-master", extension: ".odm"},
		{mediaType: "application/vnd.oasis.opendocument.text-master-template", extension: ".otm"},
		{mediaType: "application/vnd.oasis.opendocument.text-web", extension: ".oth"},
		{mediaType: "application/vnd.oasis.opendocument.spreadsheet", extension: ".ods"},
		{mediaType: "application/vnd.oasis.opendocument.spreadsheet-template", extension: ".ots"},
		{mediaType: "application/vnd.oasis.opendocument.presentation", extension: ".odp"},
		{mediaType: "application/vnd.oasis.opendocument.presentation-template", extension: ".otp"},
		{mediaType: "application/vnd.oasis.opendocument.graphics", extension: ".odg"},
		{mediaType: "application/vnd.oasis.opendocument.graphics-template", extension: ".otg"},
		{mediaType: "application/vnd.oasis.opendocument.chart", extension: ".odc"},
		{mediaType: "application/vnd.oasis.opendocument.chart-template", extension: ".otc"},
		{mediaType: "application/vnd.oasis.opendocument.image", extension: ".odi"},
		{mediaType: "application/vnd.oasis.opendocument.image-template", extension: ".oti"},
		{mediaType: "application/vnd.oasis.opendocument.formula", extension: ".odf"},
		{mediaType: "application/vnd.oasis.opendocument.formula-template", extension: ".odft"},
		{mediaType: "application/vnd.oasis.opendocument.base", extension: ".odb"},
		{mediaType: "application/vnd.oasis.opendocument.database", extension: ".odb"},
		{mediaType: "application/vnd.sun.xml.writer", extension: ".sxw"},
		{mediaType: "application/vnd.sun.xml.writer.template", extension: ".stw"},
		{mediaType: "application/vnd.sun.xml.writer.global", extension: ".sxg"},
		{mediaType: "application/vnd.sun.xml.calc", extension: ".sxc"},
		{mediaType: "application/vnd.sun.xml.calc.template", extension: ".stc"},
		{mediaType: "application/vnd.sun.xml.impress", extension: ".sxi"},
		{mediaType: "application/vnd.sun.xml.impress.template", extension: ".sti"},
		{mediaType: "application/vnd.sun.xml.draw", extension: ".sxd"},
		{mediaType: "application/vnd.sun.xml.draw.template", extension: ".std"},
		{mediaType: "application/vnd.sun.xml.math", extension: ".sxm"},
	}
	for _, test := range tests {
		t.Run(test.extension, func(t *testing.T) {
			body := packageFixture(t, test.mediaType, nil)
			assertDetectedFormat(t, body, test.mediaType, test.extension)
		})
	}
}

func TestDetectSupportedOOXMLPackages(t *testing.T) {
	t.Parallel()
	tests := []formatCase{
		{mediaType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", extension: ".docx", contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", mainPart: "word/document.xml"},
		{mediaType: "application/vnd.openxmlformats-officedocument.wordprocessingml.template", extension: ".dotx", contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml", mainPart: "word/document.xml"},
		{mediaType: "application/vnd.ms-word.document.macroenabled.12", extension: ".docm", contentType: "application/vnd.ms-word.document.macroEnabled.main+xml", mainPart: "word/document.xml"},
		{mediaType: "application/vnd.ms-word.template.macroenabled.12", extension: ".dotm", contentType: "application/vnd.ms-word.template.macroEnabledTemplate.main+xml", mainPart: "word/document.xml"},
		{mediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", extension: ".xlsx", contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml", mainPart: "xl/workbook.xml"},
		{mediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.template", extension: ".xltx", contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.template.main+xml", mainPart: "xl/workbook.xml"},
		{mediaType: "application/vnd.ms-excel.sheet.macroenabled.12", extension: ".xlsm", contentType: "application/vnd.ms-excel.sheet.macroEnabled.main+xml", mainPart: "xl/workbook.xml"},
		{mediaType: "application/vnd.ms-excel.template.macroenabled.12", extension: ".xltm", contentType: "application/vnd.ms-excel.template.macroEnabled.main+xml", mainPart: "xl/workbook.xml"},
		{mediaType: "application/vnd.ms-excel.addin.macroenabled.12", extension: ".xlam", contentType: "application/vnd.ms-excel.addin.macroEnabled.main+xml", mainPart: "xl/workbook.xml"},
		{mediaType: "application/vnd.ms-excel.sheet.binary.macroenabled.12", extension: ".xlsb", contentType: "application/vnd.ms-excel.sheet.binary.macroEnabled.main", mainPart: "xl/workbook.bin"},
		{mediaType: "application/vnd.openxmlformats-officedocument.presentationml.presentation", extension: ".pptx", contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", mainPart: "ppt/presentation.xml"},
		{mediaType: "application/vnd.openxmlformats-officedocument.presentationml.template", extension: ".potx", contentType: "application/vnd.openxmlformats-officedocument.presentationml.template.main+xml", mainPart: "ppt/presentation.xml"},
		{mediaType: "application/vnd.openxmlformats-officedocument.presentationml.slideshow", extension: ".ppsx", contentType: "application/vnd.openxmlformats-officedocument.presentationml.slideshow.main+xml", mainPart: "ppt/presentation.xml"},
		{mediaType: "application/vnd.openxmlformats-officedocument.presentationml.slide", extension: ".sldx", contentType: "application/vnd.openxmlformats-officedocument.presentationml.slide+xml", mainPart: "ppt/slides/slide1.xml"},
		{mediaType: "application/vnd.ms-powerpoint.presentation.macroenabled.12", extension: ".pptm", contentType: "application/vnd.ms-powerpoint.presentation.macroEnabled.main+xml", mainPart: "ppt/presentation.xml"},
		{mediaType: "application/vnd.ms-powerpoint.template.macroenabled.12", extension: ".potm", contentType: "application/vnd.ms-powerpoint.template.macroEnabled.main+xml", mainPart: "ppt/presentation.xml"},
		{mediaType: "application/vnd.ms-powerpoint.slideshow.macroenabled.12", extension: ".ppsm", contentType: "application/vnd.ms-powerpoint.slideshow.macroEnabled.main+xml", mainPart: "ppt/presentation.xml"},
		{mediaType: "application/vnd.ms-powerpoint.addin.macroenabled.12", extension: ".ppam", contentType: "application/vnd.ms-powerpoint.addin.macroEnabled.main+xml", mainPart: "ppt/presentation.xml"},
		{mediaType: "application/vnd.ms-powerpoint.slide.macroenabled.12", extension: ".sldm", contentType: "application/vnd.ms-powerpoint.slide.macroEnabled.main+xml", mainPart: "ppt/slides/slide1.xml"},
	}
	for _, test := range tests {
		t.Run(test.extension, func(t *testing.T) {
			body := ooxmlFixture(t, test.contentType, test.mainPart, nil)
			assertDetectedFormat(t, body, test.mediaType, test.extension)
		})
	}
}

func TestDetectLegacyBinaryMicrosoftOffice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		streamName string
		prefix     []byte
		mediaType  string
		extension  string
	}{
		{name: "word", streamName: "WordDocument", prefix: []byte{0xec, 0xa5, 0xc1, 0x00, 0, 0, 0, 0, 0, 0, 0, 0}, mediaType: "application/msword", extension: ".doc"},
		{name: "excel", streamName: "Workbook", prefix: []byte{0x09, 0x08, 0x10, 0x00, 0x00, 0x06, 0x05, 0x00}, mediaType: "application/vnd.ms-excel", extension: ".xls"},
		{name: "powerpoint", streamName: "PowerPoint Document", prefix: []byte{0x0f, 0x00, 0xe8, 0x03, 0xf8, 0x0f, 0x00, 0x00}, mediaType: "application/vnd.ms-powerpoint", extension: ".ppt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDetectedFormat(t, compoundFixture(t, test.streamName, test.prefix), test.mediaType, test.extension)
		})
	}
}

func TestDetectSupportedArchiveContainers(t *testing.T) {
	t.Parallel()
	zipBody := zipFixture(t, []zipFixtureEntry{{name: "payload.txt", body: []byte("payload")}})
	tarBody := tarFixture(t)
	gzipBody := gzipFixture(t)
	for _, test := range []struct {
		name      string
		body      []byte
		mediaType string
		extension string
	}{
		{name: "zip", body: zipBody, mediaType: "application/zip", extension: ".zip"},
		{name: "tar", body: tarBody, mediaType: "application/x-tar", extension: ".tar"},
		{name: "gzip", body: gzipBody, mediaType: "application/gzip", extension: ".gz"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDetectedFormat(t, test.body, test.mediaType, test.extension)
		})
	}
}

func TestRejectsMalformedAmbiguousAndHostileContainers(t *testing.T) {
	t.Parallel()
	validZIP := zipFixture(t, []zipFixtureEntry{{name: "payload.txt", body: []byte("payload")}})
	validOOXML := ooxmlFixture(t, "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", "word/document.xml", nil)
	validODF := packageFixture(t, "application/vnd.oasis.opendocument.text", nil)
	unknownPackage := packageFixture(t, "application/vnd.example.unsupported", nil)
	zip64Extra := zipFixture(t, []zipFixtureEntry{{name: "payload.txt", body: []byte("payload"), extra: []byte{0x01, 0x00, 0x00, 0x00}}})
	ambiguousCompound := compoundFixture(t, "WordDocument", []byte{0xec, 0xa5, 0xc1, 0x00})
	compoundDirectory := ambiguousCompound[512:1024]
	writeCompoundDirectoryEntry(compoundDirectory[256:384], "PowerPoint Document", 2, 2, 4096)
	binary.LittleEndian.PutUint32(compoundDirectory[128+72:128+76], 2)

	tests := map[string][]byte{
		"truncated zip":           validZIP[:len(validZIP)-1],
		"corrupted zip payload":   corruptFirstZIPPayload(t, validZIP),
		"prefixed zip polyglot":   append([]byte("prefix"), validZIP...),
		"duplicate zip name":      zipFixture(t, []zipFixtureEntry{{name: "same.txt", body: []byte("one")}, {name: "same.txt", body: []byte("two")}}),
		"case-fold duplicate":     zipFixture(t, []zipFixtureEntry{{name: "Same.txt", body: []byte("one")}, {name: "same.txt", body: []byte("two")}}),
		"zip traversal":           zipFixture(t, []zipFixtureEntry{{name: "../escape.txt", body: []byte("payload")}}),
		"too many zip entries":    zipFixture(t, manyZIPEntries(maxContainerEntries+1)),
		"zip compression bomb":    zipFixture(t, []zipFixtureEntry{{name: "payload.txt", body: bytes.Repeat([]byte("x"), 2<<20)}}),
		"zip64 extra":             zip64Extra,
		"unknown package MIME":    unknownPackage,
		"odf padded mimetype":     replaceZIPEntry(t, validODF, "mimetype", []byte("application/vnd.oasis.opendocument.text\n")),
		"odf without manifest":    packageFixture(t, "application/vnd.oasis.opendocument.text", map[string][]byte{"META-INF/manifest.xml": nil}),
		"odf without content":     packageFixture(t, "application/vnd.oasis.opendocument.text", map[string][]byte{"content.xml": nil}),
		"odf manifest mismatch":   packageFixture(t, "application/vnd.oasis.opendocument.text", map[string][]byte{"META-INF/manifest.xml": []byte(packageManifest("application/vnd.oasis.opendocument.spreadsheet"))}),
		"odf manifest namespace":  packageFixture(t, "application/vnd.oasis.opendocument.text", map[string][]byte{"META-INF/manifest.xml": []byte(strings.ReplaceAll(packageManifest("application/vnd.oasis.opendocument.text"), "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0", "urn:example:invalid"))}),
		"ooxml missing main part": ooxmlFixture(t, "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", "word/document.xml", map[string][]byte{"word/document.xml": nil}),
		"ooxml malformed xml":     replaceZIPEntry(t, validOOXML, "[Content_Types].xml", []byte("<Types>")),
		"ooxml missing root rel":  replaceZIPEntry(t, validOOXML, "_rels/.rels", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`)),
		"ambiguous office zip":    appendPackageEntry(t, validODF, "[Content_Types].xml", []byte("<Types/>")),
		"ambiguous compound":      ambiguousCompound,
		"truncated compound":      compoundFixture(t, "WordDocument", []byte{0xec, 0xa5})[:512],
		"truncated gzip":          gzipFixture(t)[:12],
		"truncated tar":           tarFixture(t)[:700],
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if mediaType, err := DetectBytes(body); err == nil {
				t.Fatalf("DetectBytes() = %q, ожидался закрытый отказ", mediaType)
			}
		})
	}
}

func TestRejectsActiveTextAndSignatureOnlySamples(t *testing.T) {
	t.Parallel()
	for name, body := range map[string][]byte{
		"html":          []byte("<!doctype html><script>alert(1)</script>"),
		"xml":           []byte("<?xml version=\"1.0\"?><root/>"),
		"png signature": append([]byte(nil), pngSignature...),
		"pdf prefix":    []byte("%PDF-1.7\n"),
		"webp prefix":   []byte("RIFF\x04\x00\x00\x00WEBP"),
		"zip prefix":    []byte{'P', 'K', 3, 4, 0, 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DetectBytes(body); err == nil {
				t.Fatal("усечённый или активный образец принят")
			}
		})
	}
}

func FuzzDetectNeverPanics(f *testing.F) {
	f.Add([]byte("plain text"))
	f.Add([]byte{'P', 'K', 3, 4})
	f.Add(append([]byte(nil), compoundFileSignature...))
	f.Add([]byte("%PDF-1.7\n%%EOF"))
	f.Fuzz(func(t *testing.T, body []byte) {
		if int64(len(body)) > MaxObjectBytes {
			t.Skip()
		}
		_, _ = DetectBytes(body)
	})
}

func assertDetectedFormat(t *testing.T, body []byte, mediaType string, extension string) {
	t.Helper()
	detected, err := DetectBytes(body)
	if err != nil {
		t.Fatalf("DetectBytes() error = %v", err)
	}
	if detected != mediaType {
		t.Fatalf("DetectBytes() = %q, ожидалось %q", detected, mediaType)
	}
	actualExtension, err := Extension(detected)
	if err != nil || actualExtension != extension {
		t.Fatalf("Extension(%q) = %q, %v; ожидалось %q", detected, actualExtension, err, extension)
	}
}

type zipFixtureEntry struct {
	name   string
	body   []byte
	method uint16
	stored bool
	extra  []byte
}

func zipFixture(t *testing.T, entries []zipFixtureEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		method := entry.method
		if entry.stored {
			method = zip.Store
		} else if method == 0 {
			method = zip.Deflate
		}
		header := &zip.FileHeader{Name: entry.name, Method: method, Extra: entry.extra}
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func packageFixture(t *testing.T, mediaType string, overrides map[string][]byte) []byte {
	t.Helper()
	entries := []zipFixtureEntry{
		{name: "mimetype", body: []byte(mediaType), stored: true},
		{name: "META-INF/manifest.xml", body: []byte(packageManifest(mediaType))},
		{name: "content.xml", body: []byte("<office:document/>")},
	}
	entries = applyZIPOverrides(entries, overrides)
	return zipFixture(t, entries)
}

func packageManifest(mediaType string) string {
	namespace := "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"
	if strings.HasPrefix(mediaType, "application/vnd.sun.xml.") {
		namespace = "http://openoffice.org/2001/manifest"
	}
	return `<?xml version="1.0"?><manifest:manifest xmlns:manifest="` + namespace + `"><manifest:file-entry manifest:full-path="/" manifest:media-type="` + mediaType + `"/></manifest:manifest>`
}

func ooxmlFixture(t *testing.T, contentType string, mainPart string, overrides map[string][]byte) []byte {
	t.Helper()
	contentTypes := `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/` + mainPart + `" ContentType="` + contentType + `"/></Types>`
	entries := []zipFixtureEntry{
		{name: "[Content_Types].xml", body: []byte(contentTypes)},
		{name: "_rels/.rels", body: []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="` + mainPart + `"/></Relationships>`)},
		{name: mainPart, body: []byte("<document/>")},
	}
	entries = applyZIPOverrides(entries, overrides)
	return zipFixture(t, entries)
}

func applyZIPOverrides(entries []zipFixtureEntry, overrides map[string][]byte) []zipFixtureEntry {
	for name, body := range overrides {
		found := false
		for index := range entries {
			if entries[index].name == name {
				found = true
				if body == nil {
					entries = append(entries[:index], entries[index+1:]...)
				} else {
					entries[index].body = body
				}
				break
			}
		}
		if !found && body != nil {
			entries = append(entries, zipFixtureEntry{name: name, body: body})
		}
	}
	return entries
}

func replaceZIPEntry(t *testing.T, body []byte, name string, replacement []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]zipFixtureEntry, 0, len(reader.File))
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == name {
			content = replacement
		}
		entries = append(entries, zipFixtureEntry{name: file.Name, body: content, method: file.Method, stored: file.Method == zip.Store})
	}
	return zipFixture(t, entries)
}

func corruptFirstZIPPayload(t *testing.T, body []byte) []byte {
	t.Helper()
	corrupted := append([]byte(nil), body...)
	if len(corrupted) < 31 || binary.LittleEndian.Uint32(corrupted[:4]) != zipLocalHeaderSignature {
		t.Fatal("ZIP fixture не содержит local header")
	}
	nameLength := int(binary.LittleEndian.Uint16(corrupted[26:28]))
	extraLength := int(binary.LittleEndian.Uint16(corrupted[28:30]))
	payloadOffset := 30 + nameLength + extraLength
	if payloadOffset >= len(corrupted) {
		t.Fatal("ZIP fixture не содержит payload")
	}
	corrupted[payloadOffset] ^= 0xff
	return corrupted
}

func appendPackageEntry(t *testing.T, body []byte, name string, content []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]zipFixtureEntry, 0, len(reader.File)+1)
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		entryBody, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, zipFixtureEntry{name: file.Name, body: entryBody, method: file.Method, stored: file.Method == zip.Store})
	}
	entries = append(entries, zipFixtureEntry{name: name, body: content})
	return zipFixture(t, entries)
}

func manyZIPEntries(count int) []zipFixtureEntry {
	entries := make([]zipFixtureEntry, 0, count)
	for index := 0; index < count; index++ {
		entries = append(entries, zipFixtureEntry{name: fmt.Sprintf("entry-%03d.txt", index), body: []byte("x")})
	}
	return entries
}

func tarFixture(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	body := []byte("payload")
	if err := writer.WriteHeader(&tar.Header{Name: "payload.txt", Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func gzipFixture(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	writer.Name = "payload.txt"
	if _, err := writer.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func compoundFixture(t *testing.T, streamName string, prefix []byte) []byte {
	t.Helper()
	const sectorSize = 512
	const sectorCount = 10
	body := make([]byte, sectorSize*(sectorCount+1))
	header := body[:sectorSize]
	copy(header[:8], compoundFileSignature)
	binary.LittleEndian.PutUint16(header[24:26], 0x003e)
	binary.LittleEndian.PutUint16(header[26:28], 3)
	binary.LittleEndian.PutUint16(header[28:30], 0xfffe)
	binary.LittleEndian.PutUint16(header[30:32], 9)
	binary.LittleEndian.PutUint16(header[32:34], 6)
	binary.LittleEndian.PutUint32(header[44:48], 1)
	binary.LittleEndian.PutUint32(header[48:52], 0)
	binary.LittleEndian.PutUint32(header[56:60], uint32(cfbMiniStreamCut))
	binary.LittleEndian.PutUint32(header[60:64], cfbEndOfChain)
	binary.LittleEndian.PutUint32(header[68:72], cfbEndOfChain)
	for offset := 76; offset < 512; offset += 4 {
		binary.LittleEndian.PutUint32(header[offset:offset+4], cfbFreeSector)
	}
	binary.LittleEndian.PutUint32(header[76:80], 1)

	directory := body[sectorSize : 2*sectorSize]
	writeCompoundDirectoryEntry(directory[:128], "Root Entry", 5, cfbEndOfChain, 0)
	binary.LittleEndian.PutUint32(directory[76:80], 1)
	writeCompoundDirectoryEntry(directory[128:256], streamName, 2, 2, 4096)

	fat := body[2*sectorSize : 3*sectorSize]
	for offset := 0; offset < len(fat); offset += 4 {
		binary.LittleEndian.PutUint32(fat[offset:offset+4], cfbFreeSector)
	}
	binary.LittleEndian.PutUint32(fat[0:4], cfbEndOfChain)
	binary.LittleEndian.PutUint32(fat[4:8], cfbFATSector)
	for sector := 2; sector < 9; sector++ {
		binary.LittleEndian.PutUint32(fat[sector*4:sector*4+4], uint32(sector+1))
	}
	binary.LittleEndian.PutUint32(fat[9*4:9*4+4], cfbEndOfChain)
	copy(body[3*sectorSize:], prefix)
	return body
}

func writeCompoundDirectoryEntry(target []byte, name string, objectType byte, startSector uint32, size uint64) {
	units := append(utf16.Encode([]rune(name)), 0)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(target[index*2:index*2+2], unit)
	}
	binary.LittleEndian.PutUint16(target[64:66], uint16(len(units)*2))
	target[66] = objectType
	target[67] = 1
	for _, offset := range []int{68, 72, 76} {
		binary.LittleEndian.PutUint32(target[offset:offset+4], cfbNoStream)
	}
	binary.LittleEndian.PutUint32(target[116:120], startSector)
	binary.LittleEndian.PutUint64(target[120:128], size)
}
