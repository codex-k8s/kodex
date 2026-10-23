package platform

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// uploadHashProbe records bytes and calls around an in-memory seekable reader and can inject read or seek errors. It never opens a file or supplies credentials and deliberately returns an injected read error even alongside successfully read bytes.
// uploadHashProbe учитывает байты и вызовы вокруг потока с перемещаемой позицией в памяти и позволяет задать ошибки read или seek. Файлы не открываются, реквизиты не передаются; заданная ошибка чтения намеренно возвращается даже вместе с успешно прочитанными байтами.
type uploadHashProbe struct {
	reader               *strings.Reader
	readErr, seekErr     error
	readCalls, seekCalls int
	bytesRead            int64
}

// Read forwards the in-memory read and records its byte count, then substitutes the injected error when present so tests cover partial-data failure as well as ordinary EOF.
// Read передаёт чтение потоку в памяти и учитывает число байт, затем при наличии подставляет заданную ошибку для проверки отказа с частичными данными наряду с обычным EOF.
func (probe *uploadHashProbe) Read(buffer []byte) (int, error) {
	probe.readCalls++
	n, err := probe.reader.Read(buffer)
	probe.bytesRead += int64(n)
	if probe.readErr != nil {
		return n, probe.readErr
	}
	return n, err
}

// Seek records the attempt and either fails without moving the source or forwards the exact offset/whence to the in-memory reader.
// Seek учитывает попытку и либо отказывает без изменения позиции источника, либо передаёт точные offset/whence потоку в памяти.
func (probe *uploadHashProbe) Seek(offset int64, whence int) (int64, error) {
	probe.seekCalls++
	if probe.seekErr != nil {
		return 0, probe.seekErr
	}
	return probe.reader.Seek(offset, whence)
}

// TestHashUploadContentKeepsLengthDigestAndReaderSemantics checks literal empty/ASCII/binary SHA-256 vectors, exact lengths and digest framing, one-extra-byte limits and injected seek/read failures. Every case starts from a displaced source position and checks that only the initial seek occurs; it does not assert upload authorization, format admission or a real file store.
// TestHashUploadContentKeepsLengthDigestAndReaderSemantics проверяет буквальные SHA-256-векторы пустого, ASCII- и бинарного содержимого, точные длины и форму дайджеста, предел с одним дополнительным байтом и заданные ошибки seek/read. Каждый случай начинает со смещённой позиции и проверяет наличие только первоначального seek; авторизация загрузки, допуск формата и реальное файловое хранилище не проверяются.
func TestHashUploadContentKeepsLengthDigestAndReaderSemantics(t *testing.T) {
	// abcDigest is the literal SHA-256 vector for the three ASCII bytes abc, including the production digest prefix.
	// abcDigest — буквальный SHA-256-вектор трёх ASCII-байт abc с префиксом production-дайджеста.
	const abcDigest = "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	// emptyDigest pins the empty-content vector, preserving the ordinary artifact path's permitted zero length.
	// emptyDigest закрепляет вектор пустого содержимого с сохранением допустимой нулевой длины обычного файла.
	const emptyDigest = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	for _, test := range []struct {
		name, body, expected, want string
		size, maximum              int64
		seekErr, readErr           error
		invalid                    bool
	}{
		{name: "empty", maximum: 0, want: emptyDigest},
		{name: "computed", body: "abc", size: 3, maximum: 4, want: abcDigest},
		{name: "matching", body: "abc", size: 3, maximum: 4, expected: abcDigest, want: abcDigest},
		{name: "exact ceiling", body: "abc", size: 3, maximum: 3, want: abcDigest},
		{name: "binary bytes", body: "\x00\xff", size: 2, maximum: 2, want: "sha256:06eb7d6a69ee19e5fbdf749018d3d2abfa04bcbd1365db312eb86dc7169389b8"},
		{name: "shorter than declared", body: "ab", size: 3, maximum: 3, invalid: true},
		{name: "longer than declared", body: "abcd", size: 3, maximum: 4, invalid: true},
		{name: "one beyond ceiling", body: "abcd", size: 3, maximum: 3, invalid: true},
		{name: "bounded longer stream", body: strings.Repeat("x", 1024), size: 3, maximum: 3, invalid: true},
		{name: "nonempty declared empty", body: "x", size: 0, maximum: 0, invalid: true},
		{name: "wrong digest", body: "abc", size: 3, maximum: 3, expected: emptyDigest, invalid: true},
		{name: "missing digest prefix", body: "abc", size: 3, maximum: 3, expected: strings.TrimPrefix(abcDigest, "sha256:"), invalid: true},
		{name: "uppercase digest", body: "abc", size: 3, maximum: 3, expected: strings.ToUpper(abcDigest), invalid: true},
		{name: "seek failure", body: "abc", size: 3, maximum: 3, seekErr: errors.New("synthetic seek refusal"), invalid: true},
		{name: "read failure with exact bytes", body: "abc", size: 3, maximum: 3, readErr: io.ErrUnexpectedEOF, invalid: true},
		{name: "read failure without bytes", size: 0, maximum: 0, readErr: io.ErrUnexpectedEOF, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &uploadHashProbe{reader: strings.NewReader(test.body), seekErr: test.seekErr, readErr: test.readErr}
			if _, err := probe.reader.Seek(1, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			got, err := hashUploadContent(probe, test.size, test.maximum, test.expected)
			if test.invalid {
				if got != "" || !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("invalid upload digest did not retain the original error boundary")
				}
			} else if err != nil || got != test.want {
				t.Fatal("valid upload digest changed")
			}
			if probe.seekCalls != 1 || probe.bytesRead > test.maximum+1 || test.seekErr != nil && probe.readCalls != 0 {
				t.Fatal("upload seek or read limit changed")
			}
			if test.seekErr == nil {
				position, err := probe.reader.Seek(0, io.SeekCurrent)
				if err != nil || position != probe.bytesRead {
					t.Fatal("hashing unexpectedly rewound the caller-owned reader")
				}
			}
		})
	}
}
