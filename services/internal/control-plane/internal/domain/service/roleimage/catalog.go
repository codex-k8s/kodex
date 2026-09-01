package roleimage

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// Environment хранит безопасный readback и полную server-owned спецификацию
// одного окружения. Поле Input никогда не передаётся browser-клиенту.
type Environment struct {
	Key, NameMessageKey, DescriptionMessageKey, UnavailableMessageKey string
	SoftwareMessageKeys                                               []string
	Recommended, Available, CustomInstallationAllowed                 bool
	Input                                                             entity.RoleImageRecipeInput
	DockerfileTemplate                                                string
}

type Catalog struct {
	ordered []Environment
	byKey   map[string]Environment
}

func NewCatalog(environments []Environment) (*Catalog, error) {
	if len(environments) == 0 || len(environments) > 100 {
		return nil, errors.New("role environment catalog is invalid")
	}
	ordered := append([]Environment(nil), environments...)
	slices.SortFunc(ordered, func(left, right Environment) int { return strings.Compare(left.Key, right.Key) })
	byKey := make(map[string]Environment, len(ordered))
	recommended := 0
	for index := range ordered {
		current := cloneEnvironment(ordered[index])
		current.Input.EnvironmentKey = current.Key
		current.DockerfileTemplate = dockerfileTemplate(current.Input)
		current.Input.Dockerfile = current.DockerfileTemplate
		if !validCatalogKey(current.Key) || !validMessageKey(current.NameMessageKey) ||
			!validMessageKey(current.DescriptionMessageKey) || !validOptionalMessageKey(current.UnavailableMessageKey) ||
			len(current.SoftwareMessageKeys) > 100 || validateRecipe(current.Input) != nil {
			return nil, errors.New("role environment catalog entry is invalid")
		}
		for _, key := range current.SoftwareMessageKeys {
			if !validMessageKey(key) {
				return nil, errors.New("role environment catalog software key is invalid")
			}
		}
		if index > 0 && ordered[index-1].Key == current.Key {
			return nil, errors.New("role environment catalog key is duplicated")
		}
		if current.Recommended && current.Available {
			recommended++
		}
		ordered[index] = current
		byKey[current.Key] = current
	}
	if recommended != 1 {
		return nil, errors.New("role environment catalog must have one available recommendation")
	}
	return &Catalog{ordered: ordered, byKey: byKey}, nil
}

func (catalog *Catalog) List() []Environment {
	result := make([]Environment, 0, len(catalog.ordered))
	for _, current := range catalog.ordered {
		result = append(result, cloneEnvironment(current))
	}
	return result
}

func (catalog *Catalog) Resolve(selection entity.RoleEnvironmentSelection) (entity.RoleImageRecipeInput, error) {
	current, ok := catalog.byKey[selection.EnvironmentKey]
	dockerfile := selection.Dockerfile
	if dockerfile == "" && ok {
		dockerfile = current.DockerfileTemplate
	}
	if !ok || !current.Available || !uniqueCatalogKeys(selection.PackageKeys) || !uniqueCatalogKeys(selection.ToolKeys) ||
		!utf8.ValidString(selection.InstallationBlock) || len(selection.InstallationBlock) > 64<<10 ||
		strings.ContainsRune(selection.InstallationBlock, 0) || selection.InstallationBlock != "" && !current.CustomInstallationAllowed ||
		validateDockerfile(dockerfile, current.Input.BaseImageReference, current.Input.BaseImageDigest) != nil {
		return entity.RoleImageRecipeInput{}, errs.ErrInvalid
	}
	resolved := cloneRecipeInput(current.Input)
	if len(selection.PackageKeys) > 0 {
		resolved.PackageKeys, resolved.Packages = selectPackages(selection.PackageKeys, current.Input)
		if len(resolved.PackageKeys) != len(selection.PackageKeys) {
			return entity.RoleImageRecipeInput{}, errs.ErrInvalid
		}
	}
	if len(selection.ToolKeys) > 0 {
		resolved.ToolKeys, resolved.Tools = selectTools(selection.ToolKeys, current.Input)
		if len(resolved.ToolKeys) != len(selection.ToolKeys) {
			return entity.RoleImageRecipeInput{}, errs.ErrInvalid
		}
	}
	resolved.InstallationBlock = selection.InstallationBlock
	resolved.Dockerfile = dockerfile
	if validateRecipe(resolved) != nil {
		return entity.RoleImageRecipeInput{}, errs.ErrInvalid
	}
	return resolved, nil
}

func dockerfileTemplate(input entity.RoleImageRecipeInput) string {
	return "FROM " + input.BaseImageReference + "@" + input.BaseImageDigest + "\n"
}

func selectPackages(keys []string, input entity.RoleImageRecipeInput) ([]string, []entity.RoleImagePackage) {
	resultKeys := make([]string, 0, len(keys))
	result := make([]entity.RoleImagePackage, 0, len(keys))
	for _, key := range keys {
		index := slices.Index(input.PackageKeys, key)
		if index >= 0 {
			resultKeys = append(resultKeys, key)
			result = append(result, input.Packages[index])
		}
	}
	return resultKeys, result
}

func selectTools(keys []string, input entity.RoleImageRecipeInput) ([]string, []entity.RoleImageTool) {
	resultKeys := make([]string, 0, len(keys))
	result := make([]entity.RoleImageTool, 0, len(keys))
	for _, key := range keys {
		index := slices.Index(input.ToolKeys, key)
		if index >= 0 {
			resultKeys = append(resultKeys, key)
			result = append(result, input.Tools[index])
		}
	}
	return resultKeys, result
}

func uniqueCatalogKeys(values []string) bool {
	if len(values) > 256 || !slices.IsSorted(values) {
		return false
	}
	for index, value := range values {
		if !validCatalogKey(value) || index > 0 && values[index-1] == value {
			return false
		}
	}
	return true
}

func validMessageKey(value string) bool {
	return validCatalogKey(value) && strings.Contains(value, ".")
}

func validOptionalMessageKey(value string) bool { return value == "" || validMessageKey(value) }

func cloneEnvironment(input Environment) Environment {
	input.SoftwareMessageKeys = append([]string(nil), input.SoftwareMessageKeys...)
	input.Input = cloneRecipeInput(input.Input)
	return input
}

func cloneRecipeInput(input entity.RoleImageRecipeInput) entity.RoleImageRecipeInput {
	input.PackageKeys = append([]string(nil), input.PackageKeys...)
	input.ToolKeys = append([]string(nil), input.ToolKeys...)
	input.Platforms = append([]entity.RoleImagePlatform(nil), input.Platforms...)
	input.Packages = append([]entity.RoleImagePackage(nil), input.Packages...)
	input.Tools = append([]entity.RoleImageTool(nil), input.Tools...)
	return input
}
