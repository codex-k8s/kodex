package platform

import (
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"strings"
	"testing"
)

func TestPreparedContentCommitment(t *testing.T) {
	request := preparedContentRequest{Binding: preparedContentBinding{ActorID: "actor", OperationKey: "operation"}, Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 3, Body: strings.NewReader("one")}
	first := preparedContentRequestDigest(request)
	request.Body = strings.NewReader("different ignored stream")
	if preparedContentRequestDigest(request) != first {
		t.Fatal("body leaked into safe commitment")
	}
	request.Binding.SourceLeaseGeneration++
	if preparedContentRequestDigest(request) == first {
		t.Fatal("source generation not pinned")
	}
}

func TestPreparedContentRequiresExactPositiveReceipt(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	receipt := objectstorage.Receipt{Key: "key", VersionID: "v1", ETag: "etag", Digest: digest, SizeBytes: 1}
	if !exactPreparedReceipt(receipt, "key", digest, 1) {
		t.Fatal("exact receipt rejected")
	}
	for _, bad := range []objectstorage.Receipt{{}, {Key: "other", VersionID: "v1", ETag: "etag", Digest: digest, SizeBytes: 1},
		{Key: "key", ETag: "etag", Digest: digest, SizeBytes: 1}, {Key: "key", VersionID: "v1", Digest: digest, SizeBytes: 1},
		{Key: "key", VersionID: "v1", ETag: "etag", Digest: digest, SizeBytes: 2}} {
		if exactPreparedReceipt(bad, "key", digest, 1) {
			t.Fatal("incomplete or mismatched receipt accepted")
		}
	}
}
