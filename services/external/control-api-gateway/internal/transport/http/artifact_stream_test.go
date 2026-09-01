package httptransport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

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

func TestForwardArtifactBodyRejectsContentLengthMismatch(t *testing.T) {
	t.Parallel()
	_, _, err := forwardArtifactBody(bytes.NewReader([]byte("too long")), 3, func([]byte) error { return nil })
	if !errors.Is(err, errArtifactContentLengthMismatch) {
		t.Fatalf("unexpected error: %v", err)
	}
}

type repeatedByteReader struct {
	remaining int64
	value     byte
}

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
