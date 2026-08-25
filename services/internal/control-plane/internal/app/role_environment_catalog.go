package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

const maximumRoleEnvironmentCatalogBytes = 1 << 20

type roleEnvironmentCatalogDocument struct {
	SchemaVersion uint64                         `json:"schemaVersion"`
	Context       roleEnvironmentContextDocument `json:"context"`
	Environments  []roleEnvironmentDocument      `json:"environments"`
}

type roleEnvironmentContextDocument struct {
	SourceRef      string `json:"sourceRef"`
	SourceRevision string `json:"sourceRevision"`
	SourceSHA256   string `json:"sourceSha256"`
	ContextRef     string `json:"contextRef"`
	ContextSHA256  string `json:"contextSha256"`
}

type roleEnvironmentDocument struct {
	Key                       string                            `json:"key"`
	NameMessageKey            string                            `json:"nameMessageKey"`
	DescriptionMessageKey     string                            `json:"descriptionMessageKey"`
	UnavailableMessageKey     string                            `json:"unavailableMessageKey"`
	SoftwareMessageKeys       []string                          `json:"softwareMessageKeys"`
	Recommended               bool                              `json:"recommended"`
	Available                 bool                              `json:"available"`
	CustomInstallationAllowed bool                              `json:"customInstallationAllowed"`
	BaseImageReference        string                            `json:"baseImageReference"`
	BaseImageDigest           string                            `json:"baseImageDigest"`
	Platforms                 []roleEnvironmentPlatformDocument `json:"platforms"`
	Packages                  []roleEnvironmentPackageDocument  `json:"packages"`
	Tools                     []roleEnvironmentToolDocument     `json:"tools"`
}

type roleEnvironmentPlatformDocument struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}

type roleEnvironmentPackageDocument struct {
	Key       string `json:"key"`
	Manager   string `json:"manager"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Digest    string `json:"digest"`
	SourceRef string `json:"sourceRef"`
}

type roleEnvironmentToolDocument struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	SourceRef string `json:"sourceRef"`
	SHA256    string `json:"sha256"`
}

func loadRoleEnvironmentCatalog(config Config) (*roleimageservice.Catalog, error) {
	value, err := readBoundedRoleEnvironmentCatalog(config.RoleEnvironmentCatalogFile)
	if err != nil {
		return nil, err
	}
	var document roleEnvironmentCatalogDocument
	if err := decodeStrictJSON(value, &document); err != nil || document.SchemaVersion != 1 ||
		!strings.HasPrefix(document.Context.ContextRef, "oci://"+config.RoleImageInputRepository+"@sha256:") {
		return nil, errors.New("decode role environment catalog")
	}
	environments := make([]roleimageservice.Environment, 0, len(document.Environments))
	for _, current := range document.Environments {
		input := entity.RoleImageRecipeInput{
			BaseImageReference: current.BaseImageReference,
			BaseImageDigest:    current.BaseImageDigest,
			SourceRef:          document.Context.SourceRef,
			SourceRevision:     document.Context.SourceRevision,
			SourceSHA256:       document.Context.SourceSHA256,
			ContextRef:         document.Context.ContextRef,
			ContextSHA256:      document.Context.ContextSHA256,
			BuilderSHA256:      config.RoleImageBuilderSHA256,
			FrontendSHA256:     config.RoleImageFrontendSHA256,
			ToolchainSHA256:    config.RoleImageToolchainSHA256,
			EnvironmentKey:     current.Key,
		}
		for _, platform := range current.Platforms {
			input.Platforms = append(input.Platforms, entity.RoleImagePlatform{
				OS: platform.OS, Architecture: platform.Architecture, Variant: platform.Variant,
			})
		}
		for _, item := range current.Packages {
			input.PackageKeys = append(input.PackageKeys, item.Key)
			input.Packages = append(input.Packages, entity.RoleImagePackage{
				Manager: item.Manager, Name: item.Name, Version: item.Version,
				Digest: item.Digest, SourceRef: item.SourceRef,
			})
		}
		for _, item := range current.Tools {
			input.ToolKeys = append(input.ToolKeys, item.Key)
			input.Tools = append(input.Tools, entity.RoleImageTool{
				Name: item.Name, Version: item.Version, SourceRef: item.SourceRef, SHA256: item.SHA256,
			})
		}
		environments = append(environments, roleimageservice.Environment{
			Key:                       current.Key,
			NameMessageKey:            current.NameMessageKey,
			DescriptionMessageKey:     current.DescriptionMessageKey,
			UnavailableMessageKey:     current.UnavailableMessageKey,
			SoftwareMessageKeys:       append([]string(nil), current.SoftwareMessageKeys...),
			Recommended:               current.Recommended,
			Available:                 current.Available,
			CustomInstallationAllowed: current.CustomInstallationAllowed,
			Input:                     input,
		})
	}
	return roleimageservice.NewCatalog(environments)
}

func readBoundedRoleEnvironmentCatalog(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open role environment catalog")
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, maximumRoleEnvironmentCatalogBytes+1))
	if err != nil || len(value) == 0 || len(value) > maximumRoleEnvironmentCatalogBytes {
		return nil, errors.New("read bounded role environment catalog")
	}
	return value, nil
}

func decodeStrictJSON(value []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON document")
	}
	return nil
}
