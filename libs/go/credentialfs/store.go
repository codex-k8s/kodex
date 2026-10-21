// Package credentialfs reads bounded credential files from a selected root.
// It checks reference names and resolved paths before delegating the file
// size, regular-file and read-only-mode checks to securefile.
// RU: Пакет credentialfs читает ограниченные credential-файлы из заданного
// корня. Он проверяет имена и разрешённые пути, а размер, тип и режим файла
// дополнительно проверяет securefile.
package credentialfs

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/securefile"
)

// maximumCredentialBytes bounds one materialized credential to one MiB.
// RU: maximumCredentialBytes ограничивает один credential одним МиБ.
const maximumCredentialBytes = 1 << 20

// referencePattern admits only short ASCII connection references without
// separators, dots or whitespace; the file name has a separate policy.
// RU: referencePattern принимает лишь короткие ASCII-ссылки на подключение
// без разделителей, точек и пробелов; имя файла проверяется отдельно.
var referencePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)

// Store holds a server-selected root and reads exact files on demand without
// caching their material or interpreting the credential's format.
// RU: Store хранит заданный сервером корень и читает конкретные файлы по
// запросу, не кэшируя содержимое и не интерпретируя формат credential.
type Store struct{ root string }

// New accepts only an absolute, already-clean root path. It does not open or
// resolve that root until a read, so unavailable mounts fail at use time.
// RU: New принимает только абсолютный нормализованный путь корня. До чтения
// корень не открывается и не разрешается, поэтому недоступность mount видна при чтении.
func New(root string) (*Store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("credential root is invalid")
	}
	return &Store{root: root}, nil
}

// Read resolves a bounded connection reference and safe file name inside the
// selected root. It rejects a resolved path outside the root, then delegates
// bounded read-only regular-file validation; errors omit paths and contents.
// RU: Read разрешает ограниченную ссылку на подключение и допустимое имя
// файла внутри корня. Путь за корнем отклоняется, затем проверяются размер,
// обычный тип и read-only режим файла; ошибки не раскрывают путь или содержимое.
func (store *Store) Read(reference, name string) ([]byte, error) {
	if store == nil || !referencePattern.MatchString(reference) || !safeName(name) {
		return nil, errors.New("credential reference is invalid")
	}
	root, err := filepath.EvalSymlinks(store.root)
	if err != nil {
		return nil, errors.New("credential root is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, reference, name))
	if err != nil {
		return nil, errors.New("credential file is unavailable")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, errors.New("credential file escapes root")
	}
	value, err := securefile.ReadWithin(root, resolved, maximumCredentialBytes)
	if err != nil {
		return nil, errors.New("credential file is unavailable")
	}
	return value, nil
}

// ReadKey reads one data key from a pre-mounted exact Kubernetes Secret. The
// resolved file must stay within the root with the requested basename,
// including projected ..data symlinks; securefile checks precede the return.
// RU: ReadKey читает один ключ заранее смонтированного точного Kubernetes
// Secret. Разрешённый файл остаётся внутри корня с запрошенным конечным именем,
// включая projected-ссылки ..data; значение выдаётся после проверок securefile.
func (store *Store) ReadKey(name string) ([]byte, error) {
	if store == nil || !safeName(name) {
		return nil, errors.New("credential key is invalid")
	}
	root, err := filepath.EvalSymlinks(store.root)
	if err != nil {
		return nil, errors.New("credential root is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return nil, errors.New("credential file is unavailable")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relative) || filepath.Base(relative) != name {
		return nil, errors.New("credential file escapes root")
	}
	value, err := securefile.ReadWithin(root, resolved, maximumCredentialBytes)
	if err != nil {
		return nil, errors.New("credential file is unavailable")
	}
	return value, nil
}

// safeName permits a bounded single filename of ASCII letters, digits, dot,
// dash and underscore, excluding empty, traversal and directory components.
// RU: safeName допускает одно ограниченное имя файла из ASCII-букв, цифр,
// точки, дефиса и подчёркивания, исключая пустоту и обход каталогов.
func safeName(name string) bool {
	if name == "" || len(name) > 128 || filepath.Base(name) != name || name == "." || name == ".." {
		return false
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
