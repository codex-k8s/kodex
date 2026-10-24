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

// maximumRoleEnvironmentCatalogBytes caps the nonempty serialized role-environment catalog at one
// MiB before decoding and domain catalog validation.
// maximumRoleEnvironmentCatalogBytes ограничивает непустой сериализованный каталог окружений ролей
// одним MiB до декодирования и проверки доменного каталога.
const maximumRoleEnvironmentCatalogBytes = 1 << 20

// roleEnvironmentCatalogDocument defines the versioned configured catalog envelope with shared
// source/context pins and ordered environment documents.
// roleEnvironmentCatalogDocument определяет версионированный envelope заданного каталога с общими
// source и context pins и упорядоченными environment documents.
type roleEnvironmentCatalogDocument struct {
	SchemaVersion uint64                         `json:"schemaVersion"`
	Context       roleEnvironmentContextDocument `json:"context"`
	Environments  []roleEnvironmentDocument      `json:"environments"`
}

// roleEnvironmentContextDocument carries configured source revision/digest and immutable OCI
// context reference/digest used for all catalog environments.
// roleEnvironmentContextDocument несёт заданные source revision и digest и immutable OCI context
// reference и digest для всех окружений каталога.
type roleEnvironmentContextDocument struct {
	SourceRef      string `json:"sourceRef"`
	SourceRevision string `json:"sourceRevision"`
	SourceSHA256   string `json:"sourceSha256"`
	ContextRef     string `json:"contextRef"`
	ContextSHA256  string `json:"contextSha256"`
}

// roleEnvironmentDocument describes one environment and localized availability metadata with
// pinned base, platforms, packages and tools; builder/toolchain pins still come from server
// configuration.
// roleEnvironmentDocument описывает одно окружение и локализованные availability метаданные с
// pinned base, platforms, packages и tools; builder и toolchain pins поступают из конфигурации
// сервера.
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

// roleEnvironmentPlatformDocument captures explicit OS, architecture and optional variant for
// conversion into a validated role-image target platform.
// roleEnvironmentPlatformDocument сохраняет явные OS, architecture и необязательный variant для
// преобразования в проверенную целевую платформу образа роли.
type roleEnvironmentPlatformDocument struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}

// roleEnvironmentPackageDocument pairs a selectable catalog key with package manager/name/version
// and exact source/digest pins rather than arbitrary installation commands.
// roleEnvironmentPackageDocument связывает выбираемый catalog key с manager, именем, версией и
// точными source и digest pins package вместо произвольных installation команд.
type roleEnvironmentPackageDocument struct {
	Key       string `json:"key"`
	Manager   string `json:"manager"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Digest    string `json:"digest"`
	SourceRef string `json:"sourceRef"`
}

// roleEnvironmentToolDocument pairs a selectable tool key with immutable name/version/source and
// expected SHA-256 for later domain recipe validation.
// roleEnvironmentToolDocument связывает выбираемый tool key с immutable именем, версией,
// источником и ожидаемым SHA-256 для дальнейшей доменной проверки рецепта.
type roleEnvironmentToolDocument struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	SourceRef string `json:"sourceRef"`
	SHA256    string `json:"sha256"`
}

// loadRoleEnvironmentCatalog decodes one known-field schema-v1 catalog under the exact configured
// OCI repository prefix, injects server builder/frontend/toolchain pins and delegates full
// recipe/recommendation validation to the domain catalog.
// loadRoleEnvironmentCatalog декодирует один schema-v1 каталог с известными полями под точным
// configured OCI repository prefix, добавляет server pins builder, frontend и toolchain и передаёт
// полную проверку рецептов и рекомендации доменному каталогу.
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

// readBoundedRoleEnvironmentCatalog reads a nonempty catalog with a streaming one-MiB-plus-one
// bound and safe errors without asserting filesystem ownership or regular-file checks.
// readBoundedRoleEnvironmentCatalog читает непустой каталог с потоковым пределом один MiB плюс
// один байт и безопасными ошибками без утверждения проверки filesystem владения либо regular типа
// файла.
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

// decodeStrictJSON rejects unknown fields and any trailing JSON document while decoding into the
// supplied target; it is not a duplicate-key validator.
// decodeStrictJSON отклоняет неизвестные поля и любой следующий JSON документ при декодировании в
// переданную цель; валидатором повторных ключей не является.
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
