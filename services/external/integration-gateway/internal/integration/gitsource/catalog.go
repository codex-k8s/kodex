// Package gitsource загружает закрытый server-owned registry Git sources.
package gitsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
)

const maximumCatalogBytes = 256 << 10

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type CatalogFile struct {
	Version uint64             `json:"version"`
	Sources []RepositorySource `json:"sources"`
}

type RepositorySource struct {
	RepositoryKey               string            `json:"repository_key"`
	URL                         string            `json:"url"`
	TLSServerName               string            `json:"tls_server_name"`
	Refs                        map[string]string `json:"refs"`
	Paths                       map[string]string `json:"paths"`
	RepositoryConnectionID      string            `json:"repository_connection_id"`
	RepositoryConnectionVersion uint64            `json:"repository_connection_version"`
	CredentialBindingID         string            `json:"credential_binding_id"`
	CredentialBindingVersion    uint64            `json:"credential_binding_version"`
	CredentialSecretRef         string            `json:"credential_secret_ref"`
	MaximumBytes                int64             `json:"maximum_bytes"`
	Digest                      string            `json:"-"`
}

type FetchSource struct {
	RepositorySource
	RefKey, Ref, PathKey, Path string
}

type Catalog struct {
	path        string
	version     uint64
	sources     map[string]RepositorySource
	credentials map[string]uint64
}

func NewCatalog(path string) (*Catalog, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("Git source catalog path is invalid")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumCatalogBytes {
		return nil, errors.New("Git source catalog file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read Git source catalog")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var file CatalogFile
	if decoder.Decode(&file) != nil || file.Version == 0 || len(file.Sources) == 0 || len(file.Sources) > 32 {
		return nil, errors.New("Git source catalog is invalid")
	}
	catalog := &Catalog{path: path, version: file.Version, sources: make(map[string]RepositorySource, len(file.Sources)), credentials: make(map[string]uint64, len(file.Sources))}
	for _, source := range file.Sources {
		parsed, parseErr := url.Parse(source.URL)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() != source.TLSServerName || source.TLSServerName != "github.com" || len(source.URL) > 320 || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			!keyPattern.MatchString(source.RepositoryKey) || uuid.Validate(source.RepositoryConnectionID) != nil || source.RepositoryConnectionVersion == 0 ||
			uuid.Validate(source.CredentialBindingID) != nil || source.CredentialBindingVersion == 0 || source.CredentialSecretRef == "" ||
			source.MaximumBytes < 1024 || source.MaximumBytes > 8<<20 || len(source.Refs) == 0 || len(source.Paths) == 0 || len(source.Refs) > 16 || len(source.Paths) > 64 {
			return nil, errors.New("Git repository source is invalid")
		}
		if source.CredentialSecretRef != "mattercodex/integration-gateway/git-credentials/"+source.RepositoryKey {
			return nil, errors.New("Git credential reference is not registered")
		}
		for key, ref := range source.Refs {
			if !keyPattern.MatchString(key) || !strings.HasPrefix(ref, "refs/") || strings.ContainsAny(ref, "\x00\r\n :") {
				return nil, errors.New("Git source ref is invalid")
			}
		}
		for key, path := range source.Paths {
			clean := filepath.ToSlash(filepath.Clean(path))
			if !keyPattern.MatchString(key) || clean != path || len(path) > 96 || filepath.IsAbs(path) || path == "." || strings.HasPrefix(path, "../") || strings.ContainsAny(path, "\x00\r\n:") {
				return nil, errors.New("Git source path is invalid")
			}
		}
		if _, duplicate := catalog.sources[source.RepositoryKey]; duplicate {
			return nil, errors.New("Git repository source is duplicated")
		}
		if version, duplicate := catalog.credentials[source.CredentialSecretRef]; duplicate && version != source.CredentialBindingVersion {
			return nil, errors.New("Git credential reference version is ambiguous")
		}
		source.Digest = digestSource(source)
		catalog.sources[source.RepositoryKey] = source
		catalog.credentials[source.CredentialSecretRef] = source.CredentialBindingVersion
	}
	return catalog, nil
}

func digestSource(source RepositorySource) string {
	source.Digest = ""
	raw, _ := json.Marshal(source)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (catalog *Catalog) Resolve(repositoryKey, refKey, pathKey string) (entity.GitSource, bool) {
	source, ok := catalog.sources[repositoryKey]
	if !ok {
		return entity.GitSource{}, false
	}
	if _, ok = source.Refs[refKey]; !ok {
		return entity.GitSource{}, false
	}
	if _, ok = source.Paths[pathKey]; !ok {
		return entity.GitSource{}, false
	}
	return entity.GitSource{
		RepositoryKey: repositoryKey, RefKey: refKey, PathKey: pathKey,
		RepositoryConnectionID: source.RepositoryConnectionID, RepositoryConnectionVersion: source.RepositoryConnectionVersion,
		RepositoryConnectionDigest: source.Digest, CredentialBindingID: source.CredentialBindingID,
		CredentialBindingVersion: source.CredentialBindingVersion, CredentialBindingDigest: digestCredential(source),
	}, true
}

func (catalog *Catalog) SourceRef(repositoryKey, refKey, pathKey string) (string, bool) {
	source, ok := catalog.FetchSource(repositoryKey, refKey, pathKey)
	if !ok {
		return "", false
	}
	return source.URL + "#" + source.Ref + ":" + source.Path, true
}

func digestCredential(source RepositorySource) string {
	raw, _ := json.Marshal([]any{source.CredentialBindingID, source.CredentialBindingVersion, source.CredentialSecretRef})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (catalog *Catalog) FetchSource(repositoryKey, refKey, pathKey string) (FetchSource, bool) {
	source, ok := catalog.sources[repositoryKey]
	if !ok {
		return FetchSource{}, false
	}
	ref, ok := source.Refs[refKey]
	if !ok {
		return FetchSource{}, false
	}
	path, ok := source.Paths[pathKey]
	if !ok {
		return FetchSource{}, false
	}
	return FetchSource{RepositorySource: source, RefKey: refKey, Ref: ref, PathKey: pathKey, Path: path}, true
}

func (catalog *Catalog) Check(context.Context) error {
	info, err := os.Stat(catalog.path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumCatalogBytes {
		return errors.New("Git source catalog is unavailable")
	}
	return nil
}

// CredentialRegistry возвращает только exact server-owned Secret refs и версии
// из уже проверенного Git source catalog.
func (catalog *Catalog) CredentialRegistry() map[string]uint64 {
	registry := make(map[string]uint64, len(catalog.credentials))
	for ref, version := range catalog.credentials {
		registry[ref] = version
	}
	return registry
}
