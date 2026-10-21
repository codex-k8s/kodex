package objectstoragetest

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// credentialFailure classifies only local E2E fixture input errors, never the credential value.
// RU: credentialFailure классифицирует только ошибки входа локальной E2E-оснастки, не значение реквизита.
type credentialFailure uint8

const (
	// credentialValid means a bounded private-mode fixture value was read successfully.
	// RU: credentialValid означает успешное чтение ограниченного значения из файла с закрытым режимом.
	credentialValid credentialFailure = iota
	// credentialInvalidFile means the path or file mode violates the fixture boundary.
	// RU: credentialInvalidFile означает, что путь или режим файла нарушает границу оснастки.
	credentialInvalidFile
	// credentialUnreadable means the file was empty, oversized or could not be read.
	// RU: credentialUnreadable означает пустой, слишком большой или нечитаемый файл.
	credentialUnreadable
	// credentialInvalidValue means the trimmed credential is empty or contains a line break.
	// RU: credentialInvalidValue означает пустой после обрезки реквизит либо перевод строки в нём.
	credentialInvalidValue
)

// validLoopbackHTTPEndpoint accepts only an HTTP origin on localhost or 127.0.0.1
// with an explicit port, no userinfo, query, fragment or non-root path.
// RU: validLoopbackHTTPEndpoint допускает только HTTP origin на localhost либо 127.0.0.1
// с явным портом и без userinfo, query, fragment или пути вне корня.
func validLoopbackHTTPEndpoint(raw string) bool {
	endpoint, err := url.Parse(raw)
	return err == nil && endpoint.Scheme == "http" && endpoint.User == nil && endpoint.RawQuery == "" &&
		endpoint.Fragment == "" && (endpoint.Path == "" || endpoint.Path == "/") &&
		(endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "localhost") && endpoint.Port() != ""
}

// RequireLoopbackHTTPEndpoint returns a validated local endpoint or fails the calling E2E test
// with its fixed component-specific diagnostic prefix before any network request.
// RU: RequireLoopbackHTTPEndpoint возвращает проверенный локальный адрес либо останавливает E2E-тест
// с фиксированным префиксом компонента до любого сетевого запроса.
func RequireLoopbackHTTPEndpoint(t testing.TB, raw, prefix string) string {
	t.Helper()
	if !validLoopbackHTTPEndpoint(raw) {
		t.Fatal(prefix + " endpoint must be loopback-only HTTP")
	}
	return raw
}

// readE2ECredential accepts only an absolute regular private-mode file with at most 16 KiB of content,
// then trims surrounding space and clears the mutable buffer after converting accepted bytes.
// RU: readE2ECredential принимает лишь абсолютный regular file с закрытым режимом и содержимым до 16 КиБ,
// затем обрезает внешние пробелы и очищает изменяемый буфер после преобразования принятых байтов.
func readE2ECredential(path string) (string, credentialFailure) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		!filepath.IsAbs(path) || info.Mode().Perm()&0o077 != 0 {
		return "", credentialInvalidFile
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return "", credentialUnreadable
	}
	value := strings.TrimSpace(string(raw))
	for index := range raw {
		raw[index] = 0
	}
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", credentialInvalidValue
	}
	return value, credentialValid
}

// ReadE2ECredential returns one bounded local fixture credential or fails with the caller's
// fixed diagnostic prefix; it never includes a path or credential value in its error.
// RU: ReadE2ECredential возвращает один ограниченный реквизит локальной оснастки либо
// останавливает тест с фиксированным префиксом, не раскрывая путь и значение в ошибке.
func ReadE2ECredential(t testing.TB, path, prefix string) string {
	t.Helper()
	value, failure := readE2ECredential(path)
	switch failure {
	case credentialInvalidFile:
		t.Fatal(prefix + " credential file is invalid")
	case credentialUnreadable:
		t.Fatal(prefix + " credential cannot be read")
	case credentialInvalidValue:
		t.Fatal(prefix + " credential is invalid")
	}
	return value
}
