package roleimage

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// Environment combines localization/readback metadata with one server-owned recipe and Dockerfile
// template; Input is internal material and must not be exposed to browser clients.
// Environment объединяет метаданные локализации и чтения с одним рецептом и Dockerfile template
// сервера; Input является внутренним материалом и не выдаётся browser-клиентам.
type Environment struct {
	Key, NameMessageKey, DescriptionMessageKey, UnavailableMessageKey string
	SoftwareMessageKeys                                               []string
	Recommended, Available, CustomInstallationAllowed                 bool
	Input                                                             entity.RoleImageRecipeInput
	DockerfileTemplate                                                string
}

// Catalog keeps validated environments in deterministic key order and an exact-key index,
// returning copies rather than mutable catalog storage.
// Catalog хранит проверенные окружения в детерминированном порядке ключей и индексе точного поиска
// с возвратом копий вместо изменяемого хранения каталога.
type Catalog struct {
	ordered []Environment
	byKey   map[string]Environment
}

// NewCatalog clones and sorts up to one hundred environments, derives server-pinned templates,
// validates keys/recipes and requires exactly one available recommendation.
// NewCatalog копирует и сортирует до ста окружений, выводит templates с pins сервера, проверяет
// ключи и рецепты и требует ровно одну доступную рекомендацию.
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

// List returns ordered copies of environment metadata and recipe slices so caller mutation cannot
// rewrite catalog entries.
// List возвращает упорядоченные копии метаданных окружений и срезов рецепта, чтобы изменение
// caller не переписывало элементы каталога.
func (catalog *Catalog) List() []Environment {
	result := make([]Environment, 0, len(catalog.ordered))
	for _, current := range catalog.ordered {
		result = append(result, cloneEnvironment(current))
	}
	return result
}

// RecommendedSelection returns only the available recommended environment key for resolution
// through the same catalog as manual image creation.
// RecommendedSelection возвращает только ключ доступного рекомендованного окружения для разрешения
// тем же каталогом, что ручное создание образа.
func (catalog *Catalog) RecommendedSelection() (entity.RoleEnvironmentSelection, error) {
	for _, current := range catalog.ordered {
		if current.Available && current.Recommended {
			return entity.RoleEnvironmentSelection{EnvironmentKey: current.Key}, nil
		}
	}
	return entity.RoleEnvironmentSelection{}, errs.ErrUnavailable
}

// CopyBootstrapSelection requires exactly one available catalog entry with the exact base
// reference/digest and no catalog packages/tools, preserving the input Dockerfile without adding
// software.
// CopyBootstrapSelection требует ровно одно доступное окружение каталога с точными base reference
// и digest без каталоговых packages/tools, сохраняя входной Dockerfile без добавления ПО.
func (catalog *Catalog) CopyBootstrapSelection(input entity.RoleImageRecipeInput) (entity.RoleEnvironmentSelection, error) {
	var selected entity.RoleEnvironmentSelection
	for _, environment := range catalog.ordered {
		if !environment.Available || environment.Input.BaseImageReference != input.BaseImageReference || environment.Input.BaseImageDigest != input.BaseImageDigest || len(environment.Input.Packages) != 0 || len(environment.Input.Tools) != 0 {
			continue
		}
		candidate := entity.RoleEnvironmentSelection{EnvironmentKey: environment.Key, Dockerfile: input.Dockerfile}
		if _, err := catalog.Resolve(candidate); err != nil {
			continue
		}
		if selected.EnvironmentKey != "" {
			return entity.RoleEnvironmentSelection{}, errs.ErrConflict
		}
		selected = candidate
	}
	if selected.EnvironmentKey == "" {
		return selected, errs.ErrConflict
	}
	return selected, nil
}

// Resolve validates an available selection and exact server base, selects only registered
// package/tool keys and copies pinned recipe inputs before applying bounded installation and
// Dockerfile text.
// Resolve проверяет доступный выбор и точную base сервера, выбирает только зарегистрированные
// ключи packages/tools и копирует pins рецепта до применения ограниченных installation и
// Dockerfile текстов.
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
		resolved.PackageKeys, resolved.Packages = selectCatalogEntries(selection.PackageKeys, current.Input.PackageKeys, current.Input.Packages)
		if len(resolved.PackageKeys) != len(selection.PackageKeys) {
			return entity.RoleImageRecipeInput{}, errs.ErrInvalid
		}
	}
	if len(selection.ToolKeys) > 0 {
		resolved.ToolKeys, resolved.Tools = selectCatalogEntries(selection.ToolKeys, current.Input.ToolKeys, current.Input.Tools)
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

// dockerfileTemplate builds the single FROM line from the exact server-owned base image reference
// and manifest digest with a final newline.
// dockerfileTemplate строит единственную строку FROM из точных принадлежащих серверу base image
// reference и manifest digest с завершающим переводом строки.
func dockerfileTemplate(input entity.RoleImageRecipeInput) string {
	return "FROM " + input.BaseImageReference + "@" + input.BaseImageDigest + "\n"
}

// selectCatalogEntries copies matched keys and their same-index pinned entries in requested
// order; callers validate aligned catalog slices and reject any missing selection by count.
// selectCatalogEntries копирует совпавшие ключи и pins элементов с теми же индексами в запрошенном
// порядке; callers проверяют согласованные срезы каталога и отклоняют пропуски по числу результатов.
func selectCatalogEntries[T any](keys, catalogKeys []string, entries []T) ([]string, []T) {
	resultKeys := make([]string, 0, len(keys))
	result := make([]T, 0, len(keys))
	for _, key := range keys {
		index := slices.Index(catalogKeys, key)
		if index >= 0 {
			resultKeys = append(resultKeys, key)
			result = append(result, entries[index])
		}
	}
	return resultKeys, result
}

// uniqueCatalogKeys bounds a selection to two hundred fifty-six sorted, unique keys and checks
// each key against the catalog-key grammar.
// uniqueCatalogKeys ограничивает выбор двумястами пятьюдесятью шестью отсортированными уникальными
// ключами с проверкой каждого по грамматике каталога.
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

// validMessageKey requires a valid catalog-shaped localization key containing at least one dot
// without resolving a translation or inferring availability.
// validMessageKey требует правильный по форме каталога ключ локализации хотя бы с одной точкой без
// поиска перевода либо вывода доступности.
func validMessageKey(value string) bool {
	return validCatalogKey(value) && strings.Contains(value, ".")
}

// validOptionalMessageKey permits an omitted localization key and otherwise applies the same
// dotted message-key grammar as required catalog messages.
// validOptionalMessageKey допускает отсутствующий ключ локализации и иначе применяет ту же
// грамматику message key с точкой, что обязательные сообщения каталога.
func validOptionalMessageKey(value string) bool { return value == "" || validMessageKey(value) }

// cloneEnvironment copies software message keys and every recipe slice while preserving scalar
// metadata and the immutable Dockerfile template string.
// cloneEnvironment копирует software message keys и каждый срез рецепта с сохранением scalar
// метаданных и immutable строки Dockerfile template.
func cloneEnvironment(input Environment) Environment {
	input.SoftwareMessageKeys = append([]string(nil), input.SoftwareMessageKeys...)
	input.Input = cloneRecipeInput(input.Input)
	return input
}

// cloneRecipeInput copies package/tool keys, platforms, packages and tools into independent slice
// storage while retaining their scalar server-owned pin values.
// cloneRecipeInput копирует ключи packages/tools, platforms, packages и tools в независимые срезы
// с сохранением scalar значений pins сервера.
func cloneRecipeInput(input entity.RoleImageRecipeInput) entity.RoleImageRecipeInput {
	input.PackageKeys = append([]string(nil), input.PackageKeys...)
	input.ToolKeys = append([]string(nil), input.ToolKeys...)
	input.Platforms = append([]entity.RoleImagePlatform(nil), input.Platforms...)
	input.Packages = append([]entity.RoleImagePackage(nil), input.Packages...)
	input.Tools = append([]entity.RoleImageTool(nil), input.Tools...)
	return input
}
