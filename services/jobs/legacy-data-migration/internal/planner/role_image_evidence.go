package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

type roleImagePlatformEvidence struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

type roleImagePackageEvidence struct {
	Manager   string `json:"manager"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Digest    string `json:"digest"`
	SourceRef string `json:"sourceRef"`
}

type roleImageToolEvidence struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	SourceRef string `json:"sourceRef"`
	SHA256    string `json:"sha256"`
}

type roleImageInputEvidence struct {
	BaseImageReference string                      `json:"baseImageReference"`
	BaseImageDigest    string                      `json:"baseImageDigest"`
	SourceRef          string                      `json:"sourceRef"`
	SourceRevision     string                      `json:"sourceRevision"`
	SourceSHA256       string                      `json:"sourceSha256"`
	ContextRef         string                      `json:"contextRef"`
	ContextSHA256      string                      `json:"contextSha256"`
	BuilderSHA256      string                      `json:"builderSha256"`
	FrontendSHA256     string                      `json:"frontendSha256"`
	Platforms          []roleImagePlatformEvidence `json:"platforms"`
	Packages           []roleImagePackageEvidence  `json:"packages"`
	Tools              []roleImageToolEvidence     `json:"tools"`
	InstallationBlock  string                      `json:"installationBlock"`
	ToolchainSHA256    string                      `json:"toolchainSha256"`
}

// RoleImageSpecSHA256 повторяет server-owned canonical hash recipe без
// доверия к значению из execution manifest. Порядок полей и наборы совпадают
// с entity.RoleImageRecipeInput в control-plane.
func RoleImageSpecSHA256(input *controlplanev1.RoleImageRecipeInput, policyRevision uint64,
	policySHA256 string, runtimeContractRevision uint64, runtimeContractSHA256 string,
) (string, error) {
	if input == nil || policyRevision == 0 || runtimeContractRevision == 0 ||
		!validSHA(policySHA256) || !validSHA(runtimeContractSHA256) {
		return "", errors.New("role image policy evidence is invalid")
	}
	canonical := roleImageInputEvidence{
		BaseImageReference: input.GetBaseImageReference(), BaseImageDigest: input.GetBaseImageDigest(),
		SourceRef: input.GetSourceRef(), SourceRevision: input.GetSourceRevision(), SourceSHA256: input.GetSourceSha256(),
		ContextRef: input.GetContextRef(), ContextSHA256: input.GetContextSha256(), BuilderSHA256: input.GetBuilderSha256(),
		FrontendSHA256: input.GetFrontendSha256(), InstallationBlock: input.GetInstallationBlock(),
		ToolchainSHA256: input.GetToolchainSha256(),
	}
	if len(input.GetPlatforms()) > 0 {
		canonical.Platforms = make([]roleImagePlatformEvidence, 0, len(input.GetPlatforms()))
	}
	if len(input.GetPackages()) > 0 {
		canonical.Packages = make([]roleImagePackageEvidence, 0, len(input.GetPackages()))
	}
	if len(input.GetTools()) > 0 {
		canonical.Tools = make([]roleImageToolEvidence, 0, len(input.GetTools()))
	}
	for _, item := range input.GetPlatforms() {
		canonical.Platforms = append(canonical.Platforms, roleImagePlatformEvidence{OS: item.GetOs(), Architecture: item.GetArchitecture(), Variant: item.GetVariant()})
	}
	for _, item := range input.GetPackages() {
		canonical.Packages = append(canonical.Packages, roleImagePackageEvidence{Manager: item.GetManager(), Name: item.GetName(), Version: item.GetVersion(), Digest: item.GetDigest(), SourceRef: item.GetSourceRef()})
	}
	for _, item := range input.GetTools() {
		canonical.Tools = append(canonical.Tools, roleImageToolEvidence{Name: item.GetName(), Version: item.GetVersion(), SourceRef: item.GetSourceRef(), SHA256: item.GetSha256()})
	}
	slices.SortFunc(canonical.Platforms, func(left, right roleImagePlatformEvidence) int {
		return compareText(left.OS+"/"+left.Architecture+"/"+left.Variant, right.OS+"/"+right.Architecture+"/"+right.Variant)
	})
	slices.SortFunc(canonical.Packages, func(left, right roleImagePackageEvidence) int {
		return compareText(left.Manager+"/"+left.Name, right.Manager+"/"+right.Name)
	})
	slices.SortFunc(canonical.Tools, func(left, right roleImageToolEvidence) int { return compareText(left.Name, right.Name) })
	encoded, err := json.Marshal(struct {
		Input                   roleImageInputEvidence `json:"Input"`
		PolicyRevision          uint64                 `json:"PolicyRevision"`
		PolicySHA256            string                 `json:"PolicySHA256"`
		RuntimeContractRevision uint64                 `json:"RuntimeContractRevision"`
		RuntimeContractSHA256   string                 `json:"RuntimeContractSHA256"`
	}{canonical, policyRevision, policySHA256, runtimeContractRevision, runtimeContractSHA256})
	if err != nil {
		return "", errors.New("encode role image specification evidence")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func compareText(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
