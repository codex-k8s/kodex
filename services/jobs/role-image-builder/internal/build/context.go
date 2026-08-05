package build

import (
	"archive/tar"
	"bufio"
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

// ExtractContext закрыто проверяет digest и каждый tar entry до передачи BuildKit.
func ExtractContext(archivePath, destination, expectedContextSHA256, expectedSourceSHA256 string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("%w: open archive", ErrInvalidContext)
	}
	defer archive.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(archive, maximumContextBytes+1)); err != nil {
		return fmt.Errorf("%w: hash archive", ErrInvalidContext)
	}
	stat, err := archive.Stat()
	if err != nil || stat.Size() > maximumContextBytes || hex.EncodeToString(hash.Sum(nil)) != expectedContextSHA256 {
		return fmt.Errorf("%w: archive digest mismatch", ErrInvalidContext)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("%w: rewind archive", ErrInvalidContext)
	}
	reader := tar.NewReader(bufio.NewReader(archive))
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
	if !sourceBindingFound {
		return fmt.Errorf("%w: source binding is missing", ErrInvalidContext)
	}
	return nil
}
