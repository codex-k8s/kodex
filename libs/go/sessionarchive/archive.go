// Package sessionarchive задаёт единый bounded-контракт подтверждённого архива Codex.
package sessionarchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	MaxFileBytes  = int64(8 << 20)
	MaxTotalBytes = int64(32 << 20)
	MaxFiles      = 512
	MaxEntries    = 1024
	MaxTarBytes   = MaxTotalBytes + int64(MaxEntries*512+MaxFiles*511+1024)
)

// Metadata содержит безопасные метаданные сжатого архива.
type Metadata struct {
	SHA256    string
	SizeBytes int64
}

// LimitError позволяет вызывающему коду отличить bounded-отказ от повреждения формата.
type LimitError struct {
	Limit string
}

func (err LimitError) Error() string {
	return "архив сессии отклонён: превышен серверный предел " + err.Limit
}

// ValidateEncoded полностью проверяет base64, метаданные и canonical gzip/USTAR.
func ValidateEncoded(encoded string, expectedSHA256 string, expectedSizeBytes int64) (Metadata, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return Metadata{}, fmt.Errorf("архив сессии пуст")
	}
	maxEncodedBytes := base64.StdEncoding.EncodedLen(int(MaxTarBytes + 1024))
	if len(encoded) > maxEncodedBytes {
		return Metadata{}, LimitError{Limit: "сжатого размера"}
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Metadata{}, fmt.Errorf("decode codex session archive: %w", err)
	}
	if int64(len(raw)) > MaxTarBytes+1024 {
		return Metadata{}, LimitError{Limit: "сжатого размера"}
	}
	digest := sha256.Sum256(raw)
	metadata := Metadata{SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(raw))}
	expectedSHA256 = strings.TrimSpace(expectedSHA256)
	if expectedSHA256 != "" && metadata.SHA256 != expectedSHA256 {
		return Metadata{}, fmt.Errorf("контрольная сумма архива сессии не совпадает")
	}
	if expectedSizeBytes != 0 && metadata.SizeBytes != expectedSizeBytes {
		return Metadata{}, fmt.Errorf("размер архива сессии не совпадает")
	}
	if err := validateRaw(raw, nil); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

// RestoreEncoded сначала дважды проверяет архив, затем атомарно публикует каталог sessions.
func RestoreEncoded(encoded string, root string, expectedSHA256 string, expectedSizeBytes int64) error {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		if strings.TrimSpace(expectedSHA256) != "" || expectedSizeBytes != 0 {
			return fmt.Errorf("пустой архив сессии содержит неожиданные метаданные")
		}
		return nil
	}
	if _, err := ValidateEncoded(encoded, expectedSHA256, expectedSizeBytes); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode codex session archive: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	stagingRoot, err := os.MkdirTemp(root, ".sessions-restore-")
	if err != nil {
		return fmt.Errorf("private staging восстановления сессии не создан")
	}
	if err := os.Chmod(stagingRoot, 0o700); err != nil {
		_ = os.RemoveAll(stagingRoot)
		return fmt.Errorf("private staging восстановления сессии не защищён")
	}
	defer os.RemoveAll(stagingRoot)
	if err := validateRaw(raw, func(header *tar.Header, name string, reader io.Reader) error {
		target := filepath.Join(stagingRoot, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			return os.Mkdir(target, 0o700)
		case tar.TypeReg:
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			syncErr := file.Sync()
			closeErr := file.Close()
			return errors.Join(copyErr, syncErr, closeErr)
		default:
			return fmt.Errorf("архив сессии содержит недопустимый тип записи")
		}
	}); err != nil {
		return err
	}
	return replaceDirectoryAtomically(filepath.Join(stagingRoot, "sessions"), filepath.Join(root, "sessions"))
}

type visitor func(header *tar.Header, canonicalName string, reader io.Reader) error

func validateRaw(raw []byte, visit visitor) error {
	compressed := bytes.NewReader(raw)
	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return fmt.Errorf("open codex session archive: %w", err)
	}
	gzipReader.Multistream(false)
	limitedArchive := &io.LimitedReader{R: gzipReader, N: MaxTarBytes + 1}
	tarReader := tar.NewReader(limitedArchive)
	fileCount := 0
	entryCount := 0
	totalBytes := int64(0)
	previousName := ""
	directories := map[string]struct{}{}
	for {
		header, readErr := tarReader.Next()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = gzipReader.Close()
			return fmt.Errorf("read codex session archive: %w", readErr)
		}
		entryCount++
		if entryCount > MaxEntries {
			_ = gzipReader.Close()
			return LimitError{Limit: "количества записей"}
		}
		if header.Format != tar.FormatUSTAR {
			_ = gzipReader.Close()
			return fmt.Errorf("архив сессии содержит недопустимый формат записи")
		}
		name, canonicalErr := canonicalName(header.Name)
		if canonicalErr != nil {
			_ = gzipReader.Close()
			return canonicalErr
		}
		if entryCount == 1 {
			if name != "sessions" || header.Typeflag != tar.TypeDir {
				_ = gzipReader.Close()
				return fmt.Errorf("архив сессии не содержит canonical directory root")
			}
		} else if name <= previousName {
			_ = gzipReader.Close()
			return fmt.Errorf("архив сессии содержит дубликат или нарушенный порядок записей")
		}
		if header.Linkname != "" || header.Mode < 0 || header.Mode&^0o7777 != 0 || header.Devmajor != 0 || header.Devminor != 0 {
			_ = gzipReader.Close()
			return fmt.Errorf("архив сессии содержит недопустимую USTAR семантику")
		}
		parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(name)))
		if name != "sessions" {
			if _, exists := directories[parent]; !exists {
				_ = gzipReader.Close()
				return fmt.Errorf("архив сессии содержит запись до canonical parent directory")
			}
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if header.Size != 0 {
				_ = gzipReader.Close()
				return fmt.Errorf("архив сессии содержит directory с данными")
			}
			directories[name] = struct{}{}
		case tar.TypeReg:
			fileCount++
			totalBytes += header.Size
			if fileCount > MaxFiles || header.Size < 0 || header.Size > MaxFileBytes {
				_ = gzipReader.Close()
				return LimitError{Limit: "размера или количества файлов"}
			}
			if totalBytes > MaxTotalBytes {
				_ = gzipReader.Close()
				return LimitError{Limit: "общего размера"}
			}
			if visit != nil {
				if err := visit(header, name, tarReader); err != nil {
					_ = gzipReader.Close()
					return err
				}
			} else if _, err := io.CopyN(io.Discard, tarReader, header.Size); err != nil {
				_ = gzipReader.Close()
				return fmt.Errorf("архив сессии содержит усечённый файл")
			}
		default:
			_ = gzipReader.Close()
			return fmt.Errorf("архив сессии содержит недопустимый тип записи")
		}
		if visit != nil && header.Typeflag == tar.TypeDir {
			if err := visit(header, name, tarReader); err != nil {
				_ = gzipReader.Close()
				return err
			}
		}
		previousName = name
	}
	if entryCount == 0 {
		_ = gzipReader.Close()
		return fmt.Errorf("архив сессии не содержит canonical directory root")
	}
	trailing, err := io.ReadAll(limitedArchive)
	if err != nil {
		_ = gzipReader.Close()
		return fmt.Errorf("архив сессии не прошёл проверку gzip")
	}
	for _, value := range trailing {
		if value != 0 {
			_ = gzipReader.Close()
			return fmt.Errorf("архив сессии содержит недопустимые trailing data")
		}
	}
	if limitedArchive.N <= 0 {
		_ = gzipReader.Close()
		return LimitError{Limit: "несжатого размера"}
	}
	if err := gzipReader.Close(); err != nil || compressed.Len() != 0 {
		return fmt.Errorf("архив сессии содержит недопустимый gzip stream")
	}
	return nil
}

func canonicalName(rawName string) (string, error) {
	if rawName == "" || strings.Contains(rawName, "\\") {
		return "", fmt.Errorf("unsafe archive path")
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rawName)))
	if cleaned != rawName || filepath.IsAbs(filepath.FromSlash(rawName)) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("unsafe archive path")
	}
	if cleaned != "sessions" && !strings.HasPrefix(cleaned, "sessions/") {
		return "", fmt.Errorf("unexpected archive path")
	}
	return cleaned, nil
}

func replaceDirectoryAtomically(source string, target string) error {
	sourceInfo, err := os.Lstat(source)
	if err != nil || !sourceInfo.IsDir() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("staging directory восстановления недоступен")
	}
	targetInfo, err := os.Lstat(target)
	if os.IsNotExist(err) {
		if err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err != nil {
			return fmt.Errorf("atomic publication каталога сессий не выполнена")
		}
		return nil
	}
	if err != nil || !targetInfo.IsDir() || targetInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("целевой каталог сессий имеет недопустимый тип")
	}
	if err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_EXCHANGE); err != nil {
		return fmt.Errorf("atomic exchange каталога сессий не выполнен")
	}
	if err := os.RemoveAll(source); err == nil {
		return nil
	}
	if rollbackErr := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_EXCHANGE); rollbackErr != nil {
		return fmt.Errorf("очистка старого каталога и rollback восстановления сессии не выполнены")
	}
	return fmt.Errorf("старый каталог сессий не удалён; восстановление отменено")
}
