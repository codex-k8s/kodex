package artifactpolicy

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
)

// InspectRepositorySourceReader admits validated active text only for the server-owned Git
// publication path. It never grants preview or execution, and mandatory antivirus remains the
// caller's separate exact-byte check. Ordinary uploads must continue to use InspectReader.
// InspectRepositorySourceReader допускает проверенный активный текст только в принадлежащем
// серверу пути публикации Git. Preview и исполнение не разрешаются; обязательный антивирус
// отдельно проверяет точные байты. Обычная загрузка продолжает использовать InspectReader.
func InspectRepositorySourceReader(fileName string, reader Reader, sizeBytes int64) (Verdict, error) {
	verdict, err := InspectReader(fileName, "application/octet-stream", reader, sizeBytes)
	if err != nil {
		return Verdict{}, err
	}
	if verdict.ScanState == ScanClean {
		verdict.PreviewState = PreviewUnavailable
		return verdict, nil
	}
	if verdict.ScanState != ScanQuarantined {
		return verdict, nil
	}
	head := make([]byte, contentDetectionBytes)
	count, err := reader.ReadAt(head, 0)
	if err != nil && err != io.EOF {
		return Verdict{}, err
	}
	head = head[:count]
	if bytes.HasPrefix(head, []byte("MZ")) || bytes.HasPrefix(head, []byte("\x7fELF")) || bytes.HasPrefix(head, []byte("PK")) {
		return verdict, nil
	}
	extension := strings.ToLower(filepath.Ext(fileName))
	active := isActiveContent(extension, verdict.MediaType, "application/octet-stream")
	if !active && !bytes.HasPrefix(head, []byte("#!")) {
		return verdict, nil
	}
	mediaType := textMediaType(extension, "application/octet-stream")
	valid, err := validText(reader, sizeBytes, mediaType)
	if err != nil {
		return Verdict{}, err
	}
	if !valid {
		return Verdict{MediaType: verdict.MediaType, ScanState: ScanFailed, PreviewState: PreviewUnavailable}, nil
	}
	if active {
		mediaType = activeMediaType(extension, verdict.MediaType, "application/octet-stream")
	}
	return Verdict{MediaType: mediaType, ScanState: ScanClean, PreviewState: PreviewUnavailable}, nil
}
