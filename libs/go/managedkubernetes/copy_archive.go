package managedkubernetes

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// CopyArchiveNormalizationRevision pins the closed ownership/metadata/name transformation recipe.
	// CopyArchiveNormalizationRevision закрепляет closed ownership/metadata/name transformation recipe.
	CopyArchiveNormalizationRevision uint32 = 1
)

// CopyArchiveContent owns normalized bytes without proving artifact visibility or source authority.
// Its consumer must clear Bytes after private storage/use; generic JSON and diagnostics omit content.
// CopyArchiveContent владеет normalized bytes, не доказывая artifact visibility/source authority.
// Consumer очищает Bytes после private storage/use; JSON/diagnostics не раскрывают content.
type CopyArchiveContent struct {
	Bytes                 []byte `json:"-"`
	SHA256, MediaType     string
	Files, Directories    uint32
	NormalizationRevision uint32
}

// String excludes private file data and remote paths from diagnostic formatting.
// String скрывает private file data/remote paths в diagnostic formatting.
func (CopyArchiveContent) String() string { return "[managed Kubernetes normalized copy content]" }

// GoString preserves the same private boundary for detailed diagnostic formatting.
// GoString сохраняет private boundary при подробном diagnostic formatting.
func (content CopyArchiveContent) GoString() string { return content.String() }

// NormalizeCopyUpload verifies source bytes against the approved artifact hash, then wraps FILE
// or normalizes TAR into the exact finite profile. It never reads host paths or grants native execution.
// NormalizeCopyUpload сверяет source bytes с approved artifact hash, затем оборачивает FILE
// либо нормализует TAR по finite profile. Host paths не читает, native execution не разрешает.
func NormalizeCopyUpload(profile CopyProfile, source []byte) (result CopyArchiveContent, resultErr error) {
	if profile.Validate("kubernetes.cp.upload") != nil || uint64(len(source)) > profile.MaximumBytes {
		return result, ErrInvalidOperationInput
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != profile.ArtifactSHA256 {
		return result, ErrInvalidOperationInput
	}
	if profile.TransferFormat == "TAR" {
		return normalizeCopyTar(source, profile.MaximumBytes, "", false)
	}
	name, err := copyArchiveName(path.Base(profile.RemotePath), false)
	if err != nil {
		return result, err
	}
	buffer := &copyArchiveBuffer{limit: profile.MaximumBytes}
	defer func() {
		if resultErr != nil {
			clear(buffer.Bytes())
		}
	}()
	writer := tar.NewWriter(buffer)
	header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(source)), ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
	if writer.WriteHeader(header) != nil {
		return result, ErrInvalidOperationInput
	}
	if _, err := writer.Write(source); err != nil {
		return result, ErrInvalidOperationInput
	}
	if writer.Close() != nil {
		return result, ErrInvalidOperationInput
	}
	return copyArchiveResult(buffer.Bytes(), "application/x-tar", 1, 0), nil
}

// NormalizeCopyDownload validates the complete received tar before exposing a captured result.
// FILE requires exactly the named regular entry; TAR retains only safe normalized members under
// the requested root. Native exit success alone never validates an archive or publishes an artifact.
// NormalizeCopyDownload проверяет complete received tar до выдачи captured result.
// FILE требует exact named regular entry; TAR сохраняет safe normalized members requested root.
// Native exit success сам по себе не проверяет archive и не публикует artifact.
func NormalizeCopyDownload(profile CopyProfile, received []byte) (CopyArchiveContent, error) {
	if profile.Validate("kubernetes.cp.download") != nil || uint64(len(received)) > profile.MaximumBytes {
		return CopyArchiveContent{}, ErrInvalidOperationInput
	}
	root := ""
	if profile.RemotePath != "/" && profile.RemotePath != "." {
		root = path.Base(profile.RemotePath)
	}
	return normalizeCopyTar(received, profile.MaximumBytes, root, profile.TransferFormat == "FILE")
}

// copyArchiveBuffer refuses growth beyond the original wire budget before allocating another body.
// copyArchiveBuffer не увеличивает body сверх original wire budget до новой allocation.
type copyArchiveBuffer struct {
	bytes.Buffer
	limit uint64
}

// Write provides a bounded streaming sink for tar headers, bodies, padding and EOF markers.
// Write задаёт bounded streaming sink для tar headers/bodies/padding/EOF markers.
func (buffer *copyArchiveBuffer) Write(data []byte) (int, error) {
	if uint64(buffer.Len())+uint64(len(data)) > buffer.limit {
		return 0, ErrInvalidOperationInput
	}
	return buffer.Buffer.Write(data)
}

// copyArchiveResult transfers exclusive normalized bytes and computes their exact private digest.
// copyArchiveResult передаёт exclusive normalized bytes и вычисляет exact private digest.
func copyArchiveResult(body []byte, mediaType string, files, directories uint32) CopyArchiveContent {
	sum := sha256.Sum256(body)
	return CopyArchiveContent{Bytes: body, SHA256: hex.EncodeToString(sum[:]), MediaType: mediaType, Files: files, Directories: directories, NormalizationRevision: CopyArchiveNormalizationRevision}
}

// copyArchiveName closes POSIX member addressing, alias collisions and portable path ambiguity.
// A single conventional ./ prefix and a directory slash are normalized; traversal is never cleaned away.
// copyArchiveName закрывает POSIX member addressing, alias collisions и portable path ambiguity.
// Один conventional ./ prefix и directory slash нормализуются; traversal не удаляется скрыто.
func copyArchiveName(name string, directory bool) (string, error) {
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", ErrInvalidOperationInput
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", ErrInvalidOperationInput
		}
	}
	name = strings.TrimPrefix(name, "./")
	if directory {
		name = strings.TrimSuffix(name, "/")
	}
	if name == "" && directory {
		name = "."
	}
	firstEnd := strings.IndexByte(name, '/')
	if firstEnd < 0 {
		firstEnd = len(name)
	}
	if firstEnd == 2 && name[1] == ':' && (name[0] >= 'a' && name[0] <= 'z' || name[0] >= 'A' && name[0] <= 'Z') {
		return "", ErrInvalidOperationInput
	}
	for start := 0; start <= len(name); {
		end := strings.IndexByte(name[start:], '/')
		if end < 0 {
			end = len(name)
		} else {
			end += start
		}
		part := name[start:end]
		if part == ".." || part == "" || part == "." && name != "." {
			return "", ErrInvalidOperationInput
		}
		if end == len(name) {
			break
		}
		start = end + 1
	}
	if path.Clean(name) != name || name == "." && !directory {
		return "", ErrInvalidOperationInput
	}
	return name, nil
}

// validCopyArchiveHeader admits only ordinary bytes/directories and known non-executable PAX metadata.
// Ownership, timestamps and extended metadata are discarded rather than forwarded to remote tar.
// validCopyArchiveHeader допускает лишь ordinary bytes/directories и known non-executable PAX metadata.
// Ownership/timestamps/extended metadata удаляются и не передаются remote tar.
func validCopyArchiveHeader(header *tar.Header, maximum uint64) bool {
	if header == nil || header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir || header.Size < 0 || uint64(header.Size) > maximum || header.Typeflag == tar.TypeDir && header.Size != 0 || header.Linkname != "" || header.Mode < 0 || header.Mode&^0777 != 0 || len(header.Xattrs) != 0 {
		return false
	}
	for key := range header.PAXRecords {
		switch key {
		case "path", "size", "mtime", "atime", "ctime", "uid", "gid", "uname", "gname":
		default:
			return false
		}
	}
	return true
}

// normalizeCopyTar validates all members and the physical terminator before returning any bytes.
// It detects path aliases/file-parent collisions and rejects data hidden after the tar EOF marker.
// normalizeCopyTar проверяет всех members и physical terminator до возврата bytes.
// Находит path aliases/file-parent collisions и отвергает data после tar EOF marker.
func normalizeCopyTar(source []byte, maximum uint64, root string, extractFile bool) (result CopyArchiveContent, resultErr error) {
	if uint64(len(source)) > maximum || len(source) < 1024 || len(source)%512 != 0 {
		return result, ErrInvalidOperationInput
	}
	reader := bytes.NewReader(source)
	archive := tar.NewReader(reader)
	buffer := &copyArchiveBuffer{limit: maximum}
	defer func() {
		if resultErr != nil {
			clear(buffer.Bytes())
		}
	}()
	writer := tar.NewWriter(buffer)
	seen := map[string]bool{}
	var files, directories uint32
	lastAlignedEnd := int64(0)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			offset := int64(len(source) - reader.Len())
			if offset-lastAlignedEnd < 1024 {
				return result, ErrInvalidOperationInput
			}
			for _, value := range source[offset:] {
				if value != 0 {
					return result, ErrInvalidOperationInput
				}
			}
			break
		}
		if err != nil || !validCopyArchiveHeader(header, maximum) {
			return result, ErrInvalidOperationInput
		}
		directory := header.Typeflag == tar.TypeDir
		name, err := copyArchiveName(header.Name, directory)
		if err != nil {
			return result, err
		}
		if root != "" && name != root && !strings.HasPrefix(name, root+"/") {
			return result, ErrInvalidOperationInput
		}
		if _, duplicate := seen[name]; duplicate {
			return result, ErrInvalidOperationInput
		}

		seen[name] = directory
		if directory {
			directories++
		} else {
			files++
		}
		if extractFile {
			if directory || files != 1 || name != root {
				return result, ErrInvalidOperationInput
			}
		} else {
			normalizedName := name
			if directory && name != "." {
				normalizedName += "/"
			}
			normalized := &tar.Header{Name: normalizedName, Typeflag: tar.TypeReg, Mode: header.Mode, Size: header.Size, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
			if directory {
				normalized.Typeflag = tar.TypeDir
			}
			if writer.WriteHeader(normalized) != nil {
				return result, ErrInvalidOperationInput
			}
		}
		var target io.Writer = writer
		if extractFile {
			target = buffer
		}
		if _, err := io.CopyN(target, archive, header.Size); err != nil {
			return result, ErrInvalidOperationInput
		}
		offset := int64(len(source) - reader.Len())
		lastAlignedEnd = (offset + 511) / 512 * 512
	}
	// Complete names consume metadata proportional to wire bytes; no fixed entry/depth cap is needed.
	// Полные names занимают metadata пропорционально wire bytes; fixed entry/depth cap не нужен.
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	for name, directory := range seen {
		if directory {
			continue
		}
		prefix := name + "/"
		index := sort.SearchStrings(names, prefix)
		if index < len(names) && strings.HasPrefix(names[index], prefix) {
			return result, ErrInvalidOperationInput
		}
	}
	if extractFile {
		if files != 1 || directories != 0 {
			return result, ErrInvalidOperationInput
		}
		return copyArchiveResult(buffer.Bytes(), "application/octet-stream", files, directories), nil
	}
	if writer.Close() != nil {
		return result, ErrInvalidOperationInput
	}
	return copyArchiveResult(buffer.Bytes(), "application/x-tar", files, directories), nil
}
