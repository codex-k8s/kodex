package build

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maximumRoleEnvironmentCatalogBytes = 1 << 20

type roleEnvironmentCatalog struct {
	SchemaVersion uint64                  `json:"schemaVersion"`
	Context       json.RawMessage         `json:"context"`
	Environments  []roleEnvironmentRecord `json:"environments"`
}

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

type baseAllowlist map[string]struct{}

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

func (allowlist baseAllowlist) Allows(reference, digest string) bool {
	_, ok := allowlist[reference+"@"+digest]
	return ok
}
