// Package artifacttype определяет фактический тип непрозрачного артефакта по его содержимому.
package artifacttype

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxObjectBytes — максимальный размер одного проверяемого артефакта.
	MaxObjectBytes                = int64(8 << 20)
	maxContainerEntries           = 512
	maxContainerEntryBytes        = uint64(32 << 20)
	maxContainerUncompressedBytes = uint64(64 << 20)
	maxIdentificationEntryBytes   = uint64(1 << 20)
	maxCompressionRatio           = uint64(100)
)

// ErrDenied означает, что содержимое не прошло закрытую проверку разрешённых форматов.
var ErrDenied = errors.New("artifact media type denied")

// Format описывает канонический MIME, безопасное расширение и семейство формата.
type Format struct {
	MediaType string
	Extension string
	Family    string
	Text      bool
}

var formats = map[string]Format{}

var packageMIMETypes = map[string]Format{}

var ooxmlMainContentTypes = map[string]ooxmlFormat{}

type ooxmlFormat struct {
	Format
	MainPart string
}

func init() {
	register("text/plain", ".txt", "text", true)
	register("text/markdown", ".md", "text", true)
	register("text/csv", ".csv", "text", true)
	register("application/json", ".json", "text", true)
	register("application/pdf", ".pdf", "document", false)
	register("image/png", ".png", "image", false)
	register("image/jpeg", ".jpg", "image", false)
	register("image/webp", ".webp", "image", false)
	register("image/gif", ".gif", "image", false)

	register("application/zip", ".zip", "archive", false)
	register("application/x-tar", ".tar", "archive", false)
	register("application/gzip", ".gz", "archive", false)

	register("application/msword", ".doc", "microsoft-office-binary", false)
	register("application/vnd.ms-excel", ".xls", "microsoft-office-binary", false)
	register("application/vnd.ms-powerpoint", ".ppt", "microsoft-office-binary", false)

	registerPackage("application/vnd.oasis.opendocument.text", ".odt", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.text-template", ".ott", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.text-master", ".odm", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.text-master-template", ".otm", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.text-web", ".oth", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.spreadsheet", ".ods", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.spreadsheet-template", ".ots", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.presentation", ".odp", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.presentation-template", ".otp", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.graphics", ".odg", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.graphics-template", ".otg", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.chart", ".odc", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.chart-template", ".otc", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.image", ".odi", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.image-template", ".oti", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.formula", ".odf", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.formula-template", ".odft", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.base", ".odb", "opendocument")
	registerPackage("application/vnd.oasis.opendocument.database", ".odb", "opendocument")

	registerPackage("application/vnd.sun.xml.writer", ".sxw", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.writer.template", ".stw", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.writer.global", ".sxg", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.calc", ".sxc", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.calc.template", ".stc", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.impress", ".sxi", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.impress.template", ".sti", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.draw", ".sxd", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.draw.template", ".std", "openoffice-xml")
	registerPackage("application/vnd.sun.xml.math", ".sxm", "openoffice-xml")

	registerOOXML("application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", "word/document.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.wordprocessingml.template", ".dotx", "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml", "word/document.xml")

	registerOOXML("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml", "xl/workbook.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.spreadsheetml.template", ".xltx", "application/vnd.openxmlformats-officedocument.spreadsheetml.template.main+xml", "xl/workbook.xml")

	registerOOXML("application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.presentationml.template", ".potx", "application/vnd.openxmlformats-officedocument.presentationml.template.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.presentationml.slideshow", ".ppsx", "application/vnd.openxmlformats-officedocument.presentationml.slideshow.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.presentationml.slide", ".sldx", "application/vnd.openxmlformats-officedocument.presentationml.slide+xml", "ppt/slides/slide1.xml")
}

func register(mediaType string, extension string, family string, text bool) Format {
	mediaType = normalizeMediaType(mediaType)
	format := Format{MediaType: mediaType, Extension: extension, Family: family, Text: text}
	formats[mediaType] = format
	return format
}

func registerPackage(mediaType string, extension string, family string) {
	format := register(mediaType, extension, family, false)
	packageMIMETypes[format.MediaType] = format
}

func registerOOXML(mediaType string, extension string, contentType string, mainPart string) {
	format := register(mediaType, extension, "microsoft-office-ooxml", false)
	ooxmlMainContentTypes[normalizeMediaType(contentType)] = ooxmlFormat{Format: format, MainPart: mainPart}
}

// SupportedFormats возвращает отсортированную копию server-side allowlist.
func SupportedFormats() []Format {
	result := make([]Format, 0, len(formats))
	for _, format := range formats {
		result = append(result, format)
	}
	sort.Slice(result, func(i int, j int) bool {
		if result[i].Family != result[j].Family {
			return result[i].Family < result[j].Family
		}
		return result[i].MediaType < result[j].MediaType
	})
	return result
}

// Extension возвращает безопасное расширение для уже проверенного MIME.
func Extension(mediaType string) (string, error) {
	format, ok := formats[normalizeMediaType(mediaType)]
	if !ok {
		return "", ErrDenied
	}
	return format.Extension, nil
}

// IsText сообщает, можно ли обрабатывать артефакт как простой текст.
func IsText(mediaType string) bool {
	format, ok := formats[normalizeMediaType(mediaType)]
	return ok && format.Text
}

// DetectBytes определяет MIME по содержимому байтового среза.
func DetectBytes(body []byte) (string, error) {
	return Detect(bytes.NewReader(body), int64(len(body)))
}

// Detect определяет MIME по ограниченному содержимому и отклоняет неоднозначные данные.
func Detect(reader io.ReaderAt, size int64) (string, error) {
	if size < 0 || size > MaxObjectBytes {
		return "", ErrDenied
	}
	if size == 0 {
		return "text/plain", nil
	}
	body := make([]byte, int(size))
	if _, err := reader.ReadAt(body, 0); err != nil {
		return "", ErrDenied
	}

	switch {
	case isZIPSignature(body):
		return detectZIPPackage(body)
	case bytes.HasPrefix(body, compoundFileSignature):
		return detectCompoundOffice(body)
	case bytes.HasPrefix(body, []byte{0x1f, 0x8b}):
		if err := validateGZIP(body); err != nil {
			return "", ErrDenied
		}
		return "application/gzip", nil
	case isTAR(body):
		if err := validateTAR(body); err != nil {
			return "", ErrDenied
		}
		return "application/x-tar", nil
	case bytes.HasPrefix(body, []byte("%PDF-")):
		if !validPDF(body) {
			return "", ErrDenied
		}
		return "application/pdf", nil
	case bytes.HasPrefix(body, pngSignature):
		if !validPNG(body) {
			return "", ErrDenied
		}
		return "image/png", nil
	case bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff}):
		if !validJPEG(body) {
			return "", ErrDenied
		}
		return "image/jpeg", nil
	case bytes.HasPrefix(body, []byte("GIF87a")), bytes.HasPrefix(body, []byte("GIF89a")):
		if !validGIF(body) {
			return "", ErrDenied
		}
		return "image/gif", nil
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		if !validWEBP(body) {
			return "", ErrDenied
		}
		return "image/webp", nil
	}

	detected := normalizeMediaType(strings.Split(http.DetectContentType(body[:min(len(body), 512)]), ";")[0])
	if detected == "text/html" || detected == "text/xml" || detected == "application/xml" || looksLikeActiveText(body) {
		return "", ErrDenied
	}
	if utf8.Valid(body) && !bytes.ContainsRune(body, '\x00') && textual(body) {
		if json.Valid(bytes.TrimSpace(body)) {
			return "application/json", nil
		}
		if unambiguousCSV(body) {
			return "text/csv", nil
		}
		if unambiguousMarkdown(body) {
			return "text/markdown", nil
		}
		return "text/plain", nil
	}
	return "", ErrDenied
}

func validateGZIP(body []byte) error {
	source := bytes.NewReader(body)
	var total uint64
	for member := 0; source.Len() > 0; member++ {
		if member >= 32 {
			return ErrDenied
		}
		before := source.Len()
		reader, err := gzip.NewReader(source)
		if err != nil {
			return ErrDenied
		}
		reader.Multistream(false)
		if reader.Name != "" && !validContainerPath(reader.Name, false) {
			_ = reader.Close()
			return ErrDenied
		}
		remaining := maxContainerUncompressedBytes - total
		written, copyErr := io.Copy(io.Discard, io.LimitReader(reader, int64(remaining)+1))
		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil || written < 0 || uint64(written) > remaining || source.Len() >= before {
			return ErrDenied
		}
		total += uint64(written)
	}
	return nil
}

func isTAR(body []byte) bool {
	return len(body) >= 1024 && (bytes.Equal(body[257:263], []byte("ustar\x00")) || bytes.Equal(body[257:263], []byte("ustar ")))
}

func validateTAR(body []byte) error {
	if len(body)%512 != 0 || len(body) < 1024 {
		return ErrDenied
	}
	terminator := tarTerminatorOffset(body)
	if terminator < 0 || terminator+1024 > len(body) || !allZero(body[terminator:]) {
		return ErrDenied
	}
	reader := tar.NewReader(bytes.NewReader(body[:terminator+1024]))
	seen := map[string]struct{}{}
	entries := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || entries >= maxContainerEntries || header.Size < 0 || uint64(header.Size) > maxContainerEntryBytes {
			return ErrDenied
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != 0 && header.Typeflag != tar.TypeDir {
			return ErrDenied
		}
		if !validContainerPath(header.Name, header.Typeflag == tar.TypeDir) {
			return ErrDenied
		}
		key := strings.ToLower(strings.TrimSuffix(header.Name, "/"))
		if _, ok := seen[key]; ok {
			return ErrDenied
		}
		seen[key] = struct{}{}
		entries++
	}
	return nil
}

func tarTerminatorOffset(body []byte) int {
	for offset := 0; offset+512 <= len(body); {
		header := body[offset : offset+512]
		if allZero(header) {
			if offset+1024 > len(body) || !allZero(body[offset+512:offset+1024]) {
				return -1
			}
			return offset
		}
		size, ok := parseTARSize(header[124:136])
		if !ok || size > maxContainerEntryBytes {
			return -1
		}
		paddedSize := (size + 511) / 512 * 512
		if paddedSize > uint64(len(body)-offset-512) {
			return -1
		}
		offset += 512 + int(paddedSize)
	}
	return -1
}

func parseTARSize(field []byte) (uint64, bool) {
	value := strings.Trim(string(bytes.Trim(field, "\x00 ")), " ")
	if value == "" {
		return 0, true
	}
	for _, character := range value {
		if character < '0' || character > '7' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseUint(value, 8, 64)
	return parsed, err == nil
}

func unambiguousCSV(body []byte) bool {
	if len(body) == 0 || len(body) > int(MaxObjectBytes) || !bytes.ContainsRune(body, ',') {
		return false
	}
	reader := csv.NewReader(bytes.NewReader(body))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	rows := 0
	columns := 0
	strongEvidence := false
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || len(record) < 2 || len(record) > 256 {
			return false
		}
		if rows == 0 {
			columns = len(record)
			if !validCSVHeader(record) {
				return false
			}
		} else {
			if len(record) != columns {
				return false
			}
			for _, field := range record {
				if csvDataEvidence(strings.TrimSpace(field)) {
					strongEvidence = true
				}
			}
		}
		rows++
		if rows > 100_000 {
			return false
		}
	}
	return rows >= 2 && (rows >= 3 || strongEvidence || bytes.ContainsRune(body, '"'))
}

func validCSVHeader(record []string) bool {
	seen := make(map[string]struct{}, len(record))
	for _, field := range record {
		field = strings.TrimSpace(field)
		if field == "" || utf8.RuneCountInString(field) > 128 || strings.ContainsAny(field, "\r\n\t") {
			return false
		}
		key := strings.ToLower(field)
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func csvDataEvidence(value string) bool {
	if value == "" {
		return true
	}
	lower := strings.ToLower(value)
	if lower == "true" || lower == "false" || lower == "null" {
		return true
	}
	digits := 0
	for index, r := range value {
		if unicode.IsDigit(r) {
			digits++
			continue
		}
		if (r == '+' || r == '-') && index == 0 || r == '.' || r == ':' || r == '/' {
			continue
		}
		return false
	}
	return digits > 0
}

func unambiguousMarkdown(body []byte) bool {
	if len(body) == 0 || len(body) > int(MaxObjectBytes) {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	if len(lines) > 100_000 {
		return false
	}
	nonEmpty := 0
	listItems := 0
	heading := false
	blockquote := 0
	inFence := false
	closedFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		nonEmpty++
		if markdownHeading(trimmed) {
			heading = true
		}
		if markdownListItem(trimmed) {
			listItems++
		}
		if strings.HasPrefix(trimmed, "> ") {
			blockquote++
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if inFence {
				closedFence = true
			}
			inFence = !inFence
		}
	}
	if heading && nonEmpty >= 2 || listItems >= 2 || blockquote >= 2 || closedFence && !inFence {
		return true
	}
	if len(lines) >= 2 && markdownTableRow(lines[0]) && markdownTableSeparator(lines[1]) {
		return true
	}
	text := string(body)
	return nonEmpty >= 1 && strings.Contains(text, "](") && strings.Contains(text, "[")
}

func markdownHeading(line string) bool {
	count := 0
	for count < len(line) && line[count] == '#' {
		count++
	}
	return count >= 1 && count <= 6 && count < len(line) && line[count] == ' '
}

func markdownListItem(line string) bool {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
		return len(strings.TrimSpace(line[2:])) > 0
	}
	dot := strings.IndexByte(line, '.')
	if dot <= 0 || dot+1 >= len(line) || line[dot+1] != ' ' {
		return false
	}
	for _, r := range line[:dot] {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(strings.TrimSpace(line[dot+2:])) > 0
}

func markdownTableRow(line string) bool {
	line = strings.TrimSpace(line)
	return strings.Count(line, "|") >= 2
}

func markdownTableSeparator(line string) bool {
	line = strings.Trim(strings.TrimSpace(line), "|")
	parts := strings.Split(line, "|")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), ":")
		if len(part) < 3 || strings.Trim(part, "-") != "" {
			return false
		}
	}
	return true
}

func validContainerPath(name string, directory bool) bool {
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') || strings.ContainsRune(name, '\\') {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			return false
		}
	}
	cleanedName := strings.TrimSuffix(name, "/")
	if cleanedName == "" || path.IsAbs(cleanedName) || path.Clean(cleanedName) != cleanedName || strings.HasPrefix(cleanedName, "../") || cleanedName == ".." {
		return false
	}
	if len(cleanedName) >= 2 && ((cleanedName[0] >= 'a' && cleanedName[0] <= 'z') || (cleanedName[0] >= 'A' && cleanedName[0] <= 'Z')) && cleanedName[1] == ':' {
		return false
	}
	return directory || !strings.HasSuffix(name, "/")
}

func normalizeMediaType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func textual(body []byte) bool {
	for _, r := range string(body) {
		if r == '\n' || r == '\r' || r == '\t' || !unicode.IsControl(r) {
			continue
		}
		return false
	}
	return true
}

func looksLikeActiveText(body []byte) bool {
	trimmed := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 512)])))
	for _, prefix := range []string{"<!doctype html", "<html", "<head", "<body", "<script", "<?xml", "<svg"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func allZero(body []byte) bool {
	for _, value := range body {
		if value != 0 {
			return false
		}
	}
	return true
}

func checksumIEEE(body []byte) uint32 {
	return crc32.ChecksumIEEE(body)
}
