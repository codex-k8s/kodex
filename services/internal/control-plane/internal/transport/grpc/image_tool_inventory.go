package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func castImageToolInventory(value *runtimecontract.ImageToolInventory, digest string) *controlplanev1.ImageToolInventory {
	result := &controlplanev1.ImageToolInventory{Status: "UNAVAILABLE"}
	if value == nil {
		return result
	}
	result.Status, result.Sha256, result.ImageDigest, result.ProvenanceSha256 = "VERIFIED", digest, value.ImageDigest, value.ProvenanceSHA256
	for _, platform := range value.Platforms {
		item := &controlplanev1.ImagePlatformToolInventory{Platform: platform.Manifest.Platform, PlatformDigest: platform.PlatformDigest, ManifestSha256: platform.ManifestSHA256}
		for _, tool := range platform.Manifest.Tools {
			item.Tools = append(item.Tools, &controlplanev1.ImageToolObservation{Name: tool.Name, Status: tool.Status, Path: tool.Path, Version: tool.Version, Sha256: tool.SHA256, Required: tool.Required})
		}
		result.Platforms = append(result.Platforms, item)
	}
	return result
}
