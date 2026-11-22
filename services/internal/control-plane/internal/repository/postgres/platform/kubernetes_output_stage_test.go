package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// TestKubernetesOutputStageRejectsDamagedReceipt prevents successful storage responses from
// substituting another tenant key, bytes, size or an absent/unrepresentable immutable version.
// TestKubernetesOutputStageRejectsDamagedReceipt не позволяет successful storage response
// подменить tenant key/bytes/size либо отсутствующую или непредставимую immutable version.
func TestKubernetesOutputStageRejectsDamagedReceipt(t *testing.T) {
	const key = "organizations/org/kubernetes-output-stages/kof_test"
	digest := strings.Repeat("a", 64)
	valid := objectstorage.Receipt{Key: key, Digest: "sha256:" + digest, SizeBytes: 4, VersionID: "version+/=1", ETag: "etag"}
	if !validKubernetesOutputObject(key, digest, 4, valid) {
		t.Fatal("valid exact immutable receipt rejected")
	}
	for _, change := range []func(*objectstorage.Receipt){
		func(v *objectstorage.Receipt) { v.Key = "organizations/foreign/object" },
		func(v *objectstorage.Receipt) { v.Digest = "sha256:" + strings.Repeat("b", 64) },
		func(v *objectstorage.Receipt) { v.SizeBytes = 5 },
		func(v *objectstorage.Receipt) { v.VersionID = "" },
		func(v *objectstorage.Receipt) { v.VersionID = "null" },
		func(v *objectstorage.Receipt) { v.VersionID = "NULL" },
		func(v *objectstorage.Receipt) { v.VersionID = "bad\x00version" },
		func(v *objectstorage.Receipt) { v.VersionID = string([]byte{255}) },
		func(v *objectstorage.Receipt) { v.ETag = strings.Repeat("x", 1025) },
	} {
		candidate := valid
		change(&candidate)
		if validKubernetesOutputObject(key, digest, 4, candidate) {
			t.Fatal("damaged receipt became accepted content")
		}
	}
}
