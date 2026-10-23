package build

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// maximumRoleEnvironmentCatalogBytes bounds catalog bytes read before JSON decoding to one MiB.
// maximumRoleEnvironmentCatalogBytes ограничивает чтение каталога перед разбором JSON одним МиБ.
const maximumRoleEnvironmentCatalogBytes = 1 << 20

// roleEnvironmentCatalog models the versioned local document; only environment base tuples become the allowlist.
// Context remains raw JSON and is not interpreted as authority by this loader.
// roleEnvironmentCatalog описывает версионированный локальный документ; в список разрешений попадают только пары баз окружений.
// Контекст остаётся сырым JSON и не трактуется загрузчиком как источник полномочий.
type roleEnvironmentCatalog struct {
	SchemaVersion uint64                  `json:"schemaVersion"`
	Context       json.RawMessage         `json:"context"`
	Environments  []roleEnvironmentRecord `json:"environments"`
}

// roleEnvironmentRecord accepts catalog presentation and setup fields while exposing exact base reference/digest pairs.
// Availability and recommendation flags are parsed but are not used to filter the base allowlist.
// roleEnvironmentRecord принимает поля представления и настройки каталога и задаёт точные пары ссылки и дайджеста базы.
// Флаги доступности и рекомендации разбираются, но не фильтруют список разрешённых баз.
type roleEnvironmentRecord struct {
	Key                       string            `json:"key"`
	NameMessageKey            string            `json:"nameMessageKey"`
	DescriptionMessageKey     string            `json:"descriptionMessageKey"`
	UnavailableMessageKey     string            `json:"unavailableMessageKey"`
	SoftwareMessageKeys       []string          `json:"softwareMessageKeys"`
	Recommended               bool              `json:"recommended"`
	Available                 bool              `json:"available"`
	CustomInstallationAllowed bool              `json:"customInstallationAllowed"`
	BaseImageReference        string            `json:"baseImageReference"`
	BaseImageDigest           string            `json:"baseImageDigest"`
	Platforms                 []json.RawMessage `json:"platforms"`
	Packages                  []json.RawMessage `json:"packages"`
	Tools                     []json.RawMessage `json:"tools"`
}

// baseAllowlist indexes exact repository@digest pairs loaded from the local role-environment catalog.
// baseAllowlist индексирует точные пары repository@digest из локального каталога ролевых окружений.
type baseAllowlist map[string]struct{}

// loadBaseAllowlist reads a bounded catalog from a clean absolute path and rejects unknown fields or trailing JSON.
// Version 1 and 1..100 nonduplicate, syntactically valid base tuples are required; no remote lookup is performed.
// loadBaseAllowlist читает ограниченный каталог по нормализованному абсолютному пути и отклоняет неизвестные поля и лишний JSON.
// Требуются версия 1 и 1..100 неповторяющихся синтаксически корректных пар баз; удалённый поиск не выполняется.
func loadBaseAllowlist(path string) (baseAllowlist, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("role environment catalog path is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open role environment catalog")
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, maximumRoleEnvironmentCatalogBytes+1))
	if err != nil || len(value) == 0 || len(value) > maximumRoleEnvironmentCatalogBytes {
		return nil, errors.New("read bounded role environment catalog")
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var document roleEnvironmentCatalog
	if err := decoder.Decode(&document); err != nil {
		return nil, errors.New("decode role environment catalog")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) ||
		document.SchemaVersion != 1 || len(document.Environments) == 0 ||
		len(document.Environments) > 100 {
		return nil, errors.New("role environment catalog is invalid")
	}
	result := make(baseAllowlist, len(document.Environments))
	for _, environment := range document.Environments {
		if !validRepository(environment.BaseImageReference) ||
			!digestPattern.MatchString(environment.BaseImageDigest) {
			return nil, errors.New("role environment base image is invalid")
		}
		key := environment.BaseImageReference + "@" + environment.BaseImageDigest
		if _, exists := result[key]; exists {
			return nil, errors.New("role environment base image is duplicated")
		}
		result[key] = struct{}{}
	}
	return result, nil
}

// Allows tests the exact combined reference/digest key without normalization, fallback or external access.
// Allows проверяет точный составной ключ ссылки и дайджеста без нормализации, запасного варианта или внешнего доступа.
func (allowlist baseAllowlist) Allows(reference, digest string) bool {
	_, ok := allowlist[reference+"@"+digest]
	return ok
}
