// Package artifactpolicy проверяет содержимое artifact до сохранения и выдачи.
package artifactpolicy

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	ScanClean                    = "CLEAN"
	ScanQuarantined              = "QUARANTINED"
	ScanFailed                   = "FAILED"
	PreviewAvailable             = "AVAILABLE"
	PreviewUnavailable           = "UNAVAILABLE"
	PreviewBlocked               = "BLOCKED"
	maximumArchiveEntries        = 512
	maximumArchiveBytes   uint64 = 64 << 20
)

// Verdict — server-owned результат синхронной встроенной проверки файла.
type Verdict struct {
	MediaType, ScanState, PreviewState string
}

// Inspect определяет канонический media type по содержимому и закрыто
// отклоняет исполняемые, активные и неподдерживаемые форматы.
func Inspect(fileName, declaredMediaType string, body []byte) Verdict {
	detected := http.DetectContentType(body)
	extension := strings.ToLower(filepath.Ext(fileName))
	if isExecutable(body) {
		return Verdict{MediaType: "application/octet-stream", ScanState: ScanQuarantined, PreviewState: PreviewBlocked}
	}
	if bytes.HasPrefix(body, []byte("PK\x03\x04")) {
		return inspectOfficeArchive(body)
	}
	switch detected {
	case "application/pdf":
		return Verdict{MediaType: detected, ScanState: ScanClean, PreviewState: PreviewUnavailable}
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return Verdict{MediaType: detected, ScanState: ScanClean, PreviewState: PreviewAvailable}
	case "text/html", "image/svg+xml", "application/xhtml+xml":
		return Verdict{MediaType: detected, ScanState: ScanQuarantined, PreviewState: PreviewBlocked}
	}
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return Verdict{MediaType: canonicalFallback(declaredMediaType), ScanState: ScanFailed, PreviewState: PreviewUnavailable}
	}
	mediaType := textMediaType(extension, declaredMediaType)
	if mediaType == "application/json" && !json.Valid(body) {
		return Verdict{MediaType: mediaType, ScanState: ScanFailed, PreviewState: PreviewUnavailable}
	}
	if detected == "text/plain; charset=utf-8" || len(body) == 0 || mediaType != "application/octet-stream" {
		return Verdict{MediaType: mediaType, ScanState: ScanClean, PreviewState: PreviewAvailable}
	}
	return Verdict{MediaType: "application/octet-stream", ScanState: ScanFailed, PreviewState: PreviewUnavailable}
}

func canonicalFallback(declared string) string {
	mediaType, _, err := mime.ParseMediaType(declared)
	if err != nil || mediaType == "" {
		return "application/octet-stream"
	}
	return strings.ToLower(mediaType)
}

func textMediaType(extension, declared string) string {
	switch extension {
	case ".json":
		return "application/json"
	case ".md", ".markdown":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".txt", ".log":
		return "text/plain"
	}
	mediaType := canonicalFallback(declared)
	if strings.HasPrefix(mediaType, "text/") || mediaType == "application/json" {
		return mediaType
	}
	return "text/plain"
}

func isExecutable(body []byte) bool {
	return bytes.HasPrefix(body, []byte("MZ")) ||
		bytes.HasPrefix(body, []byte("\x7fELF")) ||
		bytes.HasPrefix(body, []byte("#!"))
}

func inspectOfficeArchive(body []byte) Verdict {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(reader.File) == 0 || len(reader.File) > maximumArchiveEntries {
		return Verdict{MediaType: "application/zip", ScanState: ScanQuarantined, PreviewState: PreviewBlocked}
	}
	var total uint64
	var contentTypes []byte
	for _, file := range reader.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		cleaned := path.Clean(name)
		lower := strings.ToLower(cleaned)
		if cleaned == "." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) || dangerousArchiveEntry(lower) {
			return Verdict{MediaType: "application/zip", ScanState: ScanQuarantined, PreviewState: PreviewBlocked}
		}
		if file.UncompressedSize64 > maximumArchiveBytes || total > maximumArchiveBytes-file.UncompressedSize64 {
			return Verdict{MediaType: "application/zip", ScanState: ScanQuarantined, PreviewState: PreviewBlocked}
		}
		total += file.UncompressedSize64
		if lower == "[content_types].xml" {
			contentTypes, err = readBounded(file, 256<<10)
			if err != nil {
				return Verdict{MediaType: "application/zip", ScanState: ScanQuarantined, PreviewState: PreviewBlocked}
			}
		}
	}
	mediaType := officeMediaType(contentTypes)
	if mediaType == "" {
		return Verdict{MediaType: "application/zip", ScanState: ScanFailed, PreviewState: PreviewUnavailable}
	}
	return Verdict{MediaType: mediaType, ScanState: ScanClean, PreviewState: PreviewUnavailable}
}

func dangerousArchiveEntry(name string) bool {
	for _, suffix := range []string{"vbaproject.bin", ".exe", ".dll", ".com", ".js", ".vbs", ".ps1", ".sh", ".bat", ".cmd"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func readBounded(file *zip.File, limit int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(content)) > limit {
		return nil, io.ErrUnexpectedEOF
	}
	return content, nil
}

func officeMediaType(contentTypes []byte) string {
	content := strings.ToLower(string(contentTypes))
	switch {
	case strings.Contains(content, "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"):
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case strings.Contains(content, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"):
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case strings.Contains(content, "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"):
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	default:
		return ""
	}
}
