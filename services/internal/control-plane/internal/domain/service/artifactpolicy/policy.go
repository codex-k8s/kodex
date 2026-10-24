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
	// ScanClean marks content admitted by the built-in synchronous format policy, without certifying
	// absence of malware or external antivirus coverage.
	// ScanClean обозначает содержимое, допущенное встроенной синхронной policy форматов, без
	// удостоверения отсутствия malware либо внешнего antivirus покрытия.
	ScanClean = "CLEAN"
	// ScanQuarantined marks executable, active or unsafe archive content for quarantine and blocked
	// preview.
	// ScanQuarantined обозначает исполняемое, активное либо небезопасное архивное содержимое для
	// карантина и заблокированного preview.
	ScanQuarantined = "QUARANTINED"
	// ScanFailed marks unsupported or invalid content that did not pass the built-in format checks and
	// must not gain preview availability.
	// ScanFailed обозначает неподдерживаемое либо неверное содержимое, не прошедшее встроенные
	// проверки формата и не получающее доступный preview.
	ScanFailed = "FAILED"
	// PreviewAvailable marks formats admitted for preview after the built-in content checks rather
	// than granting resource visibility by itself.
	// PreviewAvailable обозначает форматы, допущенные к preview встроенными проверками содержимого,
	// без самостоятельной выдачи видимости ресурса.
	PreviewAvailable = "AVAILABLE"
	// PreviewUnavailable marks a result with no supported preview, including accepted PDF/Office
	// formats and failed format inspection.
	// PreviewUnavailable обозначает результат без поддерживаемого preview, включая принятые PDF и
	// Office форматы и отказ проверки формата.
	PreviewUnavailable = "UNAVAILABLE"
	// PreviewBlocked marks preview explicitly blocked by executable, active-content or archive
	// quarantine policy.
	// PreviewBlocked обозначает preview, явно заблокированный policy карантина executable, active
	// content либо архивов.
	PreviewBlocked = "BLOCKED"
	// maximumArchiveEntries bounds the number of ZIP central-directory entries inspected as a
	// potential Office document.
	// maximumArchiveEntries ограничивает число элементов central directory ZIP, проверяемого как
	// возможный Office документ.
	maximumArchiveEntries = 512
	// maximumArchiveBytes bounds individual and summed declared uncompressed ZIP sizes without reading
	// every archive member into memory.
	// maximumArchiveBytes ограничивает отдельный и суммарный объявленный распакованный размер ZIP без
	// чтения каждого элемента архива в память.
	maximumArchiveBytes uint64 = 512 << 20
	// maximumJSONBytes caps JSON bodies buffered for json.Valid after streamed text and declared-size
	// validation.
	// maximumJSONBytes ограничивает JSON body, помещаемый в память для json.Valid после потоковой
	// проверки текста и объявленного размера.
	maximumJSONBytes = 8 << 20
	// contentDetectionBytes bounds the initial random-access header used for executable signatures and
	// HTTP content-type detection.
	// contentDetectionBytes ограничивает начальный header произвольного чтения для executable
	// signatures и HTTP определения content type.
	contentDetectionBytes = 512
)

// Verdict records server-derived media type, scan state and preview state from the built-in
// synchronous policy without carrying file content.
// Verdict хранит вычисленные сервером media type, scan state и preview state встроенной синхронной
// policy без содержимого файла.
type Verdict struct {
	MediaType, ScanState, PreviewState string
}

// Reader requires sequential, random-access and seek operations on caller-owned content so
// inspection can revisit bounded regions without accepting a filesystem path.
// Reader требует последовательное и произвольное чтение и seek содержимого caller для повторной
// проверки ограниченных областей без принятия filesystem пути.
type Reader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

// Inspect adapts an in-memory body to reader inspection and converts reader failures into a
// failed/unavailable verdict with canonical declared media fallback.
// Inspect передаёт body в памяти проверке reader и преобразует ошибки чтения в failed/unavailable
// verdict с каноническим запасным declared media type.
func Inspect(fileName, declaredMediaType string, body []byte) Verdict {
	verdict, err := InspectReader(fileName, declaredMediaType, bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return Verdict{MediaType: canonicalFallback(declaredMediaType), ScanState: ScanFailed, PreviewState: PreviewUnavailable}
	}
	return verdict
}

// InspectReader checks executable/active headers and Office metadata, admits supported image/PDF
// or validated text, and streams UTF-8 checks while buffering only bounded JSON or content-type
// metadata.
// InspectReader проверяет executable и active headers и метаданные Office, допускает
// поддерживаемые image/PDF либо проверенный текст и потоково проверяет UTF-8 с буферизацией лишь
// ограниченных JSON либо content-type метаданных.
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

// isActiveContent treats HTML, SVG and XHTML as active when indicated by extension, detected type
// or canonical declared type, even if the body otherwise looks like plain text.
// isActiveContent считает HTML, SVG и XHTML активными по расширению, определённому либо
// каноническому объявленному типу, даже если body выглядит обычным текстом.
func isActiveContent(extension, detected, declared string) bool {
	if containsString([]string{".htm", ".html", ".svg", ".xhtml"}, extension) {
		return true
	}
	return containsString([]string{"text/html", "image/svg+xml", "application/xhtml+xml"}, detected) ||
		containsString([]string{"text/html", "image/svg+xml", "application/xhtml+xml"}, declared)
}

// activeMediaType prefers detected active MIME, then declared active MIME, then the active
// extension and finally HTML for the quarantine verdict.
// activeMediaType выбирает для quarantine verdict сначала определённый active MIME, затем
// объявленный active MIME, затем активное расширение и в конце HTML.
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

// validText streams exactly the declared byte length as UTF-8 without NUL, detecting truncation
// and extra bytes; JSON additionally requires bounded full-body syntax validation.
// validText потоково проверяет ровно объявленное число байт как UTF-8 без NUL с выявлением обрезки
// и лишних байт; JSON дополнительно требует ограниченную проверку синтаксиса полного body.
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

// containsString performs exact membership over a supplied closed media-type or extension list
// without case normalization or prefix matching.
// containsString выполняет точную проверку в переданном закрытом списке media types либо
// расширений без нормализации регистра и prefix matching.
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// canonicalFallback parses a declared MIME value, drops its parameters and lowercases the type,
// returning octet-stream on an absent or malformed declaration.
// canonicalFallback разбирает объявленный MIME, удаляет параметры и переводит тип в нижний регистр
// с возвратом octet-stream при отсутствующей либо неверной декларации.
func canonicalFallback(declared string) string {
	mediaType, _, err := mime.ParseMediaType(declared)
	if err != nil || mediaType == "" {
		return "application/octet-stream"
	}
	return strings.ToLower(mediaType)
}

// textMediaType maps recognized text extensions first, otherwise keeps a canonical text/JSON
// declaration and defaults to plain text for subsequent actual UTF-8 validation.
// textMediaType сначала отображает известные текстовые расширения, иначе сохраняет канонический
// text/JSON declaration и выбирает plain text для дальнейшей фактической проверки UTF-8.
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

// isExecutable recognizes the fixed MZ, ELF and shebang byte prefixes for quarantine rather than
// relying on a filename extension.
// isExecutable распознаёт фиксированные байтовые префиксы MZ, ELF и shebang для карантина вместо
// опоры на расширение имени файла.
func isExecutable(body []byte) bool {
	return bytes.HasPrefix(body, []byte("MZ")) ||
		bytes.HasPrefix(body, []byte("\x7fELF")) ||
		bytes.HasPrefix(body, []byte("#!"))
}

// inspectOfficeArchive bounds ZIP entries and declared expansion, rejects traversal and dangerous
// names, and derives Office MIME from bounded content-types metadata without decompressing or
// scanning every member.
// inspectOfficeArchive ограничивает ZIP элементы и объявленное распакованное содержимое, отклоняет
// traversal и опасные имена и выводит Office MIME из ограниченных content-types метаданных без
// распаковки и scanning каждого элемента.
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

// dangerousArchiveEntry rejects known macro, executable and script filename suffixes in normalized
// lowercase archive paths.
// dangerousArchiveEntry отклоняет известные суффиксы имён macro, executable и script в
// нормализованных архивных путях нижнего регистра.
func dangerousArchiveEntry(name string) bool {
	for _, suffix := range []string{"vbaproject.bin", ".exe", ".dll", ".com", ".js", ".vbs", ".ps1", ".sh", ".bat", ".cmd"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// readBounded opens one ZIP entry, reads at most limit plus one bytes and maps read failure or
// overflow to unexpected EOF while closing the entry reader.
// readBounded открывает один ZIP элемент, читает не более limit плюс один байт и отображает отказ
// чтения либо переполнение в unexpected EOF с закрытием reader элемента.
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

// officeMediaType identifies supported Word, Excel or PowerPoint MIME by their lowercase
// content-type marker in metadata and rejects absent recognized markers.
// officeMediaType определяет поддерживаемый MIME Word, Excel либо PowerPoint по content-type
// marker нижнего регистра в метаданных и отклоняет отсутствие известного marker.
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
