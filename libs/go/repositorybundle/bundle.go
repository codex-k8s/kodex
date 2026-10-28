// Package repositorybundle validates a bounded immutable repository bundle without extracting paths onto a filesystem or reading credentials.
// Package repositorybundle проверяет ограниченный immutable repository bundle без извлечения путей в filesystem и чтения credentials.
package repositorybundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits bound the complete manifest and uncompressed project independently of existing single-file integration capabilities.
// Limits ограничивают полный manifest и распакованный проект независимо от существующих single-file integration capabilities.
const (
	Operation            = "github.repository.bundle.publish"
	ToolName             = "prepare_repository_bundle_publication"
	MaximumFiles         = 256
	MaximumContentBytes  = 1 << 20
	MaximumArchiveBytes  = 1 << 20
	MaximumManifestBytes = 128 << 10
	MaximumPathBytes     = 512
)

// RejoinSchema identifies only an owner-proven read of a completed native publication, never a new write request.
// RejoinSchema обозначает owner-proven чтение completed publication, а не новый WRITE request.
const RejoinSchema = "kodex.repository-bundle-rejoin.v1"

// RejoinMode separates terminal read-only outcomes from ordinary pending publication preparation.
// RejoinMode отделяет terminal read-only outcome от обычного pending preparation.
const RejoinMode = "READ_ONLY_REJOIN"

// ErrInvalid is a fixed metadata-only diagnostic; offending archive names or content must never enter logs through this error.
// ErrInvalid — фиксированная metadata-only диагностика; недопустимые имена и содержимое архива через неё в логи не попадают.
var ErrInvalid = errors.New("repository bundle is invalid")

// Entry binds one exact regular Git file to its full byte length, SHA256 and canonical Git mode; deletion and links are intentionally unsupported.
// Entry связывает один regular Git file с полным размером, SHA256 и canonical Git mode; удаления и links намеренно не поддерживаются.
type Entry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Manifest is a canonical versioned full set, sorted by path; it cannot describe a partial or truncated listing as complete.
// Manifest — canonical versioned полный набор, отсортированный по path; частичная либо усечённая выдача не считается полной.
type Manifest struct {
	Version    int     `json:"version"`
	TotalBytes int64   `json:"total_bytes"`
	Files      []Entry `json:"files"`
}

// File carries verified bytes alongside their manifest pin for Git plumbing; callers own lifetime and must not log the content.
// File переносит проверенные bytes вместе с manifest pin для Git plumbing; caller владеет временем жизни и не логирует content.
type File struct {
	Entry Entry
	Bytes []byte
}

// ArtifactPin carries captured artifact metadata for the compact native publication request; it is not a credential, object locator or permission to download bytes.
// ArtifactPin переносит captured artifact metadata компактного native publication request; credential, object locator и разрешения download в нём нет.
type ArtifactPin struct {
	Ref, SHA256             string
	Revision, Version, Size int64
}

// InvocationInput materializes the one shared scalar capability input shape from already verified pins, without embedding source bytes or changing numeric values.
// InvocationInput материализует единственную общую scalar capability input shape из проверенных pins, не включая source bytes и не меняя числовые значения.
func InvocationInput(archive, manifest ArtifactPin, base string) map[string]any {
	return map[string]any{"archive_ref": archive.Ref, "archive_revision": archive.Revision, "archive_version": archive.Version, "archive_sha256": archive.SHA256, "archive_size_bytes": archive.Size,
		"manifest_ref": manifest.Ref, "manifest_revision": manifest.Revision, "manifest_version": manifest.Version, "manifest_sha256": manifest.SHA256, "manifest_size_bytes": manifest.Size, "base_commit_sha": base}
}

// Digest returns lowercase SHA256 of the exact bytes without encoding, whitespace normalization or newline changes.
// Digest возвращает lowercase SHA256 точных bytes без перекодирования, нормализации whitespace и изменения newline.
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ValidDigest admits only a full lowercase SHA256; it does not accept prefixes or another hash algorithm.
// ValidDigest допускает лишь полный lowercase SHA256, без prefixes и другого hash algorithm.
func ValidDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return true
}

// ValidPath rejects ambiguous relative Git paths, control/format characters, Git metadata and credential filenames before any provider or filesystem operation. Accepted paths retain their exact UTF8 bytes without normalization.
// ValidPath отклоняет неоднозначные relative Git paths, control/format characters, Git metadata и имена credentials до provider/filesystem operation. Допустимые paths сохраняют exact UTF8 bytes без нормализации.
func ValidPath(value string) bool {
	if value == "" || len(value) > MaximumPathBytes || !utf8.ValidString(value) || path.Clean(value) != value || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:\x00\r\n\t") {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return false
		}
	}
	for _, part := range strings.Split(value, "/") {
		lower := strings.ToLower(part)
		if part == "" || part == "." || part == ".." || strings.TrimSpace(part) != part || strings.TrimRight(part, ". ") != part || lower == ".git" || lower == ".env" || lower == "id_rsa" || lower == "id_ed25519" || lower == "credentials.json" || strings.HasSuffix(lower, ".key") || strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
			return false
		}
	}
	return true
}

// ParseManifest requires exact canonical JSON bytes, a bounded complete sorted file set and consistent totals. Re-encoding equality also rejects duplicate JSON keys rather than accepting decoder last-value semantics.
// ParseManifest требует exact canonical JSON bytes, ограниченный полный sorted file set и согласованные totals. Равенство повторной кодировки также отклоняет duplicate JSON keys вместо last-value semantics decoder.
func ParseManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	if len(raw) == 0 || len(raw) > MaximumManifestBytes || !json.Valid(raw) {
		return manifest, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || manifest.Version != 1 || len(manifest.Files) == 0 || len(manifest.Files) > MaximumFiles || manifest.TotalBytes < 0 || manifest.TotalBytes > MaximumContentBytes {
		return Manifest{}, ErrInvalid
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Manifest{}, ErrInvalid
	}
	var total int64
	seen := make(map[string]bool, len(manifest.Files))
	for index, entry := range manifest.Files {
		fold := strings.ToLower(entry.Path)
		if !ValidPath(entry.Path) || entry.Mode != "100644" && entry.Mode != "100755" || entry.Bytes < 0 || entry.Bytes > MaximumContentBytes-total || !ValidDigest(entry.SHA256) || seen[fold] || index > 0 && manifest.Files[index-1].Path >= entry.Path {
			return Manifest{}, ErrInvalid
		}
		seen[fold] = true
		total += entry.Bytes
	}
	if total != manifest.TotalBytes {
		return Manifest{}, ErrInvalid
	}
	// A file cannot also be another entry's parent directory.
	// Один file не может одновременно быть parent directory другой entry.
	for _, entry := range manifest.Files {
		for parent := path.Dir(entry.Path); parent != "."; parent = path.Dir(parent) {
			if seen[strings.ToLower(parent)] {
				return Manifest{}, ErrInvalid
			}
		}
	}
	return manifest, nil
}

// Open validates one of two exact bounded containers against the full canonical manifest: typed JSON/base64 or an independently admitted CLEAN ZIP. This checks byte integrity only; the owner must separately apply native format and antivirus policy to every decoded member.
// Open сверяет один из двух exact bounded containers с полным canonical manifest: typed JSON/base64 либо независимо допущенный CLEAN ZIP. Здесь проверяется лишь целостность bytes; owner отдельно применяет native format и antivirus policy к каждому decoded member.
func Open(archive, rawManifest []byte) (Manifest, []File, error) {
	manifest, err := ParseManifest(rawManifest)
	if err != nil || len(archive) == 0 || len(archive) > MaximumArchiveBytes {
		return Manifest{}, nil, ErrInvalid
	}
	if archive[0] == '{' {
		return openEncodedBundle(archive, rawManifest, manifest)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || len(reader.File) != len(manifest.Files) {
		return Manifest{}, nil, ErrInvalid
	}
	entries := make(map[string]Entry, len(manifest.Files))
	for _, entry := range manifest.Files {
		entries[entry.Path] = entry
	}
	files := make(map[string][]byte, len(manifest.Files))
	for _, member := range reader.File {
		entry, found := entries[member.Name]
		_, duplicate := files[member.Name]
		if !found || duplicate || !member.Mode().IsRegular() || member.Flags&1 != 0 || member.UncompressedSize64 != uint64(entry.Bytes) || member.Method != zip.Store && member.Method != zip.Deflate {
			return Manifest{}, nil, ErrInvalid
		}
		stream, openErr := member.Open()
		if openErr != nil {
			return Manifest{}, nil, ErrInvalid
		}
		body, readErr := io.ReadAll(io.LimitReader(stream, entry.Bytes+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || int64(len(body)) != entry.Bytes || Digest(body) != entry.SHA256 {
			return Manifest{}, nil, ErrInvalid
		}
		files[member.Name] = body
	}
	result := make([]File, 0, len(manifest.Files))
	for _, entry := range manifest.Files {
		body, found := files[entry.Path]
		if !found {
			return Manifest{}, nil, ErrInvalid
		}
		result = append(result, File{Entry: entry, Bytes: body})
	}
	return manifest, result, nil
}

// EncodedFile identifies one exact regular source file and canonical standard Base64 bytes; MIME and scan verdict are server-derived, never supplied by this envelope.
// EncodedFile обозначает exact regular source file и canonical standard Base64 bytes; MIME и scan verdict вычисляет сервер, envelope их не задаёт.
type EncodedFile struct {
	Path          string `json:"path"`
	Mode          string `json:"mode"`
	ContentBase64 string `json:"content_base64"`
}

// EncodedBundle is the closed version-one source container, bound to a separate canonical full manifest; outer JSON CLEAN is explicitly not decoded-file admission.
// EncodedBundle — closed source container версии1 с привязкой к отдельному canonical full manifest; outer JSON CLEAN явно не является допуском decoded files.
type EncodedBundle struct {
	Version        int           `json:"version"`
	ManifestSHA256 string        `json:"manifest_sha256"`
	Files          []EncodedFile `json:"files"`
}

// openEncodedBundle rejects unknown/duplicate fields, noncanonical bytes, reordered or substituted members and noncanonical Base64 before returning any decoded file set. It performs no security scan.
// openEncodedBundle отклоняет unknown/duplicate fields, noncanonical bytes, reordered/substituted members и noncanonical Base64 до выдачи decoded file set. Security scan здесь не выполняется.
func openEncodedBundle(raw, rawManifest []byte, manifest Manifest) (Manifest, []File, error) {
	var envelope EncodedBundle
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !json.Valid(raw) || decoder.Decode(&envelope) != nil || envelope.Version != 1 || envelope.ManifestSHA256 != Digest(rawManifest) || len(envelope.Files) != len(manifest.Files) {
		return Manifest{}, nil, ErrInvalid
	}
	canonical, err := json.Marshal(envelope)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Manifest{}, nil, ErrInvalid
	}
	result := make([]File, 0, len(manifest.Files))
	for index, encoded := range envelope.Files {
		entry := manifest.Files[index]
		if encoded.Path != entry.Path || encoded.Mode != entry.Mode || int64(len(encoded.ContentBase64)) != (entry.Bytes+2)/3*4 {
			return Manifest{}, nil, ErrInvalid
		}
		content, err := base64.StdEncoding.Strict().DecodeString(encoded.ContentBase64)
		if err != nil || int64(len(content)) != entry.Bytes || base64.StdEncoding.EncodeToString(content) != encoded.ContentBase64 || Digest(content) != entry.SHA256 {
			clear(content)
			return Manifest{}, nil, ErrInvalid
		}
		result = append(result, File{Entry: entry, Bytes: content})
	}
	return manifest, result, nil
}

// CanonicalContainer creates the typed source envelope and its separate full canonical manifest from already chosen regular files. It does not classify, scan, dequarantine or publish their content.
// CanonicalContainer создаёт typed source envelope и отдельный full canonical manifest из выбранных regular files. Content не классифицируется, не сканируется, не выводится из карантина и не публикуется.
func CanonicalContainer(files []File) ([]byte, []byte, error) {
	rawManifest, err := CanonicalManifest(files)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := ParseManifest(rawManifest)
	if err != nil {
		return nil, nil, err
	}
	contents := make(map[string][]byte, len(files))
	for _, file := range files {
		contents[file.Entry.Path] = file.Bytes
	}
	envelope := EncodedBundle{Version: 1, ManifestSHA256: Digest(rawManifest), Files: make([]EncodedFile, 0, len(manifest.Files))}
	for _, entry := range manifest.Files {
		envelope.Files = append(envelope.Files, EncodedFile{Path: entry.Path, Mode: entry.Mode, ContentBase64: base64.StdEncoding.EncodeToString(contents[entry.Path])})
	}
	raw, err := json.Marshal(envelope)
	if err != nil || len(raw) > MaximumArchiveBytes {
		return nil, nil, ErrInvalid
	}
	return raw, rawManifest, nil
}

// CanonicalManifest constructs sorted exact metadata from already chosen regular files; it does not sanitize or silently omit an invalid member.
// CanonicalManifest строит sorted exact metadata выбранных regular files; недопустимый member не очищается и не пропускается молча.
func CanonicalManifest(files []File) ([]byte, error) {
	manifest := Manifest{Version: 1, Files: make([]Entry, 0, len(files))}
	for _, file := range files {
		entry := file.Entry
		entry.Bytes, entry.SHA256 = int64(len(file.Bytes)), Digest(file.Bytes)
		manifest.Files = append(manifest.Files, entry)
		manifest.TotalBytes += entry.Bytes
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err := ParseManifest(raw); err != nil {
		return nil, err
	}
	return raw, nil
}
