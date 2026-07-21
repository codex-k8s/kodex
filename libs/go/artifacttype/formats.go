// Package artifacttype определяет фактический тип непрозрачного артефакта по его содержимому.
package artifacttype

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"path"
	"sort"
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
	registerOOXML("application/vnd.ms-word.document.macroenabled.12", ".docm", "application/vnd.ms-word.document.macroenabled.main+xml", "word/document.xml")
	registerOOXML("application/vnd.ms-word.template.macroenabled.12", ".dotm", "application/vnd.ms-word.template.macroenabledtemplate.main+xml", "word/document.xml")

	registerOOXML("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml", "xl/workbook.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.spreadsheetml.template", ".xltx", "application/vnd.openxmlformats-officedocument.spreadsheetml.template.main+xml", "xl/workbook.xml")
	registerOOXML("application/vnd.ms-excel.sheet.macroenabled.12", ".xlsm", "application/vnd.ms-excel.sheet.macroenabled.main+xml", "xl/workbook.xml")
	registerOOXML("application/vnd.ms-excel.template.macroenabled.12", ".xltm", "application/vnd.ms-excel.template.macroenabled.main+xml", "xl/workbook.xml")
	registerOOXML("application/vnd.ms-excel.addin.macroenabled.12", ".xlam", "application/vnd.ms-excel.addin.macroenabled.main+xml", "xl/workbook.xml")
	registerOOXML("application/vnd.ms-excel.sheet.binary.macroenabled.12", ".xlsb", "application/vnd.ms-excel.sheet.binary.macroenabled.main", "xl/workbook.bin")

	registerOOXML("application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.presentationml.template", ".potx", "application/vnd.openxmlformats-officedocument.presentationml.template.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.presentationml.slideshow", ".ppsx", "application/vnd.openxmlformats-officedocument.presentationml.slideshow.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.openxmlformats-officedocument.presentationml.slide", ".sldx", "application/vnd.openxmlformats-officedocument.presentationml.slide+xml", "ppt/slides/slide1.xml")
	registerOOXML("application/vnd.ms-powerpoint.presentation.macroenabled.12", ".pptm", "application/vnd.ms-powerpoint.presentation.macroenabled.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.ms-powerpoint.template.macroenabled.12", ".potm", "application/vnd.ms-powerpoint.template.macroenabled.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.ms-powerpoint.slideshow.macroenabled.12", ".ppsm", "application/vnd.ms-powerpoint.slideshow.macroenabled.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.ms-powerpoint.addin.macroenabled.12", ".ppam", "application/vnd.ms-powerpoint.addin.macroenabled.main+xml", "ppt/presentation.xml")
	registerOOXML("application/vnd.ms-powerpoint.slide.macroenabled.12", ".sldm", "application/vnd.ms-powerpoint.slide.macroenabled.main+xml", "ppt/slides/slide1.xml")
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
	if len(body)%512 != 0 || len(body) < 1024 || !allZero(body[len(body)-1024:]) {
		return ErrDenied
	}
	reader := tar.NewReader(bytes.NewReader(body))
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
