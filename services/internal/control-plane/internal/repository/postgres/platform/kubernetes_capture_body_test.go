package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestCapturePreparedBodyBorrowsOwnedBytesAboveLegacyLimit proves the purpose-specific bounded
// reader path without broadening the ordinary one-MiB profile or trusting a declared digest.
// TestCapturePreparedBodyBorrowsOwnedBytesAboveLegacyLimit проверяет purpose-specific bounded
// reader без расширения ordinary one-MiB profile либо доверия declared digest.
func TestCapturePreparedBodyBorrowsOwnedBytesAboveLegacyLimit(t *testing.T) {
	body := bytes.Repeat([]byte("synthetic capture\n"), 131073)
	defer clear(body)
	sum := sha256.Sum256(body)
	reader := bytes.NewReader(body)
	request := preparedContentRequest{Body: reader, SizeBytes: int64(len(body)), Digest: "sha256:" + hex.EncodeToString(sum[:]), CaptureTask: &managedkubernetes.CaptureTaskBinding{}}
	borrowed, err := preparedContentSeekableBody(request)
	if err != nil || borrowed != reader {
		t.Fatal("capture copied or rejected a bounded owned reader", err)
	}
	request.CaptureTask = nil
	if _, err := preparedContentSeekableBody(request); err == nil {
		t.Fatal("ordinary profile gained capture size authority")
	}
	request.CaptureTask = &managedkubernetes.CaptureTaskBinding{}
	request.Digest = "sha256:" + hex.EncodeToString(make([]byte, 32))
	if _, err := preparedContentSeekableBody(request); err == nil {
		t.Fatal("capture accepted a forged digest")
	}
}
