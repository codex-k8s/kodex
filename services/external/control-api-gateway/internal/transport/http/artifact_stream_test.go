package httptransport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

// TestForwardArtifactBodyStreamsBeyondLegacyLimit checks streaming seventeen MiB in nonempty
// chunks up to 64 KiB, preserving exact byte count and SHA-256 without a whole-body allocation.
// TestForwardArtifactBodyStreamsBeyondLegacyLimit проверяет потоковую передачу семнадцати MiB
// непустыми блоками до 64 KiB с точным числом байт и SHA-256 без выделения памяти для всего тела.
func TestForwardArtifactBodyStreamsBeyondLegacyLimit(t *testing.T) {
	t.Parallel()
	const size = int64(17 << 20)
	reader := &repeatedByteReader{remaining: size, value: 'a'}
	var forwarded int64
	received, digest, err := forwardArtifactBody(reader, size, func(chunk []byte) error {
		if len(chunk) == 0 || len(chunk) > 64<<10 {
			t.Fatalf("unexpected chunk size: %d", len(chunk))
		}
		forwarded += int64(len(chunk))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if received != size || forwarded != size || digest != repeatedDigest(size, 'a') {
		t.Fatalf("unexpected stream: received=%d forwarded=%d digest=%q", received, forwarded, digest)
	}
}

// TestForwardArtifactBodyRejectsContentLengthMismatch checks that a body longer than its declared
// length returns the canonical content-length mismatch error.
// TestForwardArtifactBodyRejectsContentLengthMismatch проверяет возврат канонической ошибки
// несовпадения длины для тела длиннее объявленного размера.
func TestForwardArtifactBodyRejectsContentLengthMismatch(t *testing.T) {
	t.Parallel()
	_, _, err := forwardArtifactBody(bytes.NewReader([]byte("too long")), 3, func([]byte) error { return nil })
	if !errors.Is(err, errArtifactContentLengthMismatch) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// repeatedByteReader generates a bounded stream of one repeated byte so large-body tests need no
// full-size fixture buffer.
// repeatedByteReader генерирует ограниченный поток одного повторяемого байта, чтобы тесты большого
// тела не требовали полноразмерного буфера.
type repeatedByteReader struct {
	remaining int64
	value     byte
}

// Read fills the requested slice up to the remaining synthetic byte count and reports EOF once
// that count reaches zero.
// Read заполняет переданный slice в пределах оставшегося синтетического числа байт и сообщает EOF
// при его обнулении.
func (reader *repeatedByteReader) Read(target []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	count := len(target)
	if int64(count) > reader.remaining {
		count = int(reader.remaining)
	}
	for index := 0; index < count; index++ {
		target[index] = reader.value
	}
	reader.remaining -= int64(count)
	return count, nil
}

// repeatedDigest computes the expected lowercase SHA-256 of a repeated-byte stream using a fixed
// 64 KiB working buffer.
// repeatedDigest вычисляет ожидаемый lowercase SHA-256 потока повторяющегося байта с постоянным
// рабочим буфером 64 KiB.
func repeatedDigest(size int64, value byte) string {
	hash := sha256.New()
	chunk := bytes.Repeat([]byte{value}, 64<<10)
	remaining := size
	for remaining > 0 {
		count := int64(len(chunk))
		if count > remaining {
			count = remaining
		}
		_, _ = hash.Write(chunk[:count])
		remaining -= count
	}
	return hex.EncodeToString(hash.Sum(nil))
}
