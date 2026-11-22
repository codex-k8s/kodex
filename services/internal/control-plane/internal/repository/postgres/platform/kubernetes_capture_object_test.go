package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
)

// TestKubernetesCaptureObjectPinsOldVersion proves that a later provider version cannot replace
// the owner-accepted body, and forged size/digest metadata never releases captured bytes.
// TestKubernetesCaptureObjectPinsOldVersion проверяет, что новая provider version не заменяет
// owner-accepted body; forged size/digest metadata никогда не выдаёт captured bytes.
func TestKubernetesCaptureObjectPinsOldVersion(t *testing.T) {
	store := objectstoragetest.NewVersioned()
	repository := &Repository{objects: store}
	first := []byte("retained original")
	sum := sha256.Sum256(first)
	key := "organizations/fixture/kubernetes-output-stages/kof_fixture"
	receipt, err := store.Put(t.Context(), objectstorage.PutInput{Key: key, MediaType: "application/octet-stream", Digest: "sha256:" + hex.EncodeToString(sum[:]), SizeBytes: int64(len(first)), Body: bytes.NewReader(first)})
	if err != nil {
		t.Fatal(err)
	}
	newer := []byte("different latest")
	latest := sha256.Sum256(newer)
	if _, err := store.Put(t.Context(), objectstorage.PutInput{Key: key, MediaType: "application/octet-stream", Digest: "sha256:" + hex.EncodeToString(latest[:]), SizeBytes: int64(len(newer)), Body: bytes.NewReader(newer)}); err != nil {
		t.Fatal(err)
	}
	frame := kubernetesCaptureFrame{Key: key, Version: receipt.VersionID, ETag: receipt.ETag, Digest: hex.EncodeToString(sum[:]), Bytes: uint32(len(first))}
	body, err := repository.readKubernetesCaptureObject(t.Context(), frame)
	if err != nil || !bytes.Equal(body, first) {
		t.Fatal("capture followed latest instead of exact accepted version", err)
	}
	clear(body)
	for _, change := range []func(*kubernetesCaptureFrame){func(f *kubernetesCaptureFrame) { f.Bytes++ }, func(f *kubernetesCaptureFrame) { f.Digest = hex.EncodeToString(latest[:]) }, func(f *kubernetesCaptureFrame) { f.ETag = "foreign" }} {
		changed := frame
		change(&changed)
		if body, err := repository.readKubernetesCaptureObject(t.Context(), changed); err == nil || len(body) != 0 {
			t.Fatal("capture released a forged source tuple")
		}
	}
}
