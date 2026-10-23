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
	// maximumContextBytes bounds both total extracted regular-file bytes and the
	// fully drained archive stream; the reader allows one extra byte to detect overflow.
	// maximumContextBytes ограничивает сумму извлечённых обычных файлов и весь
	// дочитанный архив; один дополнительный байт позволяет обнаружить превышение.
	maximumContextBytes = int64(512 << 20)
	// maximumContextFiles caps visited tar headers, including directory entries;
	// it is not a separate limit on parent directories created by MkdirAll.
	// maximumContextFiles ограничивает число tar headers, включая каталоги;
	// отдельного лимита родительских каталогов, созданных MkdirAll, здесь нет.
	maximumContextFiles = 100_000
	// maximumFileBytes caps each declared tar entry size before copying its body;
	// the aggregate and archive-stream limits remain independent checks.
	// maximumFileBytes ограничивает заявленный размер записи tar до копирования;
	// общий размер и поток архива имеют независимые проверки.
	maximumFileBytes = int64(128 << 20)
)

// ErrInvalidContext is the common wrapped rejection sentinel; specific safe stage
// text is appended instead of exposing underlying filesystem errors or file contents.
// ErrInvalidContext — общий оборачиваемый sentinel отказа; добавляется безопасный
// этап, а не исходная файловая ошибка либо содержимое файла.
var ErrInvalidContext = errors.New("image build context is invalid")

// ExtractContext opens the archive once and delegates extraction/hash verification
// to that same stream, closing it afterward. Opening is ordinary os.Open, not a
// no-follow/private-file check; the caller owns archive and destination isolation.
// ExtractContext один раз открывает архив и передаёт тот же поток извлечению
// и проверке хеша, затем закрывает его. Используется обычный os.Open, не no-follow/
// private-file проверка; изоляция архива и destination принадлежит вызывающему.
func ExtractContext(archivePath, destination, expectedContextSHA256, expectedSourceSHA256 string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("%w: open archive", ErrInvalidContext)
	}
	defer archive.Close()
	return ExtractContextReader(archive, destination, expectedContextSHA256, expectedSourceSHA256)
}

// ExtractContextReader extracts only relative in-root directories/regular files,
// rejects links/special entries and enforces header/per-file/aggregate/stream bounds.
// It hashes the very stream read by tar.Reader, drains trailing bytes into that hash
// and requires both exact archive digest and the trimmed source binding file.
// Files are written before final digest verification; failures leave partial output
// for the caller to discard. A caller-isolated destination is required: lexical
// containment and exclusive leaf creation do not protect an arbitrary preexisting
// directory tree against symlink races or grant permission to consume failed output.
// ExtractContextReader извлекает лишь относительные каталоги/обычные файлы внутри
// корня, отклоняет ссылки/специальные записи и ограничивает headers, размеры файла,
// суммы и потока. Хешируется тот же поток tar.Reader с дочитыванием хвоста; требуются
// точный хеш архива и файл source binding после удаления крайних пробелов.
// Запись предшествует итоговому хешу; при ошибке частичный output удаляет вызывающий.
// Destination должен быть изолирован: лексический путь и эксклюзивный конечный файл
// не защищают произвольное старое дерево от symlink races и не разрешают использовать
// output после ошибки.
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
		case tar.TypeReg:
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
			if filepath.ToSlash(clean) == ".kodex/source.sha256" {
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
