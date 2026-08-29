// Package artifactpolicy проверяет содержимое artifact до сохранения и выдачи.
package artifactpolicy

import (
	"archive/zip"
	"bufio"
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
	maximumArchiveBytes   uint64 = 512 << 20
	maximumJSONBytes             = 8 << 20
	contentDetectionBytes        = 512
)

// Verdict — server-owned результат синхронной встроенной проверки файла.
type Verdict struct {
	MediaType, ScanState, PreviewState string
}

type Reader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

// Inspect определяет канонический media type по содержимому и закрыто
// отклоняет исполняемые, активные и неподдерживаемые форматы.
func Inspect(fileName, declaredMediaType string, body []byte) Verdict {
	verdict, err := InspectReader(fileName, declaredMediaType, bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return Verdict{MediaType: canonicalFallback(declaredMediaType), ScanState: ScanFailed, PreviewState: PreviewUnavailable}
	}
	return verdict
}

// InspectReader выполняет bounded file-backed inspection. Он читает файл
// последовательно либо через ReaderAt и не материализует в RAM полный body.
func InspectReader(fileName, declaredMediaType string, reader Reader, sizeBytes int64) (Verdict, error) {
	if reader == nil || sizeBytes < 0 {
		return Verdict{}, io.ErrUnexpectedEOF
	}
	head := make([]byte, contentDetectionBytes)
	count, err := reader.ReadAt(head, 0)
	if err != nil && err != io.EOF {
		return Verdict{}, err
	}
	head = head[:count]
	detected := http.DetectContentType(head)
	extension := strings.ToLower(filepath.Ext(fileName))
	declared := canonicalFallback(declaredMediaType)
	if isExecutable(head) {
		return Verdict{MediaType: "application/octet-stream", ScanState: ScanQuarantined, PreviewState: PreviewBlocked}, nil
	}
	if isActiveContent(extension, detected, declared) {
		return Verdict{MediaType: activeMediaType(extension, detected, declared), ScanState: ScanQuarantined, PreviewState: PreviewBlocked}, nil
	}
	if bytes.HasPrefix(head, []byte("PK\x03\x04")) {
		return inspectOfficeArchive(reader, sizeBytes), nil
	}
	switch detected {
	case "application/pdf":
		return Verdict{MediaType: detected, ScanState: ScanClean, PreviewState: PreviewUnavailable}, nil
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return Verdict{MediaType: detected, ScanState: ScanClean, PreviewState: PreviewAvailable}, nil
	}
	mediaType := textMediaType(extension, declaredMediaType)
	if detected == "text/plain; charset=utf-8" || sizeBytes == 0 || mediaType != "application/octet-stream" {
		valid, validationErr := validText(reader, sizeBytes, mediaType)
		if validationErr != nil {
			return Verdict{}, validationErr
		}
		if !valid {
			return Verdict{MediaType: mediaType, ScanState: ScanFailed, PreviewState: PreviewUnavailable}, nil
		}
		return Verdict{MediaType: mediaType, ScanState: ScanClean, PreviewState: PreviewAvailable}, nil
	}
	return Verdict{MediaType: "application/octet-stream", ScanState: ScanFailed, PreviewState: PreviewUnavailable}, nil
}

func isActiveContent(extension, detected, declared string) bool {
	if containsString([]string{".htm", ".html", ".svg", ".xhtml"}, extension) {
		return true
	}
	return containsString([]string{"text/html", "image/svg+xml", "application/xhtml+xml"}, detected) ||
		containsString([]string{"text/html", "image/svg+xml", "application/xhtml+xml"}, declared)
}

func activeMediaType(extension, detected, declared string) string {
	if containsString([]string{"text/html", "image/svg+xml", "application/xhtml+xml"}, detected) {
		return detected
	}
	if containsString([]string{"text/html", "image/svg+xml", "application/xhtml+xml"}, declared) {
		return declared
	}
	if extension == ".svg" {
		return "image/svg+xml"
	}
	if extension == ".xhtml" {
		return "application/xhtml+xml"
	}
	return "text/html"
}

func validText(reader Reader, sizeBytes int64, mediaType string) (bool, error) {
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	buffered := bufio.NewReaderSize(io.LimitReader(reader, sizeBytes+1), 64<<10)
	var consumed int64
	for {
		character, width, err := buffered.ReadRune()
		consumed += int64(width)
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, err
		}
		if character == 0 || character == utf8.RuneError && width == 1 || consumed > sizeBytes {
			return false, nil
		}
	}
	if consumed != sizeBytes {
		return false, nil
	}
	if mediaType != "application/json" {
		return true, nil
	}
	if sizeBytes > maximumJSONBytes {
		return false, nil
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	body, err := io.ReadAll(io.LimitReader(reader, maximumJSONBytes+1))
	if err != nil {
		return false, err
	}
	return int64(len(body)) == sizeBytes && json.Valid(body), nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func inspectOfficeArchive(content Reader, sizeBytes int64) Verdict {
	reader, err := zip.NewReader(content, sizeBytes)
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
