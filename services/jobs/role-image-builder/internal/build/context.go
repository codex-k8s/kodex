package build

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maximumContextBytes = int64(512 << 20)
	maximumContextFiles = 100_000
	maximumFileBytes    = int64(128 << 20)
)

var ErrInvalidContext = errors.New("image build context is invalid")

// ExtractContext открывает один snapshot и передаёт его однопроходной проверке.
func ExtractContext(archivePath, destination, expectedContextSHA256, expectedSourceSHA256 string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("%w: open archive", ErrInvalidContext)
	}
	defer archive.Close()
	return ExtractContextReader(archive, destination, expectedContextSHA256, expectedSourceSHA256)
}

// ExtractContextReader хеширует те же bytes, которые tar.Reader извлекает в
// private destination. До совпадения digest destination не передаётся BuildKit.
func ExtractContextReader(archive io.Reader, destination, expectedContextSHA256, expectedSourceSHA256 string) error {
	hash := sha256.New()
	limited := &io.LimitedReader{R: archive, N: maximumContextBytes + 1}
	hashed := io.TeeReader(limited, hash)
	reader := tar.NewReader(hashed)
	files, total := 0, int64(0)
	sourceBindingFound := false
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("%w: read archive", ErrInvalidContext)
		}
		files++
		if files > maximumContextFiles || header.Size < 0 || header.Size > maximumFileBytes {
			return fmt.Errorf("%w: archive limit exceeded", ErrInvalidContext)
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
			strings.ContainsRune(clean, '\x00') {
			return fmt.Errorf("%w: unsafe archive path", ErrInvalidContext)
		}
		target := filepath.Join(destination, clean)
		relative, relErr := filepath.Rel(destination, target)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: archive path escaped root", ErrInvalidContext)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("%w: create directory", ErrInvalidContext)
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if total > maximumContextBytes {
				return fmt.Errorf("%w: extracted size exceeded", ErrInvalidContext)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return fmt.Errorf("%w: create parent directory", ErrInvalidContext)
			}
			output, openErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if openErr != nil {
				return fmt.Errorf("%w: create regular file", ErrInvalidContext)
			}
			_, copyErr := io.CopyN(output, reader, header.Size)
			closeErr := output.Close()
			if copyErr != nil || closeErr != nil {
				return fmt.Errorf("%w: extract regular file", ErrInvalidContext)
			}
			if filepath.ToSlash(clean) == ".mattercodex/source.sha256" {
				content, readFileErr := os.ReadFile(target)
				if readFileErr != nil || strings.TrimSpace(string(content)) != expectedSourceSHA256 || len(content) > 128 {
					return fmt.Errorf("%w: source binding mismatch", ErrInvalidContext)
				}
				sourceBindingFound = true
			}
		default:
			return fmt.Errorf("%w: links and special files are forbidden", ErrInvalidContext)
		}
	}
	if _, err := io.Copy(io.Discard, hashed); err != nil || limited.N <= 0 ||
		hex.EncodeToString(hash.Sum(nil)) != expectedContextSHA256 {
		return fmt.Errorf("%w: archive digest mismatch", ErrInvalidContext)
	}
	if !sourceBindingFound {
		return fmt.Errorf("%w: source binding is missing", ErrInvalidContext)
	}
	return nil
}
