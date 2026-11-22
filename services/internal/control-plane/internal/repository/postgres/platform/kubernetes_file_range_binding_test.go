package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestKubernetesFileRangeBindingPinsWholeRecipe rejects cross-source/version/frame rebindings and a purged source before storage.
// It exercises the real normalizer and shared manifest recipe, not database authority or deletion receipts.
// TestKubernetesFileRangeBindingPinsWholeRecipe отклоняет cross-source/version/frame rebindings и purged source до storage.
// Проверяет real normalizer и shared manifest recipe, не database authority либо deletion receipts.
func TestKubernetesFileRangeBindingPinsWholeRecipe(t *testing.T) {
	raw := bytes.Repeat([]byte("synthetic"), 8192)
	sum := sha256.Sum256(raw)
	defer clear(raw)
	content, err := managedkubernetes.NormalizeCopyUpload(managedkubernetes.CopyProfile{ArtifactRef: "art_synthetic", ArtifactSHA256: hex.EncodeToString(sum[:]), TransferFormat: "FILE", RemotePath: "/tmp/source", MaximumBytes: 128 << 10, TimeoutSeconds: 30}, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(content.Bytes)
	manifest, err := managedkubernetes.BuildCopyInputManifest(content)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	expected := kubernetesFileSourceRecord{view: entity.KubernetesFileInputView{FileInputView: managedkubernetes.FileInputView{SourceRef: "kfs_original_source", State: "READY", NormalizedSHA256: content.SHA256, NormalizedSizeBytes: uint64(len(content.Bytes))}}, manifestJSON: string(encoded), object: objectstorage.Receipt{Key: "fixture/one-normalized-source", VersionID: "immutable-v1", ETag: "etag-v1", Digest: "sha256:" + content.SHA256, SizeBytes: int64(len(content.Bytes))}}
	offset, length, digest, eof, err := manifest.Slice(0)
	if err != nil {
		t.Fatal(err)
	}
	metadata := entity.KubernetesStreamInputSubmission{Channel: "FILE_CONTENT", ByteCount: uint32(length), PayloadDigest: digest, EndOfInput: eof}
	for _, mode := range []string{"valid", "source", "version", "etag", "frame", "offset", "length", "hash", "eof", "metadata", "purged", "closed", "manifest"} {
		t.Run(mode, func(t *testing.T) {
			source, receipt, ref, index, start, size, hash, final, frame := expected, expected.object, expected.view.SourceRef, uint32(0), offset, length, digest, eof, metadata
			switch mode {
			case "source":
				ref = "kfs_foreign_source"
			case "version":
				receipt.VersionID = "current-head-v2"
			case "etag":
				receipt.ETag = "changed"
			case "frame":
				index = 1
			case "offset":
				start++
			case "length":
				size--
			case "hash":
				hash = strings.Repeat("f", 64)
			case "eof":
				final = !final
			case "metadata":
				frame.PayloadDigest = strings.Repeat("f", 64)
			case "purged":
				now := time.Now()
				source.purgedAt = &now
			case "closed":
				source.view.State = "CANCELLED"
			case "manifest":
				source.manifestJSON = strings.TrimSuffix(string(encoded), "}") + `,"unknown":true}`
			}
			read, err := bindKubernetesFileRange(source, ref, receipt, index, start, size, hash, final, frame)
			if mode == "valid" {
				if err != nil || read.Object != expected.object || read.Offset != int64(offset) || read.Length != int64(length) || read.Digest != "sha256:"+digest {
					t.Fatal("exact owner recipe changed", err)
				}
			} else if err == nil {
				t.Fatal("foreign or purged range became readable")
			}
		})
	}
}
